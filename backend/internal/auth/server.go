package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"glowpod/internal/cluster"
)

const (
	maxTokenLen = 16 << 10
	// loginTries is failed sign-ins per person, or attempts per address
	// (times three), within loginWindow.
	loginTries   = 10
	loginWindow  = 5 * time.Minute
	loginTimeout = 15 * time.Second
)

// WhoAmI asks the cluster who an identity is, as that identity.
type WhoAmI func(ctx context.Context, id cluster.Identity) (user string, groups []string, err error)

// ServerConfig configures in-cluster sign-in.
type ServerConfig struct {
	Store *Store
	// SecureCookies sets the Secure flag; turn it off only for plain-HTTP tests.
	SecureCookies bool
	WhoAmI        WhoAmI
	// Forget drops an identity's cached clients at sign-out.
	Forget func(cluster.Identity)
	// Token allows signing in with a service-account (or any bearer) token.
	Token bool
	OIDC  *OIDC
	LDAP  *LDAP
	// Passwords signs in a Hub's local users.
	Passwords PasswordChecker
	// OIDCName labels the SSO button, e.g. "Okta".
	OIDCName string
	// TrustedProxies are the proxies (the ingress controller) whose
	// X-Forwarded-For gives the client's address.
	TrustedProxies []netip.Prefix
	Logger         *slog.Logger
}

// Server signs people in when Glowpod runs in the cluster for many users.
type Server struct {
	cfg     ServerConfig
	ipLimit *limiter
	idLimit *limiter
	// providers are SSO and LDAP as they are now: a Hub can change them
	// while it runs.
	providers atomic.Pointer[providers]
}

type providers struct {
	oidc     *OIDC
	oidcName string
	ldap     *LDAP
}

// SetProviders replaces SSO and LDAP sign-in at once; nil turns one off.
// Sign-ins already started with the old SSO settings must start again.
func (s *Server) SetProviders(o *OIDC, oidcName string, l *LDAP) {
	s.providers.Store(&providers{oidc: o, oidcName: oidcName, ldap: l})
}

func (s *Server) current() *providers { return s.providers.Load() }

