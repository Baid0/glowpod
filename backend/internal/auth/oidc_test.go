package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"

	"glowpod/internal/cluster"
)

// fakeIDP is a minimal OpenID Connect provider: discovery, keys and a
// token endpoint that checks PKCE and signs real ID tokens.
type fakeIDP struct {
	t      *testing.T
	srv    *httptest.Server
	signer jose.Signer
	keys   jose.JSONWebKeySet

	mu         sync.Mutex
	challenges map[string]string // code → PKCE challenge
	nonces     map[string]string // code → nonce
	claims     map[string]any
	nonce      string // overrides the nonce when set
	refreshes  int
	expiresIn  int
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIDP{
		t: t, signer: signer,
		keys:       jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}},
		challenges: map[string]string{}, nonces: map[string]string{},
		claims:    map[string]any{"email": "alice@example.com", "email_verified": true, "groups": []string{"devs", "oncall"}},
		expiresIn: 3600,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(f.keys) })
	mux.HandleFunc("/token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// authorize plays the provider's sign-in page: it accepts the request and
// hands back a code for it.
func (f *fakeIDP) authorize(authURL string) (code, state string) {
	u, err := url.Parse(authURL)
	if err != nil {
		f.t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "glowpod" {
		f.t.Fatalf("auth request = %s", authURL)
	}
	code = "code-" + q.Get("state")[:8]
	f.mu.Lock()
	f.challenges[code] = q.Get("code_challenge")
	f.nonces[code] = q.Get("nonce")
	f.mu.Unlock()
	return code, q.Get("state")
}

func (f *fakeIDP) idToken(nonce string) string {
	f.mu.Lock()
	claims := map[string]any{
		"iss": f.srv.URL, "aud": "glowpod", "sub": "u-1",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": nonce,
		"refreshes": f.refreshes,
	}
	for k, v := range f.claims {
		claims[k] = v
	}
	if f.nonce != "" {
		claims["nonce"] = f.nonce
	}
	f.mu.Unlock()
	payload, _ := json.Marshal(claims)
	sig, err := f.signer.Sign(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := sig.CompactSerialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var nonce string
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		f.mu.Lock()
		challenge, ok := f.challenges[r.Form.Get("code")]
		nonce = f.nonces[r.Form.Get("code")]
		delete(f.challenges, r.Form.Get("code"))
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
	case "refresh_token":
		f.mu.Lock()
		f.refreshes++
		f.mu.Unlock()
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": f.expiresIn,
		"refresh_token": "rt", "id_token": f.idToken(nonce),
	})
}

type ssoEnv struct {
	idp    *fakeIDP
	router http.Handler
	seen   []cluster.Identity
}

func newSSOEnv(t *testing.T, pass bool) *ssoEnv {
	t.Helper()
	env := &ssoEnv{idp: newFakeIDP(t)}
	o, err := NewOIDC(OIDCConfig{
		Issuer: env.idp.srv.URL, ClientID: "glowpod", ClientSecret: "s3cret",
		RedirectURL:    "https://glowpod.example.com" + OIDCCallbackPath,
		UsernamePrefix: "oidc:", GroupsPrefix: "oidc:", PassToken: pass,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(ServerConfig{Store: NewStore(time.Hour, 12*time.Hour), SecureCookies: true, OIDC: o, OIDCName: "Okta"})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	srv.MountRoot(r)
	r.Route("/api/v1", func(r chi.Router) {
		srv.MountAPI(r)
		r.With(srv.Require).Get("/session", func(w http.ResponseWriter, r *http.Request) {
			id, _ := IdentityFrom(r.Context())
			env.seen = append(env.seen, id)
			writeOK(w, SessionInfoFrom(r.Context()))
		})
	})
	env.router = r
	return env
}

func (e *ssoEnv) get(t *testing.T, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.RemoteAddr = "198.51.100.7:4000"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// signIn runs the redirect dance and returns the callback's response.
func (e *ssoEnv) signIn(t *testing.T, returnTo string) *httptest.ResponseRecorder {
	t.Helper()
	rec := e.get(t, OIDCLoginPath+"?return="+url.QueryEscape(returnTo))
	if rec.Code != http.StatusFound {
		t.Fatalf("login = %d %s", rec.Code, rec.Body)
	}
	binding := rec.Result().Cookies()[0]
	if binding.Name != oidcCookie || binding.SameSite != http.SameSiteLaxMode || !binding.HttpOnly {
		t.Fatalf("binding cookie = %+v", binding)
	}
	code, state := e.idp.authorize(rec.Header().Get("Location"))
	return e.get(t, OIDCCallbackPath+"?code="+code+"&state="+state, binding)
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestOIDCImpersonates(t *testing.T) {
	env := newSSOEnv(t, false)
	if rec := env.get(t, "/api/v1/auth/methods"); !strings.Contains(rec.Body.String(), `{"type":"oidc","name":"Okta"}`) {
		t.Fatalf("methods = %s", rec.Body)
	}
	rec := env.signIn(t, "/workloads/pods?x=1")
	sess := sessionCookie(rec)
	if rec.Code != 200 || sess == nil || sess.SameSite != http.SameSiteStrictMode {
		t.Fatalf("callback = %d %s, session cookie %+v", rec.Code, rec.Body, sess)
	}
	if !strings.Contains(rec.Body.String(), `url=/workloads/pods?x=1`) {
		t.Fatalf("continues to the page asked for: %s", rec.Body)
	}
	if rec := env.get(t, "/api/v1/session", sess); !strings.Contains(rec.Body.String(), `"username":"oidc:alice@example.com"`) {
		t.Fatalf("session = %s", rec.Body)
	}
	id := env.seen[0]
	if id.User != "oidc:alice@example.com" || strings.Join(id.Groups, ",") != "oidc:devs,oidc:oncall" || id.Token != "" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestOIDCPassesTheToken(t *testing.T) {
	env := newSSOEnv(t, true)
	env.idp.expiresIn = 1 // expired straight away: the next request refreshes
	rec := env.signIn(t, "/")
	sess := sessionCookie(rec)
	env.get(t, "/api/v1/session", sess)
	env.get(t, "/api/v1/session", sess)
	if len(env.seen) != 2 || env.seen[0].Token == "" || env.seen[0].User != "" {
		t.Fatalf("identities = %+v", env.seen)
	}
	if env.idp.refreshes == 0 || env.seen[1].Token == env.seen[0].Token {
		t.Fatalf("expired tokens are refreshed (refreshes=%d)", env.idp.refreshes)
	}
}

func TestOIDCRefusals(t *testing.T) {
	expectError := func(t *testing.T, rec *httptest.ResponseRecorder, code string) {
		t.Helper()
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != SignInPage+"?error="+code || sessionCookie(rec) != nil {
			t.Fatalf("got %d → %s, want error %s", rec.Code, rec.Header().Get("Location"), code)
		}
	}
	t.Run("no binding cookie", func(t *testing.T) {
		env := newSSOEnv(t, false)
		rec := env.get(t, OIDCLoginPath)
		code, state := env.idp.authorize(rec.Header().Get("Location"))
		expectError(t, env.get(t, OIDCCallbackPath+"?code="+code+"&state="+state), "sso_expired")
	})
	t.Run("state can't be replayed", func(t *testing.T) {
		env := newSSOEnv(t, false)
		rec := env.get(t, OIDCLoginPath)
		binding := rec.Result().Cookies()[0]
		code, state := env.idp.authorize(rec.Header().Get("Location"))
		if first := env.get(t, OIDCCallbackPath+"?code="+code+"&state="+state, binding); sessionCookie(first) == nil {
			t.Fatal("first use works")
		}
		expectError(t, env.get(t, OIDCCallbackPath+"?code="+code+"&state="+state, binding), "sso_expired")
	})
	t.Run("wrong nonce", func(t *testing.T) {
		env := newSSOEnv(t, false)
		env.idp.nonce = "someone-elses"
		rec := env.signIn(t, "/")
		expectError(t, rec, "sso_failed")
	})
	t.Run("unverified email", func(t *testing.T) {
		env := newSSOEnv(t, false)
		env.idp.claims["email_verified"] = false
		rec := env.signIn(t, "/")
		expectError(t, rec, "sso_no_user")
	})
	t.Run("provider refused", func(t *testing.T) {
		env := newSSOEnv(t, false)
		expectError(t, env.get(t, OIDCCallbackPath+"?error=access_denied&error_description=<script>"), "sso_refused")
	})
	t.Run("provider down", func(t *testing.T) {
		env := newSSOEnv(t, false)
		env.idp.srv.Close()
		expectError(t, env.get(t, OIDCLoginPath), "sso_unavailable")
	})
}

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"/workloads":           "/workloads",
		"/a?b=c":               "/a?b=c",
		"":                     "/",
		"https://evil.example": "/",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"/x\r\nSet-Cookie: a":  "/",
		"/sign-in":             "/",
	} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}
