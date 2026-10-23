package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/auth"
)

func setupDone(t *testing.T, th *testHub) {
	t.Helper()
	if err := th.store.saveSettings(t.Context(), settings{ServerURL: "https://hub.example.com", SetupDone: true}); err != nil {
		t.Fatal(err)
	}
}

func TestGlowpodAccounts(t *testing.T) {
	th := newTestHub(t)
	setupDone(t, th)
	var ended []string
	var mu sync.Mutex
	th.cfg.EndSessions = func(_ context.Context, user string) {
		mu.Lock()
		ended = append(ended, user)
		mu.Unlock()
	}
	th.as = admin

	tests := []struct {
		name, method, path string
		body               any
		as                 auth.SessionInfo
		want               int
	}{
		{"an admin adds a user", http.MethodPost, "/api/v1/hub/users", userRequest{Username: "bob", Role: RoleUser, Password: "a long enough password"}, admin, 204},
		{"a short password", http.MethodPost, "/api/v1/hub/users", userRequest{Username: "eve", Role: RoleUser, Password: "short"}, admin, 400},
		{"a username a Secret can't hold", http.MethodPost, "/api/v1/hub/users", userRequest{Username: "Bob_1", Role: RoleUser, Password: "a long enough password"}, admin, 400},
		{"a role that isn't one", http.MethodPost, "/api/v1/hub/users", userRequest{Username: "eve", Role: "root", Password: "a long enough password"}, admin, 400},
		{"the same name again", http.MethodPost, "/api/v1/hub/users", userRequest{Username: "bob", Role: RoleUser, Password: "a long enough password"}, admin, 409},
		{"a user can't add users", http.MethodPost, "/api/v1/hub/users", userRequest{Username: "eve", Role: RoleAdmin, Password: "a long enough password"}, auth.SessionInfo{User: "glowpod:bob", Method: "password"}, 403},
		{"admin stays an administrator", http.MethodPut, "/api/v1/hub/users/admin", userRequest{Role: RoleUser}, admin, 400},
		{"admin can't be deleted", http.MethodDelete, "/api/v1/hub/users/admin", nil, admin, 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th.as = tt.as
			if rec := th.do(t, tt.method, tt.path, tt.body); rec.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}

	ctx := t.Context()
	bob := auth.SessionInfo{User: "glowpod:bob", Method: "password"}
	if u, err := th.CheckPassword(ctx, "bob", "a long enough password"); err != nil || u != "glowpod:bob" {
		t.Fatalf("bob signs in: %q %v", u, err)
	}
	if th.Role(bob) != RoleUser {
		t.Fatal("bob is a user")
	}

	// Making bob an administrator takes effect at once, and signs him out.
	th.as = admin
	if rec := th.do(t, http.MethodPut, "/api/v1/hub/users/bob", userRequest{Role: RoleAdmin}); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if th.Role(bob) != RoleAdmin || !slices.Contains(ended, "glowpod:bob") {
		t.Fatalf("role %s, ended %v", th.Role(bob), ended)
	}

	// Bob changes his own password.
	th.as = bob
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/account/password", map[string]string{"current": "wrong", "new": "another long password"}); rec.Code != 400 {
		t.Fatal("the current password is checked")
	}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/account/password", map[string]string{"current": "a long enough password", "new": "another long password"}); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if _, err := th.CheckPassword(ctx, "bob", "another long password"); err != nil {
		t.Fatal("the new password works")
	}
	th.as = auth.SessionInfo{User: "oidc:ann", Method: "oidc"}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/account/password", map[string]string{"current": "x", "new": "another long password"}); rec.Code != 400 {
		t.Fatal("an SSO user has no Glowpod password")
	}

	// Bob's kubeconfig ends with his account.
	if _, _, err := th.newKubeToken(ctx, bob, tokenScope{}); err != nil {
		t.Fatal(err)
	}
	th.as = admin
	if rec := th.do(t, http.MethodDelete, "/api/v1/hub/users/bob", nil); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if _, err := th.CheckPassword(ctx, "bob", "another long password"); err == nil {
		t.Fatal("a deleted account can't sign in")
	}
	if left, _ := th.myKubeTokens(ctx, "glowpod:bob"); len(left) != 0 {
		t.Fatal("a deleted account's kubeconfigs end")
	}
	if th.Role(bob) != RoleUser {
		t.Fatal("no longer an administrator")
	}

	rec := th.do(t, http.MethodGet, "/api/v1/hub/users", nil)
	var list struct{ Users []User }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Users) != 0 {
		t.Fatalf("users = %+v (admin is made at setup, not here)", list.Users)
	}
}

// providers records what the Hub hands the sign-in server.
type providers struct {
	mu   sync.Mutex
	oidc *auth.OIDC
	name string
	ldap *auth.LDAP
}

func (p *providers) set(o *auth.OIDC, name string, l *auth.LDAP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.oidc, p.name, p.ldap = o, name, l
}