// NewServer returns a Server; at least one sign-in method must be on.
func NewServer(cfg ServerConfig) (*Server, error) {
	if !cfg.Token && cfg.OIDC == nil && cfg.LDAP == nil && cfg.Passwords == nil {
		return nil, errors.New("turn on at least one sign-in method: token, OIDC or LDAP")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{
		cfg:     cfg,
		ipLimit: newLimiter(loginTries*3, loginWindow),
		idLimit: newLimiter(loginTries, loginWindow),
	}
	s.SetProviders(cfg.OIDC, cfg.OIDCName, cfg.LDAP)
	return s, nil
}

// Method is a sign-in option for the sign-in page.
type Method struct {
	// Type is token, oidc, ldap or password (a Hub's local users).
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// Methods lists the sign-in options, SSO first.
func (s *Server) Methods() []Method {
	var out []Method
	p := s.current()
	if p.oidc != nil {
		out = append(out, Method{Type: "oidc", Name: p.oidcName})
	}
	if p.ldap != nil {
		out = append(out, Method{Type: "ldap", Name: "LDAP"})
	}
	if s.cfg.Passwords != nil {
		out = append(out, Method{Type: "password", Name: "Glowpod"})
	}
	if s.cfg.Token {
		out = append(out, Method{Type: "token"})
	}
	return out
}

// MountRoot implements Authenticator: the SSO redirects. They're always
// there, and answer only while SSO is set up.
func (s *Server) MountRoot(r chi.Router) {
	r.Get(OIDCLoginPath, s.oidcLogin)
	r.Get(OIDCCallbackPath, s.oidcCallback)
}

// MountAPI implements Authenticator.
func (s *Server) MountAPI(r chi.Router) {
	r.Get("/auth/methods", func(w http.ResponseWriter, _ *http.Request) {
		writeOK(w, map[string][]Method{"methods": s.Methods()})
	})
	if s.cfg.Token {
		r.Post("/auth/token", s.loginToken)
	}
	r.Post("/auth/ldap", s.loginLDAP)
	if s.cfg.Passwords != nil {
		r.Post("/auth/password", s.loginPassword)
	}
	r.With(s.Require, RequireCSRF).Post("/auth/logout", s.logout)
}

// Require implements Authenticator.
func (s *Server) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		var sess *Session
		ok := false
		if err == nil {
			sess, ok = s.cfg.Store.Get(r.Context(), c.Value)
		}
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
			return
		}
		id := sess.Identity
		if sess.refresh != nil {
			fresh, err := sess.refresh()
			if err != nil {
				s.cfg.Store.Delete(r.Context(), c.Value)
				writeJSONError(w, http.StatusUnauthorized, "unauthenticated", "Your sign-in expired. Sign in again.")
				return
			}
			id = fresh
		}
		ctx := WithIdentity(r.Context(), id)
		ctx = WithCSRFToken(ctx, sess.CSRF)
		ctx = WithSessionInfo(ctx, sess.Info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// begin starts a session for a signed-in user and sets its cookie.
func (s *Server) begin(w http.ResponseWriter, r *http.Request, sess *Session) error {
	id, err := s.cfg.Store.Create(r.Context(), sess)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the config; it's off only with -insecure-cookies, for plain-HTTP tests
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(s.cfg.Store.Absolute.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	s.cfg.Logger.Info("signed in", "method", sess.Info.Method, "identity", sess.Identity.Key,
		"user", sess.Info.User, "ip", clientIP(r, s.cfg.TrustedProxies))
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		if sess, ok := s.cfg.Store.Delete(r.Context(), c.Value); ok {
			s.cfg.Logger.Info("signed out", "identity", sess.Identity.Key)
			if s.cfg.Forget != nil && sess.Identity.Token != "" {
				s.cfg.Forget(sess.Identity)
			}
		}
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // as in begin
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// loginRequest guards a sign-in POST: same origin, JSON (which a plain
// cross-site form can't send), within the rate limit.
func (s *Server) loginRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !sameOrigin(r) || ct != "application/json" {
		writeJSONError(w, http.StatusForbidden, "csrf", "This sign-in didn't come from Glowpod. Reload the page and try again.")
		return false
	}
	if !s.ipLimit.allow(clientIP(r, s.cfg.TrustedProxies)) {
		writeJSONError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many sign-in attempts. Wait a few minutes and try again.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_body", "The sign-in request isn't valid.")
		return false
	}
	return true
}

type tokenLogin struct {
	Token string `json:"token"`
}

func (s *Server) loginToken(w http.ResponseWriter, r *http.Request) {
	var req tokenLogin
	if !s.loginRequest(w, r, &req) {
		return
	}
	token := strings.TrimSpace(req.Token)
	token = strings.TrimPrefix(token, "Bearer ")
	if token == "" || len(token) > maxTokenLen || strings.ContainsAny(token, " \t\r\n") {
		writeJSONError(w, http.StatusBadRequest, "bad_token", "Paste a single token, without spaces.")
		return
	}
	id := cluster.TokenIdentity(token)
	if s.idLimit.blocked(id.Key) {
		writeJSONError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), loginTimeout)
	defer cancel()
	user, groups, err := s.cfg.WhoAmI(ctx, id)
	if err != nil {
		if apierrors.IsUnauthorized(err) {
			s.idLimit.fail(id.Key)
			writeJSONError(w, http.StatusUnauthorized, "token_rejected", "The cluster didn't accept that token. It may be wrong or expired.")
			return
		}
		s.cfg.Logger.Warn("token sign-in failed", "err", err)
		writeJSONError(w, http.StatusBadGateway, "cluster_error", "Couldn't check the token with the cluster. Try again.")
		return
	}
	sess := &Session{Identity: id, Info: SessionInfo{Mode: "in-cluster", Method: "token", User: user, Groups: groups}}
	if err := s.begin(w, r, sess); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "session_failed", "Couldn't start a session.")
		return
	}
	writeOK(w, sess.Info)
}

// writeOK sends v as a 200 JSON response.
func writeOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}
