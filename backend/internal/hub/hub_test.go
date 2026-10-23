package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"glowpod/internal/agent"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/subjects"
	"glowpod/internal/tunnel"
)

const ns = "glowpod"

type testHub struct {
	*Hub
	kube     *fake.Clientset
	registry *cluster.Registry
	router   http.Handler
	// as is who the next /api/v1/hub request is from.
	as auth.SessionInfo
}

func newTestHub(t *testing.T) *testHub {
	t.Helper()
	kube := fake.NewClientset(&rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "shop-devs", Namespace: "shop"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.GroupKind, Name: "oidc:shop"}},
	})
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{ClusterGVR: "ClusterList"})
	registry := cluster.Single(cluster.NewLocalWithClients(cluster.Info{ID: LocalID, Name: LocalID}, &cluster.Clients{Kube: kube}))
	h := New(Config{
		Namespace:  ns,
		Kube:       kube,
		Dynamic:    dyn,
		Registry:   registry,
		PublicURL:  "https://hub.example.com",
		AgentImage: "ghcr.io/glowpod-dev/glowpod:9.9.9",
		Admins:     []string{"oidc:platform"},
	})
	if err := h.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	th := &testHub{Hub: h, kube: kube, registry: registry}
	r := chi.NewRouter()
	h.MountPublic(r)
	r.Route("/api/v1/hub", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(auth.WithSessionInfo(req.Context(), th.as)))
			})
		})
		h.MountAPI(r)
	})
	th.router = r
	return th
}

func (th *testHub) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	th.router.ServeHTTP(rec, req)
	return rec
}

func (th *testHub) bootstrapPassword(t *testing.T) string {
	t.Helper()
	s, err := th.kube.CoreV1().Secrets(ns).Get(t.Context(), BootstrapSecret, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return string(s.Data[keyPassword])
}

var admin = auth.SessionInfo{Mode: "in-cluster", Method: "password", User: "glowpod:admin"}

func TestFirstStart(t *testing.T) {
	th := newTestHub(t)
	ctx := t.Context()
	boot := th.bootstrapPassword(t)
	if len(boot) < 20 {
		t.Fatalf("bootstrap password %q is short", boot)
	}
	// Starting again keeps the same password.
	if err := th.Start(ctx); err != nil || th.bootstrapPassword(t) != boot {
		t.Fatalf("restart changed the bootstrap password: %v", err)
	}

	if user, err := th.CheckPassword(ctx, "Admin", boot); err != nil || user != "glowpod:admin" {
		t.Fatalf("bootstrap sign-in: %q %v", user, err)
	}
	for _, bad := range [][2]string{{"admin", "nope"}, {"bob", boot}, {"../admin", boot}} {
		if _, err := th.CheckPassword(ctx, bad[0], bad[1]); !errors.Is(err, auth.ErrBadCredentials) {
			t.Fatalf("%v: err = %v", bad, err)
		}
	}
	if s := th.Session(ctx, admin).(SessionInfo); !s.SetupRequired || s.Role != RoleAdmin || s.ServerURL != "https://hub.example.com" {
		t.Fatalf("session = %+v", s)
	}

	th.as = auth.SessionInfo{User: "oidc:platform-lead", Groups: []string{"oidc:platform"}}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/setup", setupRequest{Password: "a long enough one", ServerURL: "https://hub.example.com"}); rec.Code != http.StatusForbidden {
		t.Fatalf("setup by a non-local admin: %d", rec.Code)
	}
	th.as = admin
	for _, tt := range []struct {
		req  setupRequest
		code int
	}{
		{setupRequest{Password: "short", ServerURL: "https://hub.example.com"}, http.StatusBadRequest},
		{setupRequest{Password: "a long enough one", ServerURL: "https://hub.example.com/path"}, http.StatusBadRequest},
		{setupRequest{Password: "a long enough one", ServerURL: "ftp://hub"}, http.StatusBadRequest},
		{setupRequest{Password: "a long enough one", ServerURL: "https://hub.example.com/"}, http.StatusNoContent},
		{setupRequest{Password: "another long one", ServerURL: "https://hub.example.com"}, http.StatusConflict},
	} {
		if rec := th.do(t, http.MethodPost, "/api/v1/hub/setup", tt.req); rec.Code != tt.code {
			t.Fatalf("setup %+v: %d %s, want %d", tt.req, rec.Code, rec.Body, tt.code)
		}
	}

	// The bootstrap password is gone; the new one works.
	if _, err := th.CheckPassword(ctx, "admin", boot); !errors.Is(err, auth.ErrBadCredentials) {
		t.Fatalf("bootstrap password still works: %v", err)
	}
	if _, err := th.CheckPassword(ctx, "admin", "a long enough one"); err != nil {
		t.Fatal(err)
	}
	if _, err := th.kube.CoreV1().Secrets(ns).Get(ctx, BootstrapSecret, metav1.GetOptions{}); err == nil {
		t.Fatal("the bootstrap Secret must be deleted")
	}
	user, _ := th.kube.CoreV1().Secrets(ns).Get(ctx, "glowpod-user-admin", metav1.GetOptions{})
	if h := string(user.Data[keyPasswordHash]); !strings.HasPrefix(h, "$argon2id$v=19$") || strings.Contains(h, "a long enough one") {
		t.Fatalf("stored hash = %q", h)
	}
	if s := th.Session(ctx, admin).(SessionInfo); s.SetupRequired {
		t.Fatal("setup still required")
	}
}

