package hub

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/auth"
)

// A backup is the Hub's state that can't be made again: Glowpod accounts,
// sign-in settings, the server URL and the imported clusters with their
// agents' credential hashes. It's encrypted with a passphrase, since the
// SSO client secret and the LDAP bind password are in it. Sign-ins,
// kubeconfigs, shells and pending import commands aren't kept: a restore
// signs everyone out.

const (
	backupFormat  = "glowpod-hub-backup"
	backupVersion = 1
	// Key derivation for the passphrase: Argon2id, heavier than for
	// sign-in, since a backup file can be attacked offline.
	backupArgonTime   = 3
	backupArgonMemory = 64 * 1024
	backupArgonLanes  = 4
	maxBackupBytes    = 8 << 20
)

// backupFile is the file as saved: JSON around the encrypted payload.
type backupFile struct {
	Format  string    `json:"format"`
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	KDF     struct {
		Name    string `json:"name"`
		Salt    []byte `json:"salt"`
		Time    uint32 `json:"time"`
		Memory  uint32 `json:"memory"`
		Threads uint8  `json:"threads"`
	} `json:"kdf"`
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

// backupData is what the file holds, once decrypted.
type backupData struct {
	ServerURL string          `json:"serverURL"`
	Users     []backupUser    `json:"users"`
	Auth      AuthSettings    `json:"auth"`
	Clusters  []backupCluster `json:"clusters"`
}

type backupUser struct {
	Username     string `json:"username"`
	Role         string `json:"role"`
	PasswordHash string `json:"passwordHash"`
	Created      string `json:"created,omitempty"`
}

type backupCluster struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ImportedBy     string    `json:"importedBy,omitempty"`
	Created        time.Time `json:"created"`
	CredentialHash string    `json:"credentialHash,omitempty"`
}

