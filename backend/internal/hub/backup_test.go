package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/agent"
	"glowpod/internal/auth"
	"glowpod/internal/tunnel"
)

// importedHub is a set-up Hub with a Glowpod account, sign-in settings and
// one imported cluster whose agent has registered.
func importedHub(t *testing.T) (*testHub, string) {
	t.Helper()
	th := newTestHub(t)
	th.as = admin
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/setup", setupRequest{Password: "admin password 1", ServerURL: "https://hub.example.com"}); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/users", userRequest{Username: "bob", Role: RoleUser, Password: "bob password 12"}); rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	if err := th.store.saveAuthSettings(t.Context(), AuthSettings{LDAP: LDAPSettings{
		Enabled: true, URL: "ldap://ldap.example.com", BindDN: "cn=glowpod", BindPassword: "bind-secret", UserBase: "dc=example,dc=com",
	}}); err != nil {
		t.Fatal(err)
	}
	rec := th.do(t, http.MethodPost, "/api/v1/hub/clusters", importRequest{Name: "prod"})
	var imp ImportResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &imp)
	token := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(imp.Command, "kubectl apply -f https://hub.example.com"), tunnel.ImportPrefix), ".yaml")
	rec = th.do(t, http.MethodPost, tunnel.RegisterPath, agent.RegisterRequest{Token: token})
	var reg agent.RegisterResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	if reg.Credential == "" {
		t.Fatalf("register: %s", rec.Body)
	}
	return th, reg.Credential
}

func backupOf(t *testing.T, th *testHub, passphrase string) string {
	t.Helper()
	th.as = admin
	rec := th.do(t, http.MethodPost, "/api/v1/hub/backup", backupRequest{Passphrase: passphrase})
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "glowpod-hub-backup-") {
		t.Fatalf("backup: %d %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func TestBackupFile(t *testing.T) {
	th, _ := importedHub(t)
	file := backupOf(t, th, "a long passphrase")
	for _, secret := range []string{"bind-secret", "argon2id$v", "bob"} {
		if strings.Contains(file, secret) {
			t.Fatalf("the file shows %q: it must be encrypted", secret)
		}
	}
	d, _, err := unseal([]byte(file), "a long passphrase")
	if err != nil || len(d.Users) != 2 || len(d.Clusters) != 1 || d.Auth.LDAP.BindPassword != "bind-secret" || d.Clusters[0].CredentialHash == "" {
		t.Fatalf("unsealed %+v, %v", d, err)
	}

	tests := []struct {
		name       string
		file       string
		passphrase string
	}{
		{"a wrong passphrase", file, "another passphrase"},
		{"a changed header", strings.Replace(file, `"time": 3`, `"time": 2`, 1), "a long passphrase"},
		{"not a backup", `{"hello": "world"}`, "a long passphrase"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := unseal([]byte(tt.file), tt.passphrase); err == nil {
				t.Fatal("must fail")
			}
		})
	}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/backup", backupRequest{Passphrase: "short"}); rec.Code != 400 {
		t.Fatalf("a weak passphrase: %d", rec.Code)
	}
	th.as = auth.SessionInfo{User: "glowpod:bob", Method: "password"}
	if rec := th.do(t, http.MethodPost, "/api/v1/hub/backup", backupRequest{Passphrase: "a long passphrase"}); rec.Code != 403 {
		t.Fatalf("only administrators: %d", rec.Code)
	}
}

