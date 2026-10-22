package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ldap/ldap/v3"
)

// fakeDir is a tiny directory: two users, one group, and a record of what
// Glowpod asked.
type fakeDir struct {
	binds    []string
	filters  []string
	startTLS bool
	closed   bool
}

var dirUsers = map[string]struct{ uid, password string }{
	"uid=alice,ou=people,dc=example,dc=com": {"alice", "correct horse"},
	"uid=bob,ou=people,dc=example,dc=com":   {"bob", "battery staple"},
}

func (d *fakeDir) Bind(dn, password string) error {
	d.binds = append(d.binds, dn)
	if dn == "cn=glowpod,dc=example,dc=com" && password == "svc" {
		return nil
	}
	if u, ok := dirUsers[dn]; ok && u.password == password {
		return nil
	}
	return ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("invalid credentials"))
}

func (d *fakeDir) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	d.filters = append(d.filters, req.Filter)
	res := &ldap.SearchResult{}
	switch req.BaseDN {
	case "ou=people,dc=example,dc=com":
		if req.Filter == "(objectClass=person)" { // matches everyone
			return nil, ldap.NewError(ldap.LDAPResultSizeLimitExceeded, errors.New("size limit"))
		}
		for dn, u := range dirUsers {
			if req.Filter == "(uid="+u.uid+")" {
				res.Entries = append(res.Entries, ldap.NewEntry(dn, map[string][]string{"uid": {u.uid}}))
			}
		}
	case "ou=groups,dc=example,dc=com":
		if req.Filter == "(member=uid=alice,ou=people,dc=example,dc=com)" {
			res.Entries = append(res.Entries,
				ldap.NewEntry("cn=devs,ou=groups,dc=example,dc=com", map[string][]string{"cn": {"devs"}}),
				ldap.NewEntry("cn=oncall,ou=groups,dc=example,dc=com", map[string][]string{"cn": {"oncall"}}))
		}
	}
	return res, nil
}

func (d *fakeDir) StartTLS(*tls.Config) error { d.startTLS = true; return nil }
func (d *fakeDir) Close() error               { d.closed = true; return nil }

func testLDAP(t *testing.T, mod func(*LDAPConfig)) (*LDAP, *fakeDir) {
	t.Helper()
	cfg := LDAPConfig{
		URL: "ldap://ldap.example.com", BindDN: "cn=glowpod,dc=example,dc=com", BindPassword: "svc",
		UserBase: "ou=people,dc=example,dc=com", GroupBase: "ou=groups,dc=example,dc=com",
		GroupsPrefix: "ldap:", StartTLS: true,
	}
	if mod != nil {
		mod(&cfg)
	}
	l, err := NewLDAP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := &fakeDir{}
	l.dial = func(context.Context) (ldapConn, error) { return dir, nil }
	return l, dir
}

func TestLDAPAuthenticate(t *testing.T) {
	l, dir := testLDAP(t, nil)
	user, groups, err := l.Authenticate(context.Background(), "alice", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" || strings.Join(groups, ",") != "ldap:devs,ldap:oncall" {
		t.Fatalf("user = %q groups = %v", user, groups)
	}
	want := "cn=glowpod,dc=example,dc=com,uid=alice,ou=people,dc=example,dc=com,cn=glowpod,dc=example,dc=com"
	if !dir.startTLS || !dir.closed || strings.Join(dir.binds, ",") != want {
		t.Fatalf("service bind, user bind, service bind again for groups: %v (tls %v)", dir.binds, dir.startTLS)
	}
}