// collect reads the Hub's state for a backup.
func (h *Hub) collect(ctx context.Context) (backupData, error) {
	set, err := h.store.settings(ctx)
	if err != nil {
		return backupData{}, err
	}
	a, err := h.store.authSettings(ctx)
	if err != nil {
		return backupData{}, err
	}
	out := backupData{ServerURL: set.ServerURL, Auth: a}
	l, err := h.store.kube.CoreV1().Secrets(h.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=user"})
	if err != nil {
		return backupData{}, fmt.Errorf("list users: %w", err)
	}
	for i := range l.Items {
		name, ok := strings.CutPrefix(l.Items[i].Name, userSecretPrefix)
		if !ok {
			continue
		}
		d := l.Items[i].Data
		out.Users = append(out.Users, backupUser{
			Username: name, Role: string(d[keyRole]), PasswordHash: string(d[keyPasswordHash]), Created: string(d[keyCreated]),
		})
	}
	records, err := h.store.clusters(ctx)
	if err != nil {
		return backupData{}, err
	}
	for _, r := range records {
		data, err := h.store.secret(ctx, clusterSecretPrefix+r.ID)
		if err != nil {
			return backupData{}, err
		}
		out.Clusters = append(out.Clusters, backupCluster{
			ID: r.ID, Name: r.Name, ImportedBy: r.ImportedBy, Created: r.Created, CredentialHash: string(data[keyCredentialHash]),
		})
	}
	slices.SortFunc(out.Users, func(a, b backupUser) int { return strings.Compare(a.Username, b.Username) })
	slices.SortFunc(out.Clusters, func(a, b backupCluster) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func backupKey(passphrase string, f *backupFile) []byte {
	return argon2.IDKey([]byte(passphrase), f.KDF.Salt, f.KDF.Time, f.KDF.Memory, f.KDF.Threads, 32)
}

func seal(d backupData, passphrase string, now time.Time) ([]byte, error) {
	plain, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("encode the backup: %w", err)
	}
	f := backupFile{Format: backupFormat, Version: backupVersion, Created: now.UTC()}
	f.KDF.Name, f.KDF.Time, f.KDF.Memory, f.KDF.Threads = "argon2id", backupArgonTime, backupArgonMemory, backupArgonLanes
	f.KDF.Salt = make([]byte, 16)
	f.Nonce = make([]byte, 12)
	if _, err := rand.Read(f.KDF.Salt); err != nil {
		return nil, fmt.Errorf("random: %w", err)
	}
	if _, err := rand.Read(f.Nonce); err != nil {
		return nil, fmt.Errorf("random: %w", err)
	}
	gcm, err := newGCM(backupKey(passphrase, &f))
	if err != nil {
		return nil, err
	}
	// The header is authenticated too: nothing in the file can be changed
	// unnoticed.
	f.Data = gcm.Seal(nil, f.Nonce, plain, additional(&f))
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the backup file: %w", err)
	}
	return raw, nil
}

// errBadBackup covers a wrong passphrase and a changed or broken file: an
// attacker learns nothing from which it was.
var errBadBackup = errors.New("this isn't a Glowpod Hub backup, or the passphrase is wrong")

func unseal(raw []byte, passphrase string) (backupData, time.Time, error) {
	var f backupFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Format != backupFormat {
		return backupData{}, time.Time{}, errBadBackup
	}
	if f.Version != backupVersion {
		return backupData{}, time.Time{}, fmt.Errorf("this backup is format %d; this Hub reads format %d", f.Version, backupVersion)
	}
	if f.KDF.Name != "argon2id" || len(f.KDF.Salt) < 16 || len(f.Nonce) != 12 ||
		f.KDF.Time == 0 || f.KDF.Time > 10 || f.KDF.Memory < 19*1024 || f.KDF.Memory > 1<<20 || f.KDF.Threads == 0 {
		return backupData{}, time.Time{}, errBadBackup
	}
	gcm, err := newGCM(backupKey(passphrase, &f))
	if err != nil {
		return backupData{}, time.Time{}, err
	}
	plain, err := gcm.Open(nil, f.Nonce, f.Data, additional(&f))
	if err != nil {
		return backupData{}, time.Time{}, errBadBackup
	}
	var d backupData
	if err := json.Unmarshal(plain, &d); err != nil {
		return backupData{}, time.Time{}, errBadBackup
	}
	return d, f.Created, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	return gcm, nil
}

// additional is the header the encryption also covers.
func additional(f *backupFile) []byte {
	return fmt.Appendf(nil, "%s|%d|%s|%x|%d|%d|%d", f.Format, f.Version, f.Created.UTC().Format(time.RFC3339Nano),
		f.KDF.Salt, f.KDF.Time, f.KDF.Memory, f.KDF.Threads)
}

// check refuses a backup that would leave the Hub broken.
func (d backupData) check() error {
	if _, ok := checkServerURL(d.ServerURL); !ok {
		return errors.New("the backup's server URL isn't valid")
	}
	hasAdmin := false
	for _, u := range d.Users {
		if checkUsername(u.Username) != "" || !checkRole(u.Role) || !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
			return fmt.Errorf("the backup's account %q isn't valid", u.Username)
		}
		hasAdmin = hasAdmin || u.Username == AdminUser
	}
	if !hasAdmin {
		return errors.New("the backup has no admin account")
	}
	for _, c := range d.Clusters {
		if len(validation.IsDNS1123Label(c.ID)) > 0 || c.ID == LocalID {
			return fmt.Errorf("the backup's cluster %q isn't valid", c.ID)
		}
	}
	return nil
}

