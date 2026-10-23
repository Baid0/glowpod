package auth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"glowpod/internal/cluster"
)

// OIDC paths, outside /api so the identity provider redirects to them.
const (
	OIDCLoginPath    = "/auth/oidc/login"
	OIDCCallbackPath = "/auth/oidc/callback"
	// SignInPage is the UI's sign-in page; SSO failures land there with ?error=.
	SignInPage = "/sign-in"

	oidcCookie   = "glowpod_oidc"
	loginExpiry  = 10 * time.Minute
	maxPending   = 10000
	exchangeTime = 15 * time.Second
)

// OIDCConfig describes the OpenID Connect provider and how its users map
// to Kubernetes users.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is the public URL of OIDCCallbackPath.
	RedirectURL string
	Scopes      []string
	// UsernameClaim names the claim used as the Kubernetes username (email by default).
	UsernameClaim  string
	GroupsClaim    string
	UsernamePrefix string
	GroupsPrefix   string
	// PassToken sends the user's ID token to the API server, which must
	// trust this issuer and client. Otherwise Glowpod impersonates them.
	PassToken bool
}

// OIDC signs people in with an OpenID Connect provider: authorization
// code with PKCE, a nonce, and state bound to the browser by a cookie.
type OIDC struct {
	cfg OIDCConfig
	now func() time.Time

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	pending  map[string]pendingLogin
}

type pendingLogin struct {
	verifier string
	nonce    string
	binding  string
	returnTo string
	expires  time.Time
}

// NewOIDC prepares SSO. The provider's discovery document is fetched on
// first use, so Glowpod starts even while the provider is unreachable.
func NewOIDC(cfg OIDCConfig) (*OIDC, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, errors.New("OIDC needs an issuer, a client ID and a redirect URL")
	}
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "email"
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	return &OIDC{cfg: cfg, now: time.Now, pending: map[string]pendingLogin{}}, nil
}

// Check reaches the provider and reads its discovery document: a settings
// page's "Check".
func (o *OIDC) Check(ctx context.Context) error {
	_, _, err := o.setup(ctx)
	return err
}

// setup discovers the provider once.
func (o *OIDC) setup(ctx context.Context) (*oidc.IDTokenVerifier, *oauth2.Config, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.verifier != nil {
		return o.verifier, o.oauth, nil
	}
	p, err := oidc.NewProvider(ctx, o.cfg.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("discover OIDC provider %s: %w", o.cfg.Issuer, err)
	}
	o.verifier = p.Verifier(&oidc.Config{ClientID: o.cfg.ClientID})
	o.oauth = &oauth2.Config{
		ClientID:     o.cfg.ClientID,
		ClientSecret: o.cfg.ClientSecret,
		RedirectURL:  o.cfg.RedirectURL,
		Endpoint:     p.Endpoint(),
		Scopes:       o.cfg.Scopes,
	}
	return o.verifier, o.oauth, nil
}

func (o *OIDC) remember(state string, p pendingLogin) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now()
	for k, v := range o.pending {
		if now.After(v.expires) {
			delete(o.pending, k)
		}
	}
	if len(o.pending) >= maxPending {
		return // under a flood, new logins wait; existing sessions are unaffected
	}
	o.pending[state] = p
}

// take returns a pending login once: a state can't be replayed.
func (o *OIDC) take(state string) (pendingLogin, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.pending[state]
	delete(o.pending, state)
	if !ok || o.now().After(p.expires) {
		return pendingLogin{}, false
	}
	return p, true
}