func TestLDAPRefusals(t *testing.T) {
	tests := []struct {
		name, user, password string
		mod                  func(*LDAPConfig)
	}{
		{name: "wrong password", user: "alice", password: "wrong"},
		{name: "unknown user", user: "mallory", password: "x"},
		{name: "empty password is never an anonymous bind", user: "alice", password: ""},
		{name: "filter injection", user: "*)(uid=*", password: "x"},
		{name: "filter matching many users", user: "alice", password: "correct horse", mod: func(c *LDAPConfig) { c.UserFilter = "(objectClass=person)({username})" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, dir := testLDAP(t, tt.mod)
			if tt.name == "filter matching many users" {
				l.cfg.UserFilter = "(objectClass=person)"
			}
			_, _, err := l.Authenticate(context.Background(), tt.user, tt.password)
			if !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("err = %v, want bad credentials", err)
			}
			if tt.password == "" && len(dir.binds) != 0 {
				t.Fatal("an empty password must not reach the directory")
			}
			if tt.name == "filter injection" && dir.filters[0] != `(uid=\2a\29\28uid=\2a)` {
				t.Fatalf("filter = %q, want the username escaped", dir.filters[0])
			}
		})
	}
}

func TestLDAPDirectoryDown(t *testing.T) {
	l, _ := testLDAP(t, nil)
	l.dial = func(context.Context) (ldapConn, error) { return nil, errors.New("connection refused") }
	if _, _, err := l.Authenticate(context.Background(), "alice", "correct horse"); err == nil || errors.Is(err, ErrBadCredentials) {
		t.Fatalf("err = %v, want a connection error", err)
	}
}

func TestLDAPConfig(t *testing.T) {
	if _, err := NewLDAP(LDAPConfig{URL: "ldap://x"}); err == nil {
		t.Fatal("needs a user base")
	}
	if _, err := NewLDAP(LDAPConfig{URL: "ldap://x", UserBase: "dc=x", UserFilter: "(uid=alice)"}); err == nil {
		t.Fatal("the filter must use {username}")
	}
}

func TestLDAPSignIn(t *testing.T) {
	l, _ := testLDAP(t, nil)
	srv, err := NewServer(ServerConfig{Store: NewStore(time.Hour, time.Hour), SecureCookies: true, LDAP: l})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		srv.MountAPI(r)
		r.With(srv.Require).Get("/session", func(w http.ResponseWriter, r *http.Request) {
			id, _ := IdentityFrom(r.Context())
			writeOK(w, map[string]any{"user": id.User, "groups": id.Groups, "token": id.Token})
		})
	})
	env := &serverEnv{router: r}

	ok := env.do(t, http.MethodPost, "/api/v1/auth/ldap", `{"username":"alice","password":"correct horse"}`, jsonSameOrigin, nil)
	if ok.Code != 200 || len(ok.Result().Cookies()) != 1 {
		t.Fatalf("sign-in = %d %s", ok.Code, ok.Body)
	}
	rec := env.do(t, http.MethodGet, "/api/v1/session", "", nil, ok.Result().Cookies()[0])
	if rec.Body.String() != `{"groups":["ldap:devs","ldap:oncall"],"token":"","user":"alice"}`+"\n" {
		t.Fatalf("impersonates the directory user: %s", rec.Body)
	}

	wrongPass := env.do(t, http.MethodPost, "/api/v1/auth/ldap", `{"username":"alice","password":"nope"}`, jsonSameOrigin, nil)
	noUser := env.do(t, http.MethodPost, "/api/v1/auth/ldap", `{"username":"mallory","password":"nope"}`, jsonSameOrigin, nil)
	if wrongPass.Code != 401 || noUser.Code != 401 || wrongPass.Body.String() != noUser.Body.String() {
		t.Fatalf("wrong password and unknown user look the same: %d %s / %d %s", wrongPass.Code, wrongPass.Body, noUser.Code, noUser.Body)
	}
	if rec := env.do(t, http.MethodPost, "/api/v1/auth/ldap", `{"username":"alice","password":""}`, jsonSameOrigin, nil); rec.Code != 400 {
		t.Fatalf("empty password = %d", rec.Code)
	}
}

func TestLDAPSystemUser(t *testing.T) {
	l, _ := testLDAP(t, func(c *LDAPConfig) { c.GroupsPrefix = "" })
	_, groups, err := l.Authenticate(context.Background(), "alice", "correct horse")
	if err != nil || strings.Join(groups, ",") != "devs,oncall" {
		t.Fatalf("groups = %v err = %v", groups, err)
	}
	if got, err := impersonable("system:kube-controller-manager", nil); err == nil {
		t.Fatalf("got %v", got)
	}
}