// restore makes the Hub's state the backup's, and signs everyone out.
func (h *Hub) restore(ctx context.Context, d backupData) error {
	if err := d.check(); err != nil {
		return err
	}
	// Accounts: the backup's, and no others.
	keep := map[string]bool{}
	for _, u := range d.Users {
		keep[u.Username] = true
		data := map[string][]byte{keyPasswordHash: []byte(u.PasswordHash), keyRole: []byte(u.Role)}
		if u.Created != "" {
			data[keyCreated] = []byte(u.Created)
		}
		if err := h.store.putSecret(ctx, userSecretPrefix+u.Username, "user", data); err != nil {
			return err
		}
	}
	current, err := h.store.users(ctx)
	if err != nil {
		return err
	}
	for _, u := range current {
		if !keep[u.Username] {
			if err := h.store.deleteSecret(ctx, userSecretPrefix+u.Username); err != nil {
				return err
			}
		}
	}
	if err := h.loadRoles(ctx); err != nil {
		return err
	}

	// Clusters: the backup's, with their agents' credentials, and no others.
	want := map[string]backupCluster{}
	for _, c := range d.Clusters {
		want[c.ID] = c
	}
	have, err := h.store.clusters(ctx)
	if err != nil {
		return err
	}
	for _, r := range have {
		if _, ok := want[r.ID]; !ok {
			if err := h.forgetCluster(ctx, r.ID); err != nil {
				return err
			}
		}
	}
	for _, c := range d.Clusters {
		if _, ok := h.record(c.ID); !ok {
			if err := h.store.createCluster(ctx, Record{ID: c.ID, Name: c.Name, ImportedBy: c.ImportedBy}); err != nil {
				return err
			}
		}
		data := map[string][]byte{}
		if c.CredentialHash != "" {
			data[keyCredentialHash] = []byte(c.CredentialHash)
		}
		// A pending import command doesn't survive: the credential does.
		if err := h.store.putSecret(ctx, clusterSecretPrefix+c.ID, "cluster", data); err != nil {
			return err
		}
		h.sessions.Close(c.ID) // an agent with an old credential reconnects
		if _, ok := h.record(c.ID); !ok {
			h.add(Record{ID: c.ID, Name: c.Name, ImportedBy: c.ImportedBy, Created: c.Created})
		}
	}

	if err := h.store.saveAuthSettings(ctx, d.Auth); err != nil {
		return err
	}
	if err := h.store.saveSettings(ctx, settings{ServerURL: d.ServerURL, SetupDone: true}); err != nil {
		return err
	}
	if err := h.store.deleteSecret(ctx, BootstrapSecret); err != nil {
		return err
	}
	if err := h.applyAuth(ctx); err != nil {
		h.cfg.Logger.Warn("the restored sign-in settings don't work yet; fix them under Hub settings", "err", err)
	}

	// Everyone signs in again; kubeconfigs from before stop working.
	if h.cfg.EndAllSessions != nil {
		h.cfg.EndAllSessions(ctx)
	}
	tokens, err := h.store.kubeTokens(ctx)
	if err == nil {
		for _, t := range tokens {
			_ = h.deleteKubeToken(ctx, t.ID)
		}
	}
	h.poll(ctx)
	return nil
}

// API.

type backupRequest struct {
	Passphrase string `json:"passphrase"`
}

func (h *Hub) downloadBackup(w http.ResponseWriter, r *http.Request) {
	var req backupRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := weakPassword(req.Passphrase); msg != "" {
		writeError(w, http.StatusBadRequest, "weak_passphrase", strings.Replace(msg, "characters", "characters in the passphrase", 1))
		return
	}
	ctx := r.Context()
	d, err := h.collect(ctx)
	if err != nil {
		h.cfg.Logger.Warn("backup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "backup_failed", "Couldn't read the Hub's state.")
		return
	}
	now := h.now()
	raw, err := seal(d, req.Passphrase, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", "Couldn't make the backup.")
		return
	}
	h.cfg.Logger.Info("backup made", "by", auth.SessionInfoFrom(ctx).User, "users", len(d.Users), "clusters", len(d.Clusters))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="glowpod-hub-backup-%s.json"`, now.UTC().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

type restoreRequest struct {
	Passphrase string `json:"passphrase"`
	// Backup is the file's content.
	Backup string `json:"backup"`
}

// RestoreResult says what came back.
type RestoreResult struct {
	Created  time.Time `json:"created"`
	Users    int       `json:"users"`
	Clusters int       `json:"clusters"`
}

func (h *Hub) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBackupBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", "The request isn't valid, or the backup is too large.")
		return
	}
	d, created, err := unseal([]byte(req.Backup), req.Passphrase)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_backup", plain(err))
		return
	}
	ctx := r.Context()
	by := auth.SessionInfoFrom(ctx).User
	if err := h.restore(ctx, d); err != nil {
		h.cfg.Logger.Warn("restore failed", "err", err)
		writeError(w, http.StatusBadRequest, "restore_failed", plain(err))
		return
	}
	h.cfg.Logger.Info("backup restored", "by", by, "made", created, "users", len(d.Users), "clusters", len(d.Clusters))
	writeJSON(w, http.StatusOK, RestoreResult{Created: created, Users: len(d.Users), Clusters: len(d.Clusters)})
}
