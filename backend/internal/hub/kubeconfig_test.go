package hub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
)

var ann = auth.SessionInfo{Mode: "in-cluster", Method: "oidc", User: "oidc:ann", Groups: []string{"oidc:shop"}}

// fakeAPI is an API server that records what reached it. An upgrade
// request gets 101 and an echo.
type fakeAPI struct {
	*httptest.Server
	mu   sync.Mutex
	seen []*http.Request
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Clone(r.Context()))
		f.mu.Unlock()
		if r.Header.Get("Upgrade") != "" {
			conn, buf, err := http.NewResponseController(w).Hijack()
			if err != nil {
				return
			}
			defer conn.Close() //nolint:errcheck // test
			_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + r.Header.Get("Upgrade") + "\r\n\r\n")
			_ = buf.Flush()
			line, _ := buf.ReadString('\n')
			_, _ = conn.Write([]byte("echo: " + line))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList","items":[]}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) last(t *testing.T) *http.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		t.Fatal("nothing reached the API server")
	}
	return f.seen[len(f.seen)-1]
}

// proxyHub is a test Hub whose local cluster is a fake API server, reached
// with the Hub's own (fake) service-account token.
func proxyHub(t *testing.T) (*testHub, *fakeAPI, *httptest.Server) {
	t.Helper()
	th := newTestHub(t)
	api := newFakeAPI(t)
	th.registry.Add(cluster.NewServer(&rest.Config{Host: api.URL, BearerToken: "hub-own-token"}, cluster.Info{ID: LocalID, Name: LocalID}))
	if err := th.store.saveSettings(t.Context(), settings{ServerURL: "https://hub.example.com", SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(th.router)
	t.Cleanup(srv.Close)
	return th, api, srv
}

func (th *testHub) kubeconfig(t *testing.T, as auth.SessionInfo) (string, clientcmdKubeconfig) {
	t.Helper()
	th.as = as
	rec := th.do(t, http.MethodPost, "/api/v1/hub/kubeconfig", map[string]string{"current": LocalID})
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "glowpod-kubeconfig.yaml") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers %v", rec.Header())
	}
	cfg, err := clientcmd.Load(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var out clientcmdKubeconfig
	for name, c := range cfg.Contexts {
		out.contexts = append(out.contexts, name)
		out.servers = append(out.servers, cfg.Clusters[c.Cluster].Server)
	}
	slices.Sort(out.contexts)
	out.current = cfg.CurrentContext
	return cfg.AuthInfos["glowpod"].Token, out
}

type clientcmdKubeconfig struct {
	contexts, servers []string
	current           string
}

// reply is an answer read whole, so nothing is left open.
type reply struct {
	code   int
	header http.Header
	body   []byte
}

func kubectl(t *testing.T, srv *httptest.Server, path, token string, header map[string]string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test
	body, _ := io.ReadAll(resp.Body)
	return reply{code: resp.StatusCode, header: resp.Header, body: body}
}

func statusMessage(t *testing.T, r reply) metav1.Status {
	t.Helper()
	var s metav1.Status
	if err := json.Unmarshal(r.body, &s); err != nil || s.Kind != "Status" {
		t.Fatalf("want a Kubernetes Status for kubectl, got %v", err)
	}
	return s
}

func TestKubeconfigDownload(t *testing.T) {
	th, _, _ := proxyHub(t)
	th.registry.Add(cluster.NewServer(&rest.Config{Host: "https://elsewhere"}, cluster.Info{ID: "prod", Name: "prod"}))

	_, mine := th.kubeconfig(t, ann)
	if !slices.Equal(mine.contexts, []string{LocalID}) || mine.servers[0] != "https://hub.example.com/k8s/clusters/local" || mine.current != LocalID {
		t.Fatalf("ann sees local only (a group binding there): %+v", mine)
	}
	_, all := th.kubeconfig(t, admin)
	if !slices.Equal(all.contexts, []string{LocalID, "prod"}) {
		t.Fatalf("an admin sees every cluster: %+v", all)
	}

	th.as = auth.SessionInfo{Mode: "in-cluster", Method: "oidc", User: "oidc:nobody"}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/kubeconfig", map[string]string{}); rec.Code != http.StatusConflict {
		t.Fatalf("no clusters, no kubeconfig: %d", rec.Code)
	}

	secrets, _ := th.kube.CoreV1().Secrets(ns).List(t.Context(), metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=" + kindKubeToken})
	for _, s := range secrets.Items {
		raw := string(s.Data[keyKubeToken])
		if strings.Contains(raw, `"token"`) || !strings.Contains(raw, `"hash"`) {
			t.Fatalf("a token's Secret keeps only its hash: %s", raw)
		}
	}
}

func TestKubeAPIProxy(t *testing.T) {
	th, api, srv := proxyHub(t)
	token, _ := th.kubeconfig(t, ann)

	t.Run("acts as the token's owner, whatever the client sends", func(t *testing.T) {
		resp := kubectl(t, srv, "/k8s/clusters/local/api/v1/namespaces/shop/pods?limit=5", token, map[string]string{
			"Impersonate-User":  "system:admin",
			"Impersonate-Group": "system:masters",
			"Cookie":            "glowpod_session=x",
		})
		if resp.code != http.StatusOK {
			t.Fatalf("status %d", resp.code)
		}
		got := api.last(t)
		if got.URL.Path != "/api/v1/namespaces/shop/pods" || got.URL.RawQuery != "limit=5" {
			t.Fatalf("reached %s?%s", got.URL.Path, got.URL.RawQuery)
		}
		if got.Header.Get("Impersonate-User") != "oidc:ann" || !slices.Equal(got.Header.Values("Impersonate-Group"), []string{"oidc:shop"}) {
			t.Fatalf("impersonated %q %v", got.Header.Get("Impersonate-User"), got.Header.Values("Impersonate-Group"))
		}
		if got.Header.Get("Authorization") != "Bearer hub-own-token" || got.Header.Get("Cookie") != "" {
			t.Fatal("the Hub's own credential replaces the kubeconfig token; cookies stay behind")
		}
	})

	tests := []struct {
		name, path, token string
		want              int
	}{
		{"no token", "/k8s/clusters/local/api", "", http.StatusUnauthorized},
		{"a made-up token", "/k8s/clusters/local/api", "abc123.notit", http.StatusUnauthorized},
		{"a cluster ann can't see", "/k8s/clusters/prod/api", token, http.StatusNotFound},
		{"a cluster that doesn't exist", "/k8s/clusters/nope/api", token, http.StatusNotFound},
	}
	th.registry.Add(cluster.NewServer(&rest.Config{Host: api.URL}, cluster.Info{ID: "prod", Name: "prod"}))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := kubectl(t, srv, tt.path, tt.token, nil)
			if resp.code != tt.want {
				t.Fatalf("status %d, want %d", resp.code, tt.want)
			}
			if s := statusMessage(t, resp); s.Message == "" {
				t.Fatal("kubectl shows the message")
			}
		})
	}

	t.Run("upgrades pass through, for exec", func(t *testing.T) {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close() //nolint:errcheck // test
		_, _ = fmt.Fprintf(conn, "GET /k8s/clusters/local/api/v1/namespaces/shop/pods/web/exec?command=sh HTTP/1.1\r\nHost: hub\r\n"+
			"Authorization: Bearer %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", token)
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		resp, err := http.ReadResponse(r, nil)
		if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("got %v, %v", resp, err)
		}
		defer resp.Body.Close() //nolint:errcheck // test
		// Like kubectl, send only once the upgrade is through.
		_, _ = conn.Write([]byte("hello\n"))
		echo, _ := io.ReadAll(r)
		if string(echo) != "echo: hello\n" {
			t.Fatalf("stream: %q", echo)
		}
		if got := api.last(t); got.Header.Get("Impersonate-User") != "oidc:ann" {
			t.Fatal("upgrades impersonate too")
		}
	})

	t.Run("expired and revoked tokens stop working", func(t *testing.T) {
		th.as = ann
		rec := th.do(t, http.MethodGet, "/api/v1/hub/kubeconfig/tokens", nil)
		var list struct{ Tokens []KubeToken }
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
		if len(list.Tokens) != 1 || list.Tokens[0].User != "oidc:ann" || list.Tokens[0].LastUsed == nil {
			t.Fatalf("ann's tokens: %s", rec.Body)
		}
		id := list.Tokens[0].ID

		th.as = admin
		if rec := th.do(t, http.MethodDelete, "/api/v1/hub/kubeconfig/tokens/"+id, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("only its owner revokes a token: %d", rec.Code)
		}
		th.as = ann
		if rec := th.do(t, http.MethodDelete, "/api/v1/hub/kubeconfig/tokens/"+id, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("revoke: %d", rec.Code)
		}
		if resp := kubectl(t, srv, "/k8s/clusters/local/api", token, nil); resp.code != http.StatusUnauthorized {
			t.Fatalf("revoked, still %d", resp.code)
		}

		fresh, _ := th.kubeconfig(t, ann)
		th.now = func() time.Time { return time.Now().Add(DefaultKubeconfigTTL + time.Minute) }
		defer func() { th.now = time.Now }()
		if resp := kubectl(t, srv, "/k8s/clusters/local/api", fresh, nil); resp.code != http.StatusUnauthorized {
			t.Fatalf("expired, still %d", resp.code)
		}
		th.pruneKubeTokens(t.Context())
		if left, _ := th.store.kubeTokens(t.Context()); len(left) != 0 {
			t.Fatalf("expired tokens are deleted: %d left", len(left))
		}
	})
}

func TestKubeTokensPerPersonAreCapped(t *testing.T) {
	th, _, _ := proxyHub(t)
	for range maxKubeTokens + 3 {
		th.kubeconfig(t, ann)
	}
	mine, err := th.myKubeTokens(t.Context(), ann.User)
	if err != nil || len(mine) != maxKubeTokens {
		t.Fatalf("%d tokens, want %d", len(mine), maxKubeTokens)
	}
}
