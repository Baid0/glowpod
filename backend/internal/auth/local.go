// Package auth signs browsers in and attaches the request's identity.
//
// Local mode: glowpod prints and opens a launch link carrying a random
// token. Visiting it sets an HttpOnly, SameSite=Strict session cookie; every
// API call needs that cookie. This keeps other local users and web pages
// from using the kubeconfig through Glowpod.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"glowpod/internal/cluster"
)

const (
	// CookieName is the session cookie.
	CookieName = "glowpod_session"
	// LoginPath receives the launch link.
	LoginPath = "/auth"
)

// LocalIdentity is the only identity in local mode: the kubeconfig's user.
var LocalIdentity = cluster.Identity{Key: "local"}

// Local signs in the single local-mode user.
type Local struct {
	token    string
	session  string
	csrf     string
	disabled bool
}

// NewLocal creates fresh secrets for this run of glowpod.
func NewLocal() (*Local, error) {
	token, err := randomSecret()
	if err != nil {
		return nil, err
	}
	session, err := randomSecret()
	if err != nil {
		return nil, err
	}
	csrf, err := randomSecret()
	if err != nil {
		return nil, err
	}
	return &Local{token: token, session: session, csrf: csrf}, nil
}

// NewLocalDisabled skips the session check. Only for `glowpod -dev`, where
// the Vite dev server fronts the API and the server is still loopback-only.
func NewLocalDisabled() *Local {
	// Writes still need a CSRF token; the UI reads it from /session.
	csrf, err := randomSecret()
	if err != nil {
		panic(err) // crypto/rand doesn't fail on supported platforms
	}
	return &Local{disabled: true, csrf: csrf}
}

// LaunchURL is the link to print and open. It signs the browser in and
// then opens home, a path on this site ("/c/kind-lab/").
func (l *Local) LaunchURL(base, home string) string {
	home = localPath(home)
	if l.disabled {
		return base + home
	}
	link := base + LoginPath + "?token=" + url.QueryEscape(l.token)
	if home != "/" {
		link += "&return=" + url.QueryEscape(home)
	}
	return link
}

// HandleLogin exchanges the launch token for a session cookie.
func (l *Local) HandleLogin(w http.ResponseWriter, r *http.Request) {
	home := localPath(r.URL.Query().Get("return"))
	if l.disabled || l.validSession(r) {
		http.Redirect(w, r, home, http.StatusSeeOther) //nolint:gosec // localPath keeps it a path on this site
		return
	}
	if !equal(r.URL.Query().Get("token"), l.token) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("This sign-in link doesn't match this run of Glowpod.\n" +
			"Use the link printed in the terminal where glowpod is running.\n"))
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is set whenever the request is HTTPS; local mode is plain HTTP on loopback
		Name:     CookieName,
		Value:    l.session,
		Path:     "/",
		HttpOnly: true,
		// Plain HTTP on loopback in local mode; Secure would drop the cookie there.
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	// Redirect so the token leaves the address bar and history.
	http.Redirect(w, r, home, http.StatusSeeOther) //nolint:gosec // localPath keeps it a path on this site
}

// MountRoot implements Authenticator: the launch link.
func (l *Local) MountRoot(r chi.Router) { r.Get(LoginPath, l.HandleLogin) }

// MountAPI implements Authenticator. Local mode signs in only by the launch link.
func (l *Local) MountAPI(chi.Router) {}

// Require rejects API requests without a session and attaches the identity.
func (l *Local) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.disabled && !l.validSession(r) {
			writeJSONError(w, http.StatusUnauthorized, "unauthenticated",
				"Open Glowpod from the link in your terminal to sign in.")
			return
		}
		ctx := WithCSRFToken(WithIdentity(r.Context(), LocalIdentity), l.csrf)
		ctx = WithSessionInfo(ctx, SessionInfo{Mode: "local", Method: "kubeconfig"})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (l *Local) validSession(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	return err == nil && equal(c.Value, l.session)
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