func TestRestoreIntoANewHub(t *testing.T) {
	old, credential := importedHub(t)
	file := backupOf(t, old, "a long passphrase")

	// A fresh Hub, at first start, with an account and a cluster of its own.
	fresh := newTestHub(t)
	ctx := t.Context()
	ended := false
	fresh.cfg.EndAllSessions = func(context.Context) { ended = true }
	got := &providers{}
	fresh.UseAuth(ctx, got.set, FixedAuth{})
	fresh.as = admin
	if err := fresh.store.putSecret(ctx, userSecretPrefix+"stale", "user", map[string][]byte{keyPasswordHash: []byte("$argon2id$x"), keyRole: []byte(RoleUser)}); err != nil {
		t.Fatal(err)
	}
	fresh.add(Record{ID: "stale-cluster", Name: "stale-cluster"})
	if err := fresh.store.createCluster(ctx, Record{ID: "stale-cluster", Name: "stale-cluster"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fresh.newKubeToken(ctx, admin, tokenScope{}); err != nil {
		t.Fatal(err)
	}

	if rec := fresh.do(t, http.MethodPost, "/api/v1/hub/restore", restoreRequest{Passphrase: "wrong passphrase", Backup: file}); rec.Code != 400 {
		t.Fatalf("a wrong passphrase: %d", rec.Code)
	}
	rec := fresh.do(t, http.MethodPost, "/api/v1/hub/restore", restoreRequest{Passphrase: "a long passphrase", Backup: file})
	var res RestoreResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != 200 || res.Users != 2 || res.Clusters != 1 {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}

	// First start is over; admin's and bob's passwords are the backup's.
	if s := fresh.Session(ctx, admin).(SessionInfo); s.SetupRequired || s.ServerURL != "https://hub.example.com" {
		t.Fatalf("session = %+v", s)
	}
	if _, err := fresh.CheckPassword(ctx, "bob", "bob password 12"); err != nil {
		t.Fatal("bob's password comes back")
	}
	if _, err := fresh.CheckPassword(ctx, "admin", "admin password 1"); err != nil {
		t.Fatal("admin's password comes back")
	}
	if boot, _ := fresh.store.secret(ctx, BootstrapSecret); boot != nil {
		t.Fatal("the bootstrap password is gone")
	}
	users, _ := fresh.store.users(ctx)
	if len(users) != 2 {
		t.Fatalf("accounts that weren't in the backup are gone: %+v", users)
	}
	if got.ldap == nil {
		t.Fatal("LDAP is set up again")
	}
	if _, ok := fresh.registry.Get("stale-cluster"); ok {
		t.Fatal("clusters that weren't in the backup are gone")
	}
	if _, ok := fresh.registry.Get("prod"); !ok {
		t.Fatal("prod is back")
	}
	if !ended {
		t.Fatal("everyone is signed out")
	}
	if tokens, _ := fresh.store.kubeTokens(ctx); len(tokens) != 0 {
		t.Fatal("kubeconfigs from before stop working")
	}

	// prod's agent connects to the new Hub with the credential it had.
	srv := httptest.NewServer(fresh.router)
	t.Cleanup(srv.Close)
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+tunnel.ConnectPath, //nolint:bodyclose // the WebSocket owns it
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + credential}}})
	if err != nil {
		t.Fatalf("the agent's old credential: %v", err)
	}
	waitFor(t, func() bool { return fresh.info("prod").State == "connected" })
	_ = c.Close(websocket.StatusNormalClosure, "")
	if _, err := fresh.store.dyn.Resource(ClusterGVR).Get(ctx, "prod", metav1.GetOptions{}); err != nil {
		t.Fatalf("prod's record: %v", err)
	}
}

func TestRestoreRefusesABrokenBackup(t *testing.T) {
	th := newTestHub(t)
	th.as = admin
	now := time.Now()
	tests := []struct {
		name string
		d    backupData
	}{
		{"no admin", backupData{ServerURL: "https://hub.example.com", Users: []backupUser{{Username: "bob", Role: RoleUser, PasswordHash: "$argon2id$x"}}}},
		{"a bad server URL", backupData{ServerURL: "ftp://x", Users: []backupUser{{Username: "admin", Role: RoleAdmin, PasswordHash: "$argon2id$x"}}}},
		{"a cluster called local", backupData{ServerURL: "https://hub.example.com", Users: []backupUser{{Username: "admin", Role: RoleAdmin, PasswordHash: "$argon2id$x"}}, Clusters: []backupCluster{{ID: LocalID}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := seal(tt.d, "a long passphrase", now)
			if err != nil {
				t.Fatal(err)
			}
			if rec := th.do(t, http.MethodPost, "/api/v1/hub/restore", restoreRequest{Passphrase: "a long passphrase", Backup: string(raw)}); rec.Code != 400 {
				t.Fatalf("status %d", rec.Code)
			}
		})
	}
}
