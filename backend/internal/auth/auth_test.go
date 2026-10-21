package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newLocal(t *testing.T) *Local {
	t.Helper()
	l, err := NewLocal()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestHandleLogin(t *testing.T) {
	l := newLocal(t)
	launch, err := url.Parse(l.LaunchURL("http://127.0.0.1:7171"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		query      string
		cookie     string
		wantStatus int
		wantCookie bool
	}{
		{name: "valid token sets cookie", query: launch.RawQuery, wantStatus: http.StatusSeeOther, wantCookie: true},
		{name: "wrong token", query: "token=nope", wantStatus: http.StatusUnauthorized},
		{name: "missing token", query: "", wantStatus: http.StatusUnauthorized},
		{name: "already signed in", query: "", cookie: l.session, wantStatus: http.StatusSeeOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, LoginPath+"?"+tt.query, nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: CookieName, Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			l.HandleLogin(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			cookies := rec.Result().Cookies()
			if tt.wantCookie != (len(cookies) == 1) {
				t.Fatalf("cookies = %v, want one: %v", cookies, tt.wantCookie)
			}
			if tt.wantCookie {
				c := cookies[0]
				if c.Name != CookieName || c.Value != l.session || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
					t.Fatalf("bad cookie: %+v", c)
				}
				if c.Value == l.token {
					t.Fatal("the session cookie must not be the launch token")
				}
				if loc := rec.Header().Get("Location"); loc != "/" {
					t.Fatalf("redirect = %q, want /", loc)
				}
			}
		})
	}
}

func TestRequire(t *testing.T) {
	l := newLocal(t)
	var gotIdentity bool
	h := l.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFrom(r.Context())
		gotIdentity = ok && id == LocalIdentity
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name       string
		auth       *Local
		cookie     string
		wantStatus int
	}{
		{name: "valid session", auth: l, cookie: l.session, wantStatus: http.StatusNoContent},
		{name: "no cookie", auth: l, wantStatus: http.StatusUnauthorized},
		{name: "wrong cookie", auth: l, cookie: "guess", wantStatus: http.StatusUnauthorized},
		{name: "launch token is not a session", auth: l, cookie: l.token, wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIdentity = false
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/session", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: CookieName, Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if (rec.Code == http.StatusNoContent) != gotIdentity {
				t.Fatalf("identity attached = %v", gotIdentity)
			}
			if rec.Code == http.StatusUnauthorized && !strings.Contains(rec.Body.String(), "unauthenticated") {
				t.Fatalf("body = %q", rec.Body.String())
			}
		})
	}
}

func TestDisabledAllowsEverything(t *testing.T) {
	l := NewLocalDisabled()
	h := l.Require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, ok := IdentityFrom(r.Context()); !ok {
			t.Error("identity missing")
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := l.LaunchURL("http://localhost:5173"); got != "http://localhost:5173" {
		t.Fatalf("launch URL = %q", got)
	}
}

func TestRequireHost(t *testing.T) {
	h := RequireHost(LoopbackHosts("7171"))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		host string
		want int
	}{
		{"127.0.0.1:7171", http.StatusNoContent},
		{"localhost:7171", http.StatusNoContent},
		{"LOCALHOST:7171", http.StatusNoContent},
		{"[::1]:7171", http.StatusNoContent},
		{"evil.example.com:7171", http.StatusMisdirectedRequest},
		{"127.0.0.1:9999", http.StatusMisdirectedRequest},
		{"127.0.0.1", http.StatusMisdirectedRequest},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
