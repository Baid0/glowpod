package auth

import "net/http"

// LDAP signs people in against a directory (filled in later).
type LDAP struct{}

func (s *Server) loginLDAP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not yet", http.StatusNotImplemented)
}
