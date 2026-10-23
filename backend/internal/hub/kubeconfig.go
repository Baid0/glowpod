package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"glowpod/internal/auth"
)

// Kubeconfig tokens let kubectl reach clusters through the Hub as the person
// who downloaded them. Each is a Secret holding the token's hash, never the
// token, with the name and groups it acts as.
const (
	kubeTokenSecretPrefix = "glowpod-kubeconfig-" //nolint:gosec // a Secret's name, not a credential
	kindKubeToken         = "kubeconfig-token"    //nolint:gosec // a label value, not a credential
	keyKubeToken          = "token"
	// DefaultKubeconfigTTL is how long a downloaded kubeconfig works.
	DefaultKubeconfigTTL = 8 * time.Hour
	// maxKubeTokens per person: a new download past it replaces the oldest.
	maxKubeTokens = 20
	// kubeTokenCacheTTL is how long a checked token is trusted without
	// reading its Secret again. Revoking clears it at once.
	kubeTokenCacheTTL = 30 * time.Second
	// lastUsedEvery is how often a token in use writes its last-used time.
	lastUsedEvery = 5 * time.Minute
)

// KubeToken is a kubeconfig token as listed: what it acts as and when.
type KubeToken struct {
	ID       string     `json:"id"`
	User     string     `json:"user"`
	Groups   []string   `json:"groups,omitempty"`
	Created  time.Time  `json:"created"`
	Expires  time.Time  `json:"expires"`
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

type storedKubeToken struct {
	KubeToken
	Hash string `json:"hash"`
}

type cachedKubeToken struct {
	token   KubeToken
	checked time.Time
}

// kubeTokens keeps checked tokens briefly, keyed by the token's hash.
type kubeTokens struct {
	mu    sync.Mutex
	cache map[string]*cachedKubeToken
}

func (s *store) kubeTokens(ctx context.Context) ([]storedKubeToken, error) {
	l, err := s.kube.CoreV1().Secrets(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "hub.glowpod.dev/kind=" + kindKubeToken,
	})
	if err != nil {
		return nil, fmt.Errorf("list kubeconfig tokens: %w", err)
	}
	out := make([]storedKubeToken, 0, len(l.Items))
	for i := range l.Items {
		var t storedKubeToken
		if json.Unmarshal(l.Items[i].Data[keyKubeToken], &t) == nil && t.ID != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *store) kubeToken(ctx context.Context, id string) (storedKubeToken, bool, error) {
	data, err := s.secret(ctx, kubeTokenSecretPrefix+id)
	if err != nil || data == nil {
		return storedKubeToken{}, false, err
	}
	var t storedKubeToken
	if err := json.Unmarshal(data[keyKubeToken], &t); err != nil || t.ID != id {
		return storedKubeToken{}, false, nil //nolint:nilerr // a broken record is no token
	}
	return t, true, nil
}

func (s *store) saveKubeToken(ctx context.Context, t storedKubeToken) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("encode the token: %w", err)
	}
	return s.putSecret(ctx, kubeTokenSecretPrefix+t.ID, kindKubeToken, map[string][]byte{keyKubeToken: raw})
}

// newKubeToken stores a token acting as info and returns it once.
func (h *Hub) newKubeToken(ctx context.Context, info auth.SessionInfo) (string, KubeToken, error) {
	id, err := randomHex(6)
	if err != nil {
		return "", KubeToken{}, err
	}
	secret, hash, err := newSecret(id)
	if err != nil {
		return "", KubeToken{}, err
	}
	now := h.now().UTC()
	t := storedKubeToken{
		KubeToken: KubeToken{ID: id, User: info.User, Groups: info.Groups, Created: now, Expires: now.Add(h.kubeconfigTTL())},
		Hash:      hash,
	}
	// Past the limit, the oldest of this person's tokens goes.
	mine, err := h.myKubeTokens(ctx, info.User)
	if err != nil {
		return "", KubeToken{}, err
	}
	for len(mine) >= maxKubeTokens {
		if err := h.store.deleteSecret(ctx, kubeTokenSecretPrefix+mine[0].ID); err != nil {
			return "", KubeToken{}, err
		}
		mine = mine[1:]
	}
	if err := h.store.saveKubeToken(ctx, t); err != nil {
		return "", KubeToken{}, err
	}
	h.cfg.Logger.Info("kubeconfig token made", "user", info.User, "token_id", id, "expires", t.Expires)
	return secret, t.KubeToken, nil
}

func (h *Hub) kubeconfigTTL() time.Duration {
	if h.cfg.KubeconfigTTL > 0 {
		return h.cfg.KubeconfigTTL
	}
	return DefaultKubeconfigTTL
}

// myKubeTokens are user's live tokens, oldest first.
func (h *Hub) myKubeTokens(ctx context.Context, user string) ([]KubeToken, error) {
	all, err := h.store.kubeTokens(ctx)
	if err != nil {
		return nil, err
	}
	now := h.now()
	var out []KubeToken
	for _, t := range all {
		if t.User == user && now.Before(t.Expires) {
			out = append(out, t.KubeToken)
		}
	}
	slices.SortFunc(out, func(a, b KubeToken) int { return a.Created.Compare(b.Created) })
	return out, nil
}

