package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"glowpod/internal/cluster"
)

const goodToken = "eyJhbGciOiJSUzI1NiJ9.good"

type serverEnv struct {
	router  http.Handler
	forgot  []cluster.Identity
	reached []cluster.Identity
}

func newServerEnv(t *testing.T) *serverEnv {
	t.Helper()
	env := &serverEnv{}
	srv, err := NewServer(ServerConfig{
		Store:         NewStore(time.Hour, 12*time.Hour),
		Token:         true,
		SecureCookies: true,
		WhoAmI: func(_ context.Context, id cluster.Identity) (string, []string, error) {
			if id.Token != goodToken {
				return "", nil, apierrors.NewUnauthorized("bad token")
			}
			return "system:serviceaccount:shop:viewer", []string{"system:serviceaccounts"}, nil
		},
		Forget: func(id cluster.Identity) { env.forgot = append(env.forgot, id) },
	})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	srv.MountRoot(r)
	r.Route("/api/v1", func(r chi.Router) {
		srv.MountAPI(r)
		r.With(srv.Require, RequireCSRF).Get("/session", func(w http.ResponseWriter, r *http.Request) {
			id, _ := IdentityFrom(r.Context())
			env.reached = append(env.reached, id)
			writeOK(w, map[string]any{"info": SessionInfoFrom(r.Context()), "csrf": CSRFTokenFrom(r.Context())})
		})
	})
	env.router = r
	return env
}

func (e *serverEnv) do(t *testing.T, method, path, body string, header map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Host = "glowpod.example.com"
	req.RemoteAddr = "198.51.100.7:4000"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

var jsonSameOrigin = map[string]string{"Content-Type": "application/json", "Origin": "https://glowpod.example.com"}

func TestTokenSignIn(t *testing.T) {
	env := newServerEnv(t)

	rec := env.do(t, http.MethodGet, "/api/v1/auth/methods", "", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"token"`) {
		t.Fatalf("methods = %d %s", rec.Code, rec.Body)
	}
	if rec := env.do(t, http.MethodGet, "/api/v1/session", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d", rec.Code)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/auth/token", `{"token":"Bearer `+goodToken+`\n"}`, jsonSameOrigin, nil)
	if rec.Code != 200 {
		t.Fatalf("sign-in = %d %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %+v", cookies)
	}
	if strings.Contains(cookies[0].Value, goodToken) || strings.Contains(rec.Body.String(), goodToken) {
		t.Fatal("the token never goes back to the browser")
	}

	rec = env.do(t, http.MethodGet, "/api/v1/session", "", nil, cookies[0])
	var got struct {
		Info SessionInfo
		CSRF string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got.Info.User != "system:serviceaccount:shop:viewer" || got.Info.Method != "token" || got.CSRF == "" {
		t.Fatalf("session = %d %+v", rec.Code, got)
	}
	if id := env.reached[0]; id.Token != goodToken || id.User != "" {
		t.Fatalf("requests act with the user's own token: %+v", id)
	}

	// Sign-out needs the CSRF token, then ends the session.
	if rec := env.do(t, http.MethodPost, "/api/v1/auth/logout", "", nil, cookies[0]); rec.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf = %d", rec.Code)
	}
	rec = env.do(t, http.MethodPost, "/api/v1/auth/logout", "", map[string]string{CSRFHeader: got.CSRF}, cookies[0])
	if rec.Code != http.StatusNoContent || len(env.forgot) != 1 {
		t.Fatalf("logout = %d, forgot %d", rec.Code, len(env.forgot))
	}
	if rec := env.do(t, http.MethodGet, "/api/v1/session", "", nil, cookies[0]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after logout = %d", rec.Code)
	}
}

func TestTokenSignInRefusals(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		header map[string]string
		want   int
		code   string
	}{
		{"wrong token", `{"token":"nope"}`, jsonSameOrigin, http.StatusUnauthorized, "token_rejected"},
		{"empty", `{"token":"  "}`, jsonSameOrigin, http.StatusBadRequest, "bad_token"},
		{"two tokens", `{"token":"a b"}`, jsonSameOrigin, http.StatusBadRequest, "bad_token"},
		{"other site", `{"token":"x"}`, map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.com"}, http.StatusForbidden, "csrf"},
		{"form post", `token=x`, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusForbidden, "csrf"},
		{"unknown fields", `{"token":"x","admin":true}`, jsonSameOrigin, http.StatusBadRequest, "bad_body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newServerEnv(t).do(t, http.MethodPost, "/api/v1/auth/token", tt.body, tt.header, nil)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.code) || len(rec.Result().Cookies()) != 0 {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestSignInRateLimit(t *testing.T) {
	env := newServerEnv(t)
	var last int
	for range loginTries + 1 {
		last = env.do(t, http.MethodPost, "/api/v1/auth/token", `{"token":"guess"}`, jsonSameOrigin, nil).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d tries the same token is limited, got %d", loginTries+1, last)
	}
}

func TestNeedsAMethod(t *testing.T) {
	if _, err := NewServer(ServerConfig{Store: NewStore(time.Hour, time.Hour)}); err == nil {
		t.Fatal("want an error without sign-in methods")
	}
}

func TestStoreExpiry(t *testing.T) {
	s := NewStore(time.Hour, 3*time.Hour)
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	id, err := s.Create(t.Context(), &Session{Identity: cluster.UserIdentity("alice", nil)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(t.Context(), "not-it"); ok {
		t.Fatal("unknown id")
	}
	for range 4 { // used every 50 minutes: idle never runs out
		now = now.Add(50 * time.Minute)
		if _, ok := s.Get(t.Context(), id); !ok && now.Sub(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) <= 3*time.Hour {
			t.Fatalf("session ended early at %v", now)
		}
	}
	if _, ok := s.Get(t.Context(), id); ok {
		t.Fatal("the absolute limit ends it")
	}

	id2, _ := s.Create(t.Context(), &Session{})
	now = now.Add(61 * time.Minute)
	if _, ok := s.Get(t.Context(), id2); ok {
		t.Fatal("idle sessions end")
	}
	for k := range s.m {
		if k == id || k == id2 {
			t.Fatal("the store keeps hashes, not session ids")
		}
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(2, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	first, second, third := l.allow("a"), l.allow("a"), l.allow("a")
	if !first || !second || third {
		t.Fatal("two per minute")
	}
	if !l.allow("b") {
		t.Fatal("keys are separate")
	}
	now = now.Add(61 * time.Second)
	if !l.allow("a") {
		t.Fatal("the window slides")
	}
}

func TestClientIP(t *testing.T) {
	ingress := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	tests := []struct {
		name, remote, xff string
		trusted           []netip.Prefix
		want              string
	}{
		{"no proxy trusted: the header is ignored", "203.0.113.9:5000", "198.51.100.1", nil, "203.0.113.9"},
		{"from the ingress", "10.1.2.3:5000", "198.51.100.1", ingress, "198.51.100.1"},
		{"a spoofed hop before the real one", "10.1.2.3:5000", "6.6.6.6, 198.51.100.1", ingress, "198.51.100.1"},
		{"through two trusted hops", "10.1.2.3:5000", "198.51.100.1, 10.9.9.9", ingress, "198.51.100.1"},
		{"untrusted sender can't spoof", "203.0.113.9:5000", "10.1.1.1", ingress, "203.0.113.9"},
		{"ingress without the header", "10.1.2.3:5000", "", ingress, "10.1.2.3"},
		{"ipv6", "[2001:db8::1]:5000", "", ingress, "2001:db8::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(r, tt.trusted); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
