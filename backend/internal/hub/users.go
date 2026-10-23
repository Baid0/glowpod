package hub

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/auth"
)

// Glowpod accounts: the Hub's own users, besides SSO and LDAP. Each is a
// Secret with an Argon2id hash of the password and a Hub role. Kubernetes
// sees them as glowpod:<username>.

// User is a Glowpod account as listed.
type User struct {
	Username string    `json:"username"`
	Role     string    `json:"role"`
	Created  time.Time `json:"created"`
}

const keyCreated = "created"

func (s *store) users(ctx context.Context) ([]User, error) {
	l, err := s.kube.CoreV1().Secrets(s.namespace).List(ctx, metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=user"})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]User, 0, len(l.Items))
	for i := range l.Items {
		name, ok := strings.CutPrefix(l.Items[i].Name, userSecretPrefix)
		if !ok {
			continue
		}
		u := User{Username: name, Role: string(l.Items[i].Data[keyRole]), Created: l.Items[i].CreationTimestamp.Time}
		if t, err := time.Parse(time.RFC3339, string(l.Items[i].Data[keyCreated])); err == nil {
			u.Created = t
		}
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b User) int { return strings.Compare(a.Username, b.Username) })
	return out, nil
}

// loadRoles reads which Glowpod accounts are Hub administrators.
func (h *Hub) loadRoles(ctx context.Context) error {
	users, err := h.store.users(ctx)
	if err != nil {
		return err
	}
	admins := map[string]bool{}
	for _, u := range users {
		admins[u.Username] = u.Role == RoleAdmin
	}
	h.mu.Lock()
	h.localAdmins = admins
	h.mu.Unlock()
	return nil
}

func (h *Hub) localAdmin(user string) bool {
	name, ok := strings.CutPrefix(user, UserPrefix)
	if !ok {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.localAdmins[name]
}

// endSessions signs someone out everywhere: their sign-ins and their
// kubeconfigs, after their password, role or account changes.
func (h *Hub) endSessions(ctx context.Context, user string) {
	if h.cfg.EndSessions != nil {
		h.cfg.EndSessions(ctx, user)
	}
	tokens, err := h.store.kubeTokens(ctx)
	if err != nil {
		h.cfg.Logger.Warn("couldn't list kubeconfig tokens", "err", err)
		return
	}
	for _, t := range tokens {
		if t.User == user {
			if err := h.deleteKubeToken(ctx, t.ID); err != nil {
				h.cfg.Logger.Warn("couldn't revoke a kubeconfig token", "err", err)
			}
		}
	}
}

func checkUsername(name string) string {
	if !usernamePattern.MatchString(name) {
		return "A username is lowercase letters, digits, dots and dashes, such as ann or ann.lee."
	}
	return ""
}

func checkRole(role string) bool { return role == RoleAdmin || role == RoleUser }

func weakPassword(pw string) string {
	if len([]rune(pw)) < MinPasswordLength {
		return fmt.Sprintf("Use at least %d characters.", MinPasswordLength)
	}
	if len(pw) > 1024 {
		return "That password is too long."
	}
	return ""
}

// API, for Hub administrators.

type userRequest struct {
	Username string `json:"username,omitempty"`
	Role     string `json:"role,omitempty"`
	Password string `json:"password,omitempty"`
}

func (h *Hub) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.users(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", "Couldn't list the accounts.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Hub) createUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	if msg := checkUsername(req.Username); msg != "" {
		writeError(w, http.StatusBadRequest, "bad_username", msg)
		return
	}
	if !checkRole(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_role", "The role is admin or user.")
		return
	}
	if msg := weakPassword(req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, "weak_password", msg)
		return
	}
	existing, err := h.store.secret(ctx, userSecretPrefix+req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", "Couldn't make the account.")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "exists", "There's already an account "+req.Username+".")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", "Couldn't make the account.")
		return
	}
	if err := h.store.putSecret(ctx, userSecretPrefix+req.Username, "user", map[string][]byte{
		keyPasswordHash: []byte(hash), keyRole: []byte(req.Role), keyCreated: []byte(h.now().UTC().Format(time.RFC3339)),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", "Couldn't make the account.")
		return
	}
	_ = h.loadRoles(ctx)
	h.cfg.Logger.Info("Glowpod account made", "by", auth.SessionInfoFrom(ctx).User, "user", UserPrefix+req.Username, "role", req.Role)
	w.WriteHeader(http.StatusNoContent)
}