// localPath keeps the post-sign-in redirect on this site.
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") ||
		strings.ContainsAny(p, "\r\n\\") || strings.HasPrefix(p, SignInPage) {
		return "/"
	}
	return p
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	o := s.current().oidc
	if o == nil {
		redirectSignIn(w, r, "sso_unavailable")
		return
	}
	if !s.ipLimit.allow(clientIP(r, s.cfg.TrustedProxies)) {
		redirectSignIn(w, r, "too_many_attempts")
		return
	}
	_, conf, err := o.setup(r.Context())
	if err != nil {
		s.cfg.Logger.Warn("SSO provider unavailable", "err", err)
		redirectSignIn(w, r, "sso_unavailable")
		return
	}
	state, err1 := randomSecret()
	nonce, err2 := randomSecret()
	binding, err3 := randomSecret()
	if err := errors.Join(err1, err2, err3); err != nil {
		redirectSignIn(w, r, "sso_failed")
		return
	}
	verifier := oauth2.GenerateVerifier()
	o.remember(state, pendingLogin{
		verifier: verifier, nonce: nonce, binding: binding,
		returnTo: localPath(r.URL.Query().Get("return")), expires: o.now().Add(loginExpiry),
	})
	// Lax, not Strict: it must come back on the provider's redirect.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the config, as for the session cookie
		Name: oidcCookie, Value: binding, Path: "/auth/oidc", MaxAge: int(loginExpiry.Seconds()),
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, conf.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	o := s.current().oidc
	if o == nil {
		redirectSignIn(w, r, "sso_unavailable")
		return
	}
	q := r.URL.Query()
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // clearing the binding cookie
		Name: oidcCookie, Value: "", Path: "/auth/oidc", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	if q.Get("error") != "" {
		// The provider's own text isn't echoed: it could be anything.
		s.cfg.Logger.Info("SSO refused", "error", q.Get("error"))
		redirectSignIn(w, r, "sso_refused")
		return
	}
	p, ok := o.take(q.Get("state"))
	c, err := r.Cookie(oidcCookie)
	if !ok || err != nil || !equal(c.Value, p.binding) {
		redirectSignIn(w, r, "sso_expired")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), exchangeTime)
	defer cancel()
	verifier, conf, err := o.setup(ctx)
	if err != nil {
		redirectSignIn(w, r, "sso_unavailable")
		return
	}
	tok, err := conf.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(p.verifier))
	if err != nil {
		s.cfg.Logger.Warn("SSO code exchange failed", "err", err)
		redirectSignIn(w, r, "sso_failed")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idToken, err := verifier.Verify(ctx, raw)
	if err != nil || idToken.Nonce != p.nonce {
		s.cfg.Logger.Warn("SSO ID token rejected", "err", err)
		redirectSignIn(w, r, "sso_failed")
		return
	}
	user, groups, err := o.user(idToken)
	if err != nil {
		s.cfg.Logger.Info("SSO user not accepted", "reason", err)
		redirectSignIn(w, r, "sso_no_user")
		return
	}

	sess := &Session{Info: SessionInfo{Mode: "in-cluster", Method: "oidc", User: user, Groups: groups}}
	if o.cfg.PassToken {
		sess.Identity = cluster.TokenIdentity(raw)
		sess.refresh = o.refresher(context.WithoutCancel(r.Context()), conf.TokenSource(context.WithoutCancel(r.Context()), tok), verifier, raw)
	} else {
		sess.Identity = cluster.UserIdentity(user, groups)
	}
	if err := s.begin(w, r, sess); err != nil {
		redirectSignIn(w, r, "sso_failed")
		return
	}
	// The session cookie is SameSite=Strict, and this response ends a
	// navigation that started on the provider's site, so a redirect would
	// arrive without it. A page that navigates on its own is same-site.
	continueTo(w, p.returnTo)
}

// user maps the ID token's claims to a Kubernetes username and groups.
func (o *OIDC) user(t *oidc.IDToken) (string, []string, error) {
	var claims map[string]any
	if err := t.Claims(&claims); err != nil {
		return "", nil, fmt.Errorf("read claims: %w", err)
	}
	name, _ := claims[o.cfg.UsernameClaim].(string)
	if name == "" {
		return "", nil, fmt.Errorf("no %q claim", o.cfg.UsernameClaim)
	}
	// An unverified email could be anyone's; Kubernetes refuses those too.
	if o.cfg.UsernameClaim == "email" {
		if verified, ok := claims["email_verified"].(bool); ok && !verified {
			return "", nil, errors.New("email not verified")
		}
	}
	var groups []string
	switch g := claims[o.cfg.GroupsClaim].(type) {
	case []any:
		for _, v := range g {
			if s, ok := v.(string); ok && s != "" {
				groups = append(groups, o.cfg.GroupsPrefix+s)
			}
		}
	case string:
		if g != "" {
			groups = append(groups, o.cfg.GroupsPrefix+g)
		}
	}
	user := o.cfg.UsernamePrefix + name
	if o.cfg.PassToken {
		return user, groups, nil // the API server decides; nothing is impersonated
	}
	groups, err := impersonable(user, groups)
	return user, groups, err
}

// refresher returns the current ID token, refreshing it when it expires.
func (o *OIDC) refresher(ctx context.Context, ts oauth2.TokenSource, v *oidc.IDTokenVerifier, raw string) func() (cluster.Identity, error) {
	var mu sync.Mutex
	current := raw
	return func() (cluster.Identity, error) {
		mu.Lock()
		defer mu.Unlock()
		tok, err := ts.Token()
		if err != nil {
			return cluster.Identity{}, fmt.Errorf("refresh SSO token: %w", err)
		}
		if next, _ := tok.Extra("id_token").(string); next != "" && next != current {
			if _, err := v.Verify(ctx, next); err != nil {
				return cluster.Identity{}, fmt.Errorf("verify refreshed ID token: %w", err)
			}
			current = next
		}
		return cluster.TokenIdentity(current), nil
	}
}

func redirectSignIn(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, SignInPage+"?error="+url.QueryEscape(code), http.StatusSeeOther)
}

func continueTo(w http.ResponseWriter, path string) {
	target := html.EscapeString(path)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s">`+
		`<title>Signing in…</title><p>Signed in. <a href="%s">Continue to Glowpod</a>.</p>`, target, target)
}
