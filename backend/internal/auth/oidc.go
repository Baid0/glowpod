package auth

import "net/http"

// OIDC paths, outside /api so the identity provider redirects to them.
const (
	OIDCLoginPath    = "/auth/oidc/login"
	OIDCCallbackPath = "/auth/oidc/callback"
)

// OIDC signs people in with an OpenID Connect provider (filled in next).
type OIDC struct{}

func (s *Server) oidcLogin(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not yet", http.StatusNotImplemented)
}

func (s *Server) oidcCallback(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not yet", http.StatusNotImplemented)
}