// updateUser changes an account's role or resets its password. Either ends
// the account's sign-ins and kubeconfigs.
func (h *Hub) updateUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "user")
	var req userRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	me := auth.SessionInfoFrom(ctx).User
	if req.Role != "" {
		switch {
		case !checkRole(req.Role):
			writeError(w, http.StatusBadRequest, "bad_role", "The role is admin or user.")
			return
		case req.Role != RoleAdmin && (name == AdminUser || UserPrefix+name == me):
			writeError(w, http.StatusBadRequest, "keep_admin", "admin, and your own account, stay administrators.")
			return
		}
	}
	data, err := h.store.secret(ctx, userSecretPrefix+name)
	if err != nil || data == nil {
		writeError(w, http.StatusNotFound, "unknown_user", "There's no such account.")
		return
	}
	if req.Role != "" {
		data[keyRole] = []byte(req.Role)
	}
	if req.Password != "" {
		if msg := weakPassword(req.Password); msg != "" {
			writeError(w, http.StatusBadRequest, "weak_password", msg)
			return
		}
		hash, err := hashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", "Couldn't change the account.")
			return
		}
		data[keyPasswordHash] = []byte(hash)
	}
	if err := h.store.putSecret(ctx, userSecretPrefix+name, "user", data); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", "Couldn't change the account.")
		return
	}
	_ = h.loadRoles(ctx)
	if UserPrefix+name != me {
		h.endSessions(ctx, UserPrefix+name)
	}
	h.cfg.Logger.Info("Glowpod account changed", "by", me, "user", UserPrefix+name,
		"role", req.Role, "password_reset", req.Password != "")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) deleteUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "user")
	ctx := r.Context()
	me := auth.SessionInfoFrom(ctx).User
	if name == AdminUser || UserPrefix+name == me {
		writeError(w, http.StatusBadRequest, "keep_admin", "admin and your own account can't be deleted.")
		return
	}
	if err := h.store.deleteSecret(ctx, userSecretPrefix+name); err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", "Couldn't delete the account.")
		return
	}
	_ = h.loadRoles(ctx)
	h.endSessions(ctx, UserPrefix+name)
	h.cfg.Logger.Info("Glowpod account deleted", "by", me, "user", UserPrefix+name)
	w.WriteHeader(http.StatusNoContent)
}

// changeOwnPassword is for anyone signed in with a Glowpod account.
func (h *Hub) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	info := auth.SessionInfoFrom(ctx)
	name, ok := strings.CutPrefix(info.User, UserPrefix)
	if !ok || info.Method != "password" {
		writeError(w, http.StatusBadRequest, "not_local", "Your password is kept by your SSO or directory, not by Glowpod.")
		return
	}
	if _, err := h.CheckPassword(ctx, name, req.Current); err != nil {
		writeError(w, http.StatusBadRequest, "wrong_password", "Your current password isn't right.")
		return
	}
	if msg := weakPassword(req.New); msg != "" {
		writeError(w, http.StatusBadRequest, "weak_password", msg)
		return
	}
	data, err := h.store.secret(ctx, userSecretPrefix+name)
	if err != nil || data == nil {
		writeError(w, http.StatusInternalServerError, "update_failed", "Couldn't change your password.")
		return
	}
	hash, err := hashPassword(req.New)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", "Couldn't change your password.")
		return
	}
	data[keyPasswordHash] = []byte(hash)
	if err := h.store.putSecret(ctx, userSecretPrefix+name, "user", data); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", "Couldn't change your password.")
		return
	}
	h.cfg.Logger.Info("password changed", "user", info.User)
	w.WriteHeader(http.StatusNoContent)
}