// checkKubeToken returns who a bearer token acts as.
func (h *Hub) checkKubeToken(ctx context.Context, secret string) (KubeToken, bool) {
	id, ok := splitSecret(secret)
	if !ok {
		return KubeToken{}, false
	}
	hash := hashSecret(secret)
	now := h.now()
	h.kubeTokens.mu.Lock()
	if c, ok := h.kubeTokens.cache[hash]; ok && now.Sub(c.checked) < kubeTokenCacheTTL && now.Before(c.token.Expires) {
		h.kubeTokens.mu.Unlock()
		return c.token, true
	}
	h.kubeTokens.mu.Unlock()

	t, ok, err := h.store.kubeToken(ctx, id)
	if err != nil {
		h.cfg.Logger.Warn("couldn't read a kubeconfig token", "token_id", id, "err", err)
		return KubeToken{}, false
	}
	if !ok || !sameHash([]byte(t.Hash), secret) || !now.Before(t.Expires) {
		return KubeToken{}, false
	}
	if t.LastUsed == nil || now.Sub(*t.LastUsed) >= lastUsedEvery {
		used := now.UTC()
		t.LastUsed = &used
		if err := h.store.saveKubeToken(ctx, t); err != nil {
			h.cfg.Logger.Debug("couldn't save a token's last use", "token_id", id, "err", err)
		}
	}
	h.kubeTokens.mu.Lock()
	if h.kubeTokens.cache == nil || len(h.kubeTokens.cache) > 10000 {
		h.kubeTokens.cache = map[string]*cachedKubeToken{}
	}
	h.kubeTokens.cache[hash] = &cachedKubeToken{token: t.KubeToken, checked: now}
	h.kubeTokens.mu.Unlock()
	return t.KubeToken, true
}

func (h *Hub) forgetKubeToken(id string) {
	h.kubeTokens.mu.Lock()
	defer h.kubeTokens.mu.Unlock()
	for k, c := range h.kubeTokens.cache {
		if c.token.ID == id {
			delete(h.kubeTokens.cache, k)
		}
	}
}

// pruneKubeTokens deletes expired tokens.
func (h *Hub) pruneKubeTokens(ctx context.Context) {
	all, err := h.store.kubeTokens(ctx)
	if err != nil {
		h.cfg.Logger.Warn("couldn't list kubeconfig tokens", "err", err)
		return
	}
	now := h.now()
	for _, t := range all {
		if !now.Before(t.Expires) {
			if err := h.store.deleteSecret(ctx, kubeTokenSecretPrefix+t.ID); err != nil {
				h.cfg.Logger.Warn("couldn't delete an expired kubeconfig token", "err", err)
			}
		}
	}
}

// API.

type kubeconfigRequest struct {
	// Current is the context kubectl starts in: the cluster on screen.
	Current string `json:"current"`
}

// downloadKubeconfig makes a token and answers with a kubeconfig that has a
// context for each cluster the person sees.
func (h *Hub) downloadKubeconfig(w http.ResponseWriter, r *http.Request) {
	var req kubeconfigRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	info := auth.SessionInfoFrom(ctx)
	serverURL, err := h.serverURL(ctx)
	if err != nil {
		writeError(w, http.StatusConflict, "no_server_url", "Set the Hub's server URL first.")
		return
	}
	var ids []string
	for _, c := range h.cfg.Registry.List() {
		if h.Visible(info, c.ID) {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		writeError(w, http.StatusConflict, "no_clusters", "You have no clusters yet, so there's nothing to put in a kubeconfig.")
		return
	}
	secret, t, err := h.newKubeToken(ctx, info)
	if err != nil {
		h.cfg.Logger.Warn("couldn't make a kubeconfig token", "err", err)
		writeError(w, http.StatusInternalServerError, "kubeconfig_failed", "Couldn't make a kubeconfig. Try again in a moment.")
		return
	}
	raw, err := kubeconfigFor(serverURL, []byte(h.cfg.HubCA), ids, req.Current, secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kubeconfig_failed", "Couldn't make a kubeconfig.")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", `attachment; filename="glowpod-kubeconfig.yaml"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Glowpod-Token-Expires", t.Expires.Format(time.RFC3339))
	_, _ = w.Write(raw)
}

// kubeconfigFor has one context per cluster, named by its ID.
func kubeconfigFor(serverURL string, ca []byte, ids []string, current, token string) ([]byte, error) {
	cfg := clientcmdapi.NewConfig()
	const user = "glowpod"
	cfg.AuthInfos[user] = &clientcmdapi.AuthInfo{Token: token}
	for _, id := range ids {
		cfg.Clusters[id] = &clientcmdapi.Cluster{
			Server:                   strings.TrimSuffix(serverURL, "/") + KubeAPIPrefix + id,
			CertificateAuthorityData: ca,
		}
		cfg.Contexts[id] = &clientcmdapi.Context{Cluster: id, AuthInfo: user}
	}
	cfg.CurrentContext = ids[0]
	if slices.Contains(ids, current) {
		cfg.CurrentContext = current
	}
	raw, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, fmt.Errorf("write the kubeconfig: %w", err)
	}
	return raw, nil
}

func (h *Hub) listKubeTokens(w http.ResponseWriter, r *http.Request) {
	mine, err := h.myKubeTokens(r.Context(), auth.SessionInfoFrom(r.Context()).User)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", "Couldn't list your kubeconfigs.")
		return
	}
	if mine == nil {
		mine = []KubeToken{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": mine, "ttlSeconds": int(h.kubeconfigTTL().Seconds())})
}

// revokeKubeToken ends one of your own tokens at once.
func (h *Hub) revokeKubeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	t, ok, err := h.store.kubeToken(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "revoke_failed", "Couldn't revoke it.")
		return
	}
	if !ok || t.User != auth.SessionInfoFrom(ctx).User {
		writeError(w, http.StatusNotFound, "unknown_token", "You have no such kubeconfig.")
		return
	}
	if err := h.store.deleteSecret(ctx, kubeTokenSecretPrefix+id); err != nil {
		writeError(w, http.StatusInternalServerError, "revoke_failed", "Couldn't revoke it.")
		return
	}
	h.forgetKubeToken(id)
	h.cfg.Logger.Info("kubeconfig token revoked", "user", t.User, "token_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return hex.EncodeToString(b), nil
}