func TestImportRegisterConnectRemove(t *testing.T) {
	th := newTestHub(t)
	ctx := t.Context()
	th.as = admin
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/setup", setupRequest{Password: "a long enough one", ServerURL: "https://hub.example.com"}); rec.Code != http.StatusNoContent {
		t.Fatal(rec.Body)
	}

	// Only Hub administrators import.
	th.as = auth.SessionInfo{User: "oidc:bob"}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/clusters", importRequest{Name: "Prod EU"}); rec.Code != http.StatusForbidden {
		t.Fatalf("import by a user: %d", rec.Code)
	}
	th.as = admin
	rec := th.do(t, http.MethodPost, "/api/v1/hub/clusters", importRequest{Name: "Prod EU", Owner: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	var imp ImportResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &imp)
	if imp.Cluster.ID != "prod-eu" || imp.Cluster.State != "pending" || !strings.HasPrefix(imp.Command, "kubectl apply -f https://hub.example.com/v3/import/prod-eu.") {
		t.Fatalf("import = %+v", imp)
	}
	if _, ok := th.registry.Get("prod-eu"); !ok {
		t.Fatal("not in the registry")
	}
	// The same name again gets its own id.
	rec = th.do(t, http.MethodPost, "/api/v1/hub/clusters", importRequest{Name: "Prod EU"})
	var second ImportResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &second)
	if second.Cluster.ID != "prod-eu-2" {
		t.Fatalf("second id = %q", second.Cluster.ID)
	}

	manifestURL := strings.TrimPrefix(imp.Command, "kubectl apply -f https://hub.example.com")
	token := strings.TrimSuffix(strings.TrimPrefix(manifestURL, tunnel.ImportPrefix), ".yaml")

	// The manifest: valid YAML, the agent's narrow role, the owner binding.
	rec = th.do(t, http.MethodGet, manifestURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest: %d %s", rec.Code, rec.Body)
	}
	manifest := rec.Body.String()
	for i, doc := range strings.Split(manifest, "\n---\n") {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil || obj["kind"] == nil {
			t.Fatalf("document %d isn't a Kubernetes object: %v\n%s", i, err, doc)
		}
	}
	for _, want := range []string{
		`image: "ghcr.io/glowpod-dev/glowpod:9.9.9"`,
		`hub-url: "https://hub.example.com"`,
		"verbs: [impersonate]",
		`name: "glowpod:admin"`,
		"name: cluster-admin",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, manifest)
		}
	}
	if strings.Count(manifest, "cluster-admin") != 1 || strings.Contains(manifest, "verbs: [\"*\"]") {
		t.Fatal("only the owner binding may name cluster-admin")
	}
	// Without the owner box, no owner binding.
	rec = th.do(t, http.MethodPost, "/api/v1/hub/clusters/prod-eu-2/import", importRequest{})
	var noOwner ImportResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &noOwner)
	rec = th.do(t, http.MethodGet, strings.TrimPrefix(noOwner.Command, "kubectl apply -f https://hub.example.com"), nil)
	if strings.Contains(rec.Body.String(), "cluster-admin") {
		t.Fatal("owner binding without the box")
	}

	// Registering trades the token for a credential, once.
	rec = th.do(t, http.MethodPost, tunnel.RegisterPath, agent.RegisterRequest{Token: token})
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var reg agent.RegisterResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	if reg.Cluster != "prod-eu" || !strings.HasPrefix(reg.Credential, "prod-eu.") {
		t.Fatalf("register = %+v", reg)
	}
	if rec := th.do(t, http.MethodPost, tunnel.RegisterPath, agent.RegisterRequest{Token: token}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("token reused: %d", rec.Code)
	}
	if rec := th.do(t, http.MethodGet, manifestURL, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("manifest after registering: %d", rec.Code)
	}
	stored, _ := th.kube.CoreV1().Secrets(ns).Get(ctx, "glowpod-cluster-prod-eu", metav1.GetOptions{})
	if bytes.Contains(stored.Data[keyCredentialHash], []byte(reg.Credential)) || len(stored.Data[keyTokenHash]) != 0 {
		t.Fatal("the Hub must keep only the credential's hash, and drop the token")
	}

	// Connecting: a wrong credential is refused; the right one opens a tunnel.
	srv := httptest.NewServer(th.router)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + tunnel.ConnectPath
	for _, bad := range []string{"", "prod-eu.wrong", "prod-eu-2.whatever", reg.Credential + "x"} {
		_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + bad}}}) //nolint:bodyclose // a refused handshake has no body to keep
		if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("credential %q: err %v", bad, err)
		}
	}
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + reg.Credential}}}) //nolint:bodyclose // the WebSocket owns it
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return th.info("prod-eu").State == "connected" })
	_ = c.Close(websocket.StatusNormalClosure, "")
	waitFor(t, func() bool { return th.info("prod-eu").State == "disconnected" })

	// Removing it revokes the credential and tells how to clean up.
	rec = th.do(t, http.MethodDelete, "/api/v1/hub/clusters/prod-eu", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "kubectl delete namespace glowpod-system") {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if _, ok := th.registry.Get("prod-eu"); ok {
		t.Fatal("still in the registry")
	}
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + reg.Credential}}}) //nolint:bodyclose // a refused handshake has no body to keep
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("removed cluster connected: %v", err)
	}
}

