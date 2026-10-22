package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/cached/memory"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/access"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
)

type writeEnv struct {
	router http.Handler
	cookie *http.Cookie
	csrf   string
	writes *[]k8stesting.Action
}

func newWriteEnv(t *testing.T) *writeEnv {
	t.Helper()
	kube := fake.NewClientset()
	kube.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: []string{"get", "list", "update", "delete"}},
			{Name: "nodes", Kind: "Node", Verbs: []string{"get", "list"}},
		}},
		{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{
			{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "update", "patch", "delete"}},
			{Name: "deployments/scale", Kind: "Scale", Namespaced: true, Verbs: []string{"get", "patch", "update"}},
		}},
	}
	// Only "delete deployments in shop" is allowed.
	kube.PrependReactor("create", "selfsubjectaccessreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
		review := a.(k8stesting.CreateAction).GetObject().(*authzv1.SelfSubjectAccessReview)
		ra := review.Spec.ResourceAttributes
		review.Status.Allowed = ra.Verb == "delete" && ra.Group == "apps" && ra.Resource == "deployments" && ra.Namespace == "shop"
		return true, review, nil
	})

	obj := func(apiVersion, kind, name string, extra map[string]any) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": apiVersion, "kind": kind,
			"metadata": map[string]any{"name": name, "namespace": "shop", "resourceVersion": "5"},
		}}
		for k, v := range extra {
			u.Object[k] = v
		}
		return u
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	},
		obj("v1", "ConfigMap", "settings", map[string]any{"data": map[string]any{"mode": "fast"}}),
		obj("apps/v1", "Deployment", "web", map[string]any{"spec": map[string]any{"replicas": int64(2)}}),
	)
	var writes []k8stesting.Action
	dyn.PrependReactor("*", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			writes = append(writes, a)
		}
		return false, nil, nil
	})

	authn, err := auth.NewLocal()
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{
		Provider: cluster.NewLocalWithClients(cluster.Info{ID: cluster.DefaultID}, &cluster.Clients{
			Kube: kube, Dynamic: dyn, Discovery: memory.NewMemCacheClient(kube.Discovery()),
		}),
		Auth:  authn,
		Hosts: auth.LoopbackHosts("7171"),
	})
	env := &writeEnv{router: router, writes: &writes}

	launch, _ := url.Parse(authn.LaunchURL("http://127.0.0.1:7171"))
	rec := env.do(t, http.MethodGet, launch.RequestURI(), "", nil)
	env.cookie = rec.Result().Cookies()[0]
	var session sessionResponse
	mustDecode(t, env.do(t, http.MethodGet, "/api/v1/session", "", nil).Body.Bytes(), &session)
	if session.CSRFToken == "" {
		t.Fatal("session has no CSRF token")
	}
	env.csrf = session.CSRFToken
	return env
}

