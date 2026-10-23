package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"glowpod/internal/cluster"
)

// PasswordChecker signs in a Hub's local users. It returns the name
// Kubernetes sees (glowpod:admin), or ErrBadCredentials.
type PasswordChecker interface {
	CheckPassword(ctx context.Context, username, password string) (string, error)
}

func (s *Server) loginPassword(w http.ResponseWriter, r *http.Request) {
	var req ldapLogin
	if !s.loginRequest(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if !validUsername(username) || req.Password == "" || len(req.Password) > maxPassword {
		writeJSONError(w, http.StatusBadRequest, "bad_credentials", "Enter your username and password.")
		return
	}
	limitKey := "password:" + strings.ToLower(username)
	if s.idLimit.blocked(limitKey) {
		writeJSONError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), loginTimeout)
	defer cancel()
	user, err := s.cfg.Passwords.CheckPassword(ctx, username, req.Password)
	if errors.Is(err, ErrBadCredentials) {
		s.idLimit.fail(limitKey)
		writeJSONError(w, http.StatusUnauthorized, "bad_credentials", "That username and password didn't work.")
		return
	}
	if err != nil {
		s.cfg.Logger.Warn("password sign-in failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "sign_in_failed", "Couldn't check the password. Try again in a moment.")
		return
	}
	if _, err := impersonable(user, nil); err != nil {
		writeJSONError(w, http.StatusForbidden, "system_user", "This account can't be used with Glowpod.")
		return
	}
	sess := &Session{
		Identity: cluster.UserIdentity(user, nil),
		Info:     SessionInfo{Mode: "in-cluster", Method: "password", User: user},
	}
	if err := s.begin(w, r, sess); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "session_failed", "Couldn't start a session.")
		return
	}
	writeOK(w, sess.Info)
}