func TestSignInSettings(t *testing.T) {
	th := newTestHub(t)
	setupDone(t, th)
	got := &providers{}
	th.UseAuth(t.Context(), got.set, FixedAuth{})
	th.as = admin

	oidcSettings := OIDCSettings{
		Enabled: true, Name: "Okta", Issuer: "https://example.okta.com", ClientID: "glowpod", ClientSecret: "s3cret",
		Scopes: []string{"openid", "email"}, UsernameClaim: "email", GroupsClaim: "groups",
	}
	if rec := th.do(t, http.MethodPut, "/api/v1/hub/settings/oidc", oidcSettings); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if got.oidc == nil || got.name != "Okta" {
		t.Fatal("SSO takes effect at once")
	}
	ldapSettings := LDAPSettings{Enabled: true, URL: "ldap://ldap.example.com", BindDN: "cn=glowpod", BindPassword: "bindpw", UserBase: "ou=people,dc=example,dc=com", UserFilter: "(uid={username})", UsernameAttribute: "uid"}
	if rec := th.do(t, http.MethodPut, "/api/v1/hub/settings/ldap", ldapSettings); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if got.ldap == nil {
		t.Fatal("LDAP takes effect at once")
	}

	// Secrets never come back, and an empty one keeps the kept one.
	rec := th.do(t, http.MethodGet, "/api/v1/hub/settings", nil)
	if strings.Contains(rec.Body.String(), "s3cret") || strings.Contains(rec.Body.String(), "bindpw") {
		t.Fatal("secrets in the answer")
	}
	var s settingsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if !s.Auth.OIDC.ClientSecretSet || !s.Auth.LDAP.BindPasswordSet || s.RedirectURL != "https://hub.example.com/auth/oidc/callback" {
		t.Fatalf("settings = %+v", s)
	}
	oidcSettings.ClientSecret, oidcSettings.Name = "", "Okta SSO"
	if rec := th.do(t, http.MethodPut, "/api/v1/hub/settings/oidc", oidcSettings); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	kept, _ := th.store.authSettings(t.Context())
	if kept.OIDC.ClientSecret != "s3cret" {
		t.Fatal("an empty secret keeps the kept one")
	}
	sec, _ := th.kube.CoreV1().Secrets(ns).Get(t.Context(), authSecret, metav1.GetOptions{})
	if sec == nil {
		t.Fatal("kept in a Secret")
	}

	bad := []struct {
		path string
		body any
	}{
		{"/api/v1/hub/settings/oidc", OIDCSettings{Enabled: true, Issuer: "not a url", ClientID: "x"}},
		{"/api/v1/hub/settings/oidc", OIDCSettings{Enabled: true, Issuer: "https://idp.example.com"}},
		{"/api/v1/hub/settings/ldap", LDAPSettings{Enabled: true, URL: "http://ldap", UserBase: "dc=x"}},
		{"/api/v1/hub/settings/ldap", LDAPSettings{Enabled: true, URL: "ldaps://ldap", UserBase: "dc=x", CA: "not pem"}},
		{"/api/v1/hub/settings/server-url", map[string]string{"serverURL": "https://hub.example.com/path"}},
	}
	for _, b := range bad {
		if rec := th.do(t, http.MethodPut, b.path, b.body); rec.Code != 400 {
			t.Fatalf("%s %+v: %d", b.path, b.body, rec.Code)
		}
	}

	// Turning SSO off takes it away at once.
	oidcSettings.Enabled = false
	if rec := th.do(t, http.MethodPut, "/api/v1/hub/settings/oidc", oidcSettings); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if got.oidc != nil {
		t.Fatal("SSO off")
	}

	// Only administrators.
	th.as = auth.SessionInfo{User: "oidc:ann", Method: "oidc"}
	if rec := th.do(t, http.MethodGet, "/api/v1/hub/settings", nil); rec.Code != 403 {
		t.Fatalf("a user reads settings: %d", rec.Code)
	}
}

func TestFlagsWinOverTheUI(t *testing.T) {
	th := newTestHub(t)
	setupDone(t, th)
	fixed, err := auth.NewLDAP(auth.LDAPConfig{URL: "ldap://from-helm", UserBase: "dc=x"})
	if err != nil {
		t.Fatal(err)
	}
	got := &providers{}
	th.UseAuth(t.Context(), got.set, FixedAuth{LDAP: fixed})
	th.as = admin
	if rec := th.do(t, http.MethodPut, "/api/v1/hub/settings/ldap", LDAPSettings{Enabled: true, URL: "ldap://ui", UserBase: "dc=x"}); rec.Code != http.StatusConflict {
		t.Fatalf("status %d", rec.Code)
	}
	if got.ldap != fixed {
		t.Fatal("the Helm-set LDAP stays")
	}
	var s settingsResponse
	_ = json.Unmarshal(th.do(t, http.MethodGet, "/api/v1/hub/settings", nil).Body.Bytes(), &s)
	if !s.Fixed.LDAP || s.Fixed.OIDC {
		t.Fatalf("fixed = %+v", s.Fixed)
	}
}

func TestCheckOIDC(t *testing.T) {
	var issuer string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token",
			"jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(idp.Close)
	issuer = idp.URL
	th := newTestHub(t)
	setupDone(t, th)
	th.as = admin
	for _, tt := range []struct {
		issuer string
		ok     bool
	}{{issuer, true}, {issuer + "/nope", false}} {
		rec := th.do(t, http.MethodPost, "/api/v1/hub/settings/oidc/check", OIDCSettings{Issuer: tt.issuer, ClientID: "glowpod"})
		var res struct {
			OK      bool
			Message string
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.OK != tt.ok || res.Message == "" {
			t.Fatalf("%s: %+v", tt.issuer, res)
		}
	}
}
