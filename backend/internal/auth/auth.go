package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"glowpod/internal/cluster"
)

// Authenticator signs browsers in for one mode: Local for the CLI, Server
// for an in-cluster install.
type Authenticator interface {
	// MountRoot adds browser-facing routes outside /api (sign-in links and
	// SSO redirects).
	MountRoot(r chi.Router)
	// MountAPI adds sign-in and sign-out routes under /api/v1. Sign-in
	// routes run without a session.
	MountAPI(r chi.Router)
	// Require rejects requests without a session and attaches the
	// identity, the session's info and its CSRF token.
	Require(next http.Handler) http.Handler
}

// SessionInfo is what the UI shows about who is signed in.
type SessionInfo struct {
	// Mode is local or in-cluster.
	Mode string `json:"mode"`
	// Method is kubeconfig, token, oidc or ldap.
	Method string   `json:"method"`
	User   string   `json:"username,omitempty"`
	Groups []string `json:"groups,omitempty"`
}

type identityKey struct{}

type infoKey struct{}

// WithIdentity returns ctx carrying id.
func WithIdentity(ctx context.Context, id cluster.Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom returns the identity Require attached.
func IdentityFrom(ctx context.Context) (cluster.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(cluster.Identity)
	return id, ok
}

// WithSessionInfo returns ctx carrying info.
func WithSessionInfo(ctx context.Context, info SessionInfo) context.Context {
	return context.WithValue(ctx, infoKey{}, info)
}

// SessionInfoFrom returns the session info Require attached.
func SessionInfoFrom(ctx context.Context) SessionInfo {
	info, _ := ctx.Value(infoKey{}).(SessionInfo)
	return info
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