func TestImportTokenExpires(t *testing.T) {
	th := newTestHub(t)
	th.as = admin
	th.do(t, http.MethodPost, "/api/v1/hub/setup", setupRequest{Password: "a long enough one", ServerURL: "https://hub.example.com"})
	rec := th.do(t, http.MethodPost, "/api/v1/hub/clusters", importRequest{Name: "lab"})
	var imp ImportResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &imp)
	path := strings.TrimPrefix(imp.Command, "kubectl apply -f https://hub.example.com")
	th.now = func() time.Time { return time.Now().Add(importTTL + time.Minute) }
	if rec := th.do(t, http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("expired manifest: %d", rec.Code)
	}
}

func TestVisible(t *testing.T) {
	th := newTestHub(t)
	th.add(Record{ID: "prod", Name: "prod", Subjects: subjectsOf([]string{"oidc:ann"}, []string{"oidc:payments"})})
	th.add(Record{ID: "open", Name: "open", Subjects: subjectsOf(nil, []string{"system:authenticated"})})
	tests := []struct {
		who  auth.SessionInfo
		want map[string]bool
	}{
		{admin, map[string]bool{LocalID: true, "prod": true, "open": true}},
		{auth.SessionInfo{User: "oidc:lead", Groups: []string{"oidc:platform"}}, map[string]bool{LocalID: true, "prod": true, "open": true}},
		{auth.SessionInfo{User: "oidc:ann"}, map[string]bool{LocalID: false, "prod": true, "open": true}},
		{auth.SessionInfo{User: "oidc:cy", Groups: []string{"oidc:payments"}}, map[string]bool{LocalID: false, "prod": true, "open": true}},
		{auth.SessionInfo{User: "oidc:dee", Groups: []string{"oidc:shop"}}, map[string]bool{LocalID: true, "prod": false, "open": true}},
		{auth.SessionInfo{User: "oidc:eve"}, map[string]bool{LocalID: false, "prod": false, "open": true}},
	}
	for _, tt := range tests {
		for id, want := range tt.want {
			if got := th.Visible(tt.who, id); got != want {
				t.Errorf("%s sees %s: %v, want %v", tt.who.User, id, got, want)
			}
		}
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := hashPassword("correct horse battery")
	if h == other {
		t.Fatal("two hashes of one password must differ (salt)")
	}
	for pw, want := range map[string]bool{"correct horse battery": true, "correct horse batterY": false, "": false} {
		if ok, err := checkPassword(h, pw); err != nil || ok != want {
			t.Errorf("%q: %v %v", pw, ok, err)
		}
	}
	if _, err := checkPassword("$2a$10$bcrypt", "x"); err == nil {
		t.Fatal("a non-argon2id hash must be refused")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func subjectsOf(users, groups []string) subjects.Set {
	slices.Sort(users)
	slices.Sort(groups)
	return subjects.Set{Users: users, Groups: groups}
}

func TestSessionsSurviveARestart(t *testing.T) {
	th := newTestHub(t)
	before := auth.NewStore(time.Hour, 12*time.Hour)
	before.Saver = th.Sessions()
	id, err := before.Create(t.Context(), &auth.Session{
		Identity: cluster.UserIdentity("glowpod:admin", nil),
		Info:     auth.SessionInfo{Mode: "in-cluster", Method: "password", User: "glowpod:admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := th.kube.CoreV1().Secrets(ns).List(t.Context(), metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=session"})
	if err != nil || len(secrets.Items) != 1 {
		t.Fatalf("want one session Secret, got %v, %v", secrets, err)
	}
	sec := secrets.Items[0]
	if !strings.HasPrefix(sec.Name, sessionSecretPrefix) || strings.Contains(sec.Name, id) || bytes.Contains(sec.Data[keySession], []byte(id)) {
		t.Fatal("the Secret is named by a hash and never holds the cookie")
	}

	after := auth.NewStore(time.Hour, 12*time.Hour) // a new Hub process
	after.Saver = th.Sessions()
	sess, ok := after.Get(t.Context(), id)
	if !ok || sess.Info.User != "glowpod:admin" || sess.Identity.User != "glowpod:admin" {
		t.Fatalf("got %+v, %v; want admin still signed in", sess, ok)
	}
	after.Delete(t.Context(), id)
	if left, _ := th.kube.CoreV1().Secrets(ns).List(t.Context(), metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=session"}); len(left.Items) != 0 {
		t.Fatal("signing out deletes the Secret")
	}
}
