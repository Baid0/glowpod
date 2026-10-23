package agent

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"glowpod/internal/tunnel"
)

func TestCheckImpersonation(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string][]string
		ok      bool
	}{
		{name: "local user", headers: map[string][]string{"Impersonate-User": {"glowpod:admin"}}, ok: true},
		{name: "SSO user with groups", headers: map[string][]string{"Impersonate-User": {"oidc:ann@example.com"}, "Impersonate-Group": {"oidc:ops", "system:authenticated"}}, ok: true},
		{name: "LDAP user", headers: map[string][]string{"Impersonate-User": {"ldap:bob"}}, ok: true},
		{name: "no impersonation: the agent never acts as itself", headers: map[string][]string{}},
		{name: "two users", headers: map[string][]string{"Impersonate-User": {"glowpod:a", "glowpod:b"}}},
		{name: "unprefixed user", headers: map[string][]string{"Impersonate-User": {"alice"}}},
		{name: "system user", headers: map[string][]string{"Impersonate-User": {"system:admin"}}},
		{name: "a bare prefix", headers: map[string][]string{"Impersonate-User": {"oidc:"}}},
		{name: "system:masters", headers: map[string][]string{"Impersonate-User": {"oidc:ann"}, "Impersonate-Group": {"system:masters"}}},
		{name: "unprefixed group", headers: map[string][]string{"Impersonate-User": {"oidc:ann"}, "Impersonate-Group": {"admins"}}},
		{name: "a uid", headers: map[string][]string{"Impersonate-User": {"oidc:ann"}, "Impersonate-Uid": {"1"}}},
		{name: "extras", headers: map[string][]string{"Impersonate-User": {"oidc:ann"}, "Impersonate-Extra-Scopes": {"x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.headers {
				for _, x := range v {
					h.Add(k, x)
				}
			}
			if err := checkImpersonation(h); (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

// fakeAPI records what reached the "API server".
type fakeAPI struct {
	mu   sync.Mutex
	seen []http.Header
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r.Header.Clone())
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"kind":"NamespaceList","items":[]}`)
}

func (f *fakeAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func (f *fakeAPI) last() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		return nil
	}
	return f.seen[len(f.seen)-1]
}

// TestTunnel runs a Hub's sessions, an agent and a fake API server, and
// sends requests the way the Hub's clients do.
func TestTunnel(t *testing.T) {
	api := &fakeAPI{}
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)

	kube := fake.NewClientset(&rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "ops"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.GroupKind, Name: "oidc:ops"}, {Kind: rbacv1.UserKind, Name: "glowpod:admin"}},
	})
	logger := slog.New(slog.DiscardHandler)
	h, err := newHandler(&rest.Config{Host: apiSrv.URL, BearerToken: "agent-sa-token"}, kube, logger)
	if err != nil {
		t.Fatal(err)
	}

	sessions := tunnel.NewSessions()
	connected := make(chan bool, 4)
	sessions.OnChange = func(id string, ok bool) {
		if id == "c1" {
			connected <- ok
		}
	}
	hubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = sessions.Accept(w, r, "c1")
	}))
	t.Cleanup(hubSrv.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hubSrv.URL, "http")+tunnel.ConnectPath, nil) //nolint:bodyclose // the WebSocket owns it
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = tunnel.Serve(ctx, c, h) }()
	if !<-connected {
		t.Fatal("no tunnel")
	}

	client := &http.Client{Transport: &http.Transport{DialContext: sessions.DialFunc("c1")}}
	get := func(path string, header http.Header) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://c1.glowpod-agent"+path, nil)
		for k, v := range header {
			req.Header[k] = v
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck // read-only
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	// As a person: reaches the API server with the agent's token, as them.
	code, _ := get("/api/v1/namespaces", http.Header{
		"Impersonate-User":  {"oidc:ann"},
		"Impersonate-Group": {"oidc:ops"},
		"Authorization":     {"Bearer something-the-hub-sent"},
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	seen := api.last()
	if seen.Get("Authorization") != "Bearer agent-sa-token" ||
		seen.Get("Impersonate-User") != "oidc:ann" || seen.Get("Impersonate-Group") != "oidc:ops" {
		t.Fatalf("the API server saw %v", seen)
	}

	// As cluster admin: refused before it reaches the API server.
	before := api.count()
	code, _ = get("/api/v1/secrets", http.Header{"Impersonate-User": {"oidc:ann"}, "Impersonate-Group": {"system:masters"}})
	if code != http.StatusForbidden || api.count() != before {
		t.Fatalf("system:masters: status %d, reached API %v", code, api.count() != before)
	}

	// The agent's own status: who is bound, and the version.
	code, body := get(tunnel.StatusPath, nil)
	if code != http.StatusOK || !strings.Contains(body, `"groups":["oidc:ops"]`) || !strings.Contains(body, `"users":["glowpod:admin"]`) {
		t.Fatalf("status: %d %s", code, body)
	}

	// Closing the tunnel tells the Hub, and dialing fails until it's back.
	sessions.Close("c1")
	if <-connected {
		t.Fatal("expected a disconnect")
	}
	if _, err := sessions.Dial(t.Context(), "c1"); err == nil {
		t.Fatal("dial should fail without a tunnel")
	}
}
