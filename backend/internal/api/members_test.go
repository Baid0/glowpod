package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/members"
)

func TestMembersAPI(t *testing.T) {
	kube := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "payments-api"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop"}},
	)
	// Granting cluster-admin is refused, as the API server refuses someone
	// who doesn't have it themselves.
	kube.PrependReactor("create", "clusterrolebindings", func(a k8stesting.Action) (bool, runtime.Object, error) {
		b := a.(k8stesting.CreateAction).GetObject()
		if strings.Contains(string(mustJSON(t, b)), `"name":"cluster-admin"`) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "clusterrolebindings"}, "", nil)
		}
		return false, nil, nil
	})
	authn, err := auth.NewLocal()
	if err != nil {
		t.Fatal(err)
	}
	env := &writeEnv{router: NewRouter(Options{
		Clusters: cluster.Single(cluster.NewLocalWithClients(cluster.Info{ID: cluster.DefaultID}, &cluster.Clients{Kube: kube})),
		Auth:     authn,
		Hosts:    auth.LoopbackHosts("7171"),
	})}
	launch, _ := url.Parse(authn.LaunchURL("http://127.0.0.1:7171", "/"))
	env.cookie = env.do(t, http.MethodGet, launch.RequestURI(), "", nil).Result().Cookies()[0]
	var session sessionResponse
	mustDecode(t, env.do(t, http.MethodGet, "/api/v1/session", "", nil).Body.Bytes(), &session)
	csrf := map[string]string{"X-CSRF-Token": session.CSRFToken, "Content-Type": "application/json"}

	const base = "/api/v1/clusters/default"
	tests := []struct {
		name, method, path, body string
		want                     int
		code                     string
	}{
		{"add a read-only member", http.MethodPut, "/members", `{"kind":"User","name":"oidc:ann","role":"read-only"}`, http.StatusNoContent, ""},
		{"granting more than you have", http.MethodPut, "/members", `{"kind":"Group","name":"oidc:ops","role":"owner"}`, http.StatusForbidden, "forbidden"},
		{"a role that isn't one", http.MethodPut, "/members", `{"kind":"User","name":"oidc:ann","role":"god"}`, http.StatusBadRequest, "invalid"},
		{"a system: name", http.MethodPut, "/members", `{"kind":"Group","name":"system:masters","role":"read-only"}`, http.StatusBadRequest, "invalid"},
		{"make a project", http.MethodPost, "/projects", `{"name":"payments","namespaces":["payments-api"]}`, http.StatusNoContent, ""},
		{"a project member", http.MethodPut, "/projects/payments/members", `{"kind":"Group","name":"oidc:payments","role":"member"}`, http.StatusNoContent, ""},
		{"a project that isn't there", http.MethodPut, "/projects/nope/members", `{"kind":"Group","name":"oidc:x","role":"member"}`, http.StatusBadRequest, "invalid"},
		{"no namespaces left", http.MethodPut, "/projects/payments/namespaces", `{"namespaces":[]}`, http.StatusBadRequest, "invalid"},
		{"without the CSRF token", http.MethodPut, "/members", `{"kind":"User","name":"oidc:bob","role":"read-only"}`, http.StatusForbidden, "csrf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := csrf
			if tt.code == "csrf" {
				header = map[string]string{"Content-Type": "application/json"}
			}
			rec := env.do(t, tt.method, base+tt.path, tt.body, header)
			if rec.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			if tt.code != "" && !strings.Contains(rec.Body.String(), `"code":"`+tt.code) {
				t.Fatalf("body %s, want code %s", rec.Body, tt.code)
			}
		})
	}

	var m members.Members
	mustDecode(t, env.do(t, http.MethodGet, base+"/members", "", nil).Body.Bytes(), &m)
	if len(m.Members) != 1 || m.Members[0].Name != "oidc:ann" || m.Members[0].Role != "read-only" {
		t.Fatalf("members = %+v", m)
	}
	var p members.Projects
	mustDecode(t, env.do(t, http.MethodGet, base+"/projects", "", nil).Body.Bytes(), &p)
	if len(p.Projects) != 1 || len(p.Projects[0].Members) != 1 || p.Projects[0].Members[0].Role != "member" {
		t.Fatalf("projects = %+v", p)
	}

	if rec := env.do(t, http.MethodDelete, base+"/members?kind=User&name=oidc:ann", "", csrf); rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := env.do(t, http.MethodDelete, base+"/projects/payments", "", csrf); rec.Code != http.StatusNoContent {
		t.Fatalf("delete project: %d %s", rec.Code, rec.Body)
	}
	var after members.Projects
	mustDecode(t, env.do(t, http.MethodGet, base+"/projects", "", nil).Body.Bytes(), &after)
	if len(after.Projects) != 0 {
		t.Fatalf("after = %+v", after)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