func (e *writeEnv) do(t *testing.T, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:7171"
	if e.cookie != nil {
		req.AddCookie(e.cookie)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

const editedSettings = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  resourceVersion: \"5\"\ndata:\n  mode: slow\n"

func TestWriteAPI(t *testing.T) {
	const base = "/api/v1/clusters/default"
	jsonBody := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		noCSRF     bool
		origin     string
		wantStatus int
		wantCode   string
		check      func(t *testing.T, body []byte, writes []k8stesting.Action)
	}{
		{
			name: "access checks", method: http.MethodPost, path: base + "/access", wantStatus: 200,
			body: jsonBody(accessRequest{Checks: []access.Check{
				{Verb: "delete", Group: "apps", Resource: "deployments", Namespace: "shop", Name: "web"},
				{Verb: "update", Group: "apps", Resource: "deployments", Namespace: "shop", Name: "web"},
				{Verb: "delete", Group: "apps", Resource: "deployments", Namespace: "prod"},
			}}),
			check: func(t *testing.T, body []byte, _ []k8stesting.Action) {
				var got accessResponse
				mustDecode(t, body, &got)
				if len(got.Allowed) != 3 || !got.Allowed[0] || got.Allowed[1] || got.Allowed[2] {
					t.Fatalf("allowed = %v", got.Allowed)
				}
			},
		},
		{name: "access needs checks", method: http.MethodPost, path: base + "/access", body: `{"checks":[]}`, wantStatus: 400, wantCode: "bad_checks"},
		{name: "access bad verb", method: http.MethodPost, path: base + "/access", body: `{"checks":[{"verb":"impersonate","resource":"users"}]}`, wantStatus: 400, wantCode: "bad_checks"},
		{name: "unknown body fields", method: http.MethodPost, path: base + "/access", body: `{"checks":[],"extra":1}`, wantStatus: 400, wantCode: "bad_body"},
		{name: "writes need the csrf token", method: http.MethodDelete, path: base + "/resources/apps/v1/deployments/web?namespace=shop", noCSRF: true, wantStatus: 403, wantCode: "csrf"},
		{name: "writes from another site", method: http.MethodDelete, path: base + "/resources/apps/v1/deployments/web?namespace=shop", origin: "https://evil.example.com", wantStatus: 403, wantCode: "csrf"},
		{
			name: "delete", method: http.MethodDelete, path: base + "/resources/apps/v1/deployments/web?namespace=shop&propagation=foreground", wantStatus: 204,
			check: func(t *testing.T, _ []byte, writes []k8stesting.Action) {
				if len(writes) != 1 || writes[0].GetVerb() != "delete" || writes[0].GetNamespace() != "shop" {
					t.Fatalf("writes = %v", writes)
				}
			},
		},
		{name: "delete in two namespaces", method: http.MethodDelete, path: base + "/resources/apps/v1/deployments/web?namespace=shop,prod", wantStatus: 400, wantCode: "one_namespace"},
		{name: "delete needs namespace", method: http.MethodDelete, path: base + "/resources/apps/v1/deployments/web", wantStatus: 400, wantCode: "namespace_required"},
		{name: "delete missing", method: http.MethodDelete, path: base + "/resources/apps/v1/deployments/gone?namespace=shop", wantStatus: 404, wantCode: "not_found"},
		{name: "delete not allowed by the API", method: http.MethodDelete, path: base + "/resources/core/v1/nodes/w1", wantStatus: 400, wantCode: "not_deletable"},
		{
			name: "scale", method: http.MethodPut, path: base + "/resources/apps/v1/deployments/web/scale?namespace=shop", body: `{"replicas":4}`, wantStatus: 204,
			check: func(t *testing.T, _ []byte, writes []k8stesting.Action) {
				if writes[0].GetSubresource() != "scale" {
					t.Fatalf("writes = %v", writes)
				}
			},
		},
		{name: "scale needs replicas", method: http.MethodPut, path: base + "/resources/apps/v1/deployments/web/scale?namespace=shop", body: `{}`, wantStatus: 400, wantCode: "bad_replicas"},
		{name: "scale a configmap", method: http.MethodPut, path: base + "/resources/core/v1/configmaps/settings/scale?namespace=shop", body: `{"replicas":1}`, wantStatus: 400, wantCode: "not_scalable"},
		{name: "restart", method: http.MethodPost, path: base + "/resources/apps/v1/deployments/web/restart?namespace=shop", wantStatus: 204},
		{name: "restart a configmap", method: http.MethodPost, path: base + "/resources/core/v1/configmaps/settings/restart?namespace=shop", wantStatus: 400, wantCode: "not_restartable"},
		{
			name: "edit dry run", method: http.MethodPut, path: base + "/resources/core/v1/configmaps/settings?namespace=shop&dryRun=true",
			body: jsonBody(updateRequest{YAML: editedSettings}), wantStatus: 200,
			check: func(t *testing.T, body []byte, _ []k8stesting.Action) {
				var got struct {
					DryRun        bool
					Before, After string
				}
				mustDecode(t, body, &got)
				if !got.DryRun || !strings.Contains(got.Before, "mode: fast") || !strings.Contains(got.After, "mode: slow") {
					t.Fatalf("result = %+v", got)
				}
			},
		},
		{
			name: "edit apply", method: http.MethodPut, path: base + "/resources/core/v1/configmaps/settings?namespace=shop",
			body: jsonBody(updateRequest{YAML: editedSettings}), wantStatus: 200,
		},
		{
			name: "edit rename refused", method: http.MethodPut, path: base + "/resources/core/v1/configmaps/settings?namespace=shop",
			body: jsonBody(updateRequest{YAML: strings.Replace(editedSettings, "name: settings", "name: x", 1)}), wantStatus: 400, wantCode: "identity_changed",
		},
		{name: "edit bad json", method: http.MethodPut, path: base + "/resources/core/v1/configmaps/settings?namespace=shop", body: `yaml: nope`, wantStatus: 400, wantCode: "bad_body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWriteEnv(t)
			header := map[string]string{"Content-Type": "application/json"}
			if !tt.noCSRF {
				header[auth.CSRFHeader] = env.csrf
			}
			if tt.origin != "" {
				header["Origin"] = tt.origin
			}
			rec := env.do(t, tt.method, tt.path, tt.body, header)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode != "" {
				var body struct {
					Error errorBody `json:"error"`
				}
				mustDecode(t, rec.Body.Bytes(), &body)
				if body.Error.Code != tt.wantCode {
					t.Fatalf("error = %+v, want %q", body.Error, tt.wantCode)
				}
			}
			if (tt.wantStatus == 400 || tt.wantStatus == 403) && len(*env.writes) != 0 {
				t.Fatalf("a refused request reached the cluster: %v", *env.writes)
			}
			if tt.check != nil {
				tt.check(t, rec.Body.Bytes(), *env.writes)
			}
		})
	}
}
