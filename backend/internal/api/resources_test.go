package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/cached/memory"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/resources"
)

const tablePage = `{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"continue":"p2"},
 "columnDefinitions":[{"name":"Name","type":"string","priority":0},{"name":"Status","type":"string","priority":0}],
 "rows":[{"cells":["api-1","Running"],"object":{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":{"name":"api-1","namespace":"payments","uid":"u1"}}}]}`

func resourceRouter(t *testing.T) (http.Handler, *http.Cookie, *[]string) {
	t.Helper()
	kube := fake.NewClientset()
	kube.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
			{Name: "secrets", Kind: "Secret", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "nodes", Kind: "Node", Verbs: []string{"get", "list", "watch"}},
		}},
	}
	secret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "db", "namespace": "payments"},
		"data":     map[string]any{"password": "aHVudGVyMg=="},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "secrets"}: "SecretList",
		{Version: "v1", Resource: "pods"}:    "PodList",
	}, secret)

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tablePage))
	}))
	t.Cleanup(srv.Close)
	tableKube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	authn, err := auth.NewLocal()
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{
		Clusters: cluster.Single(cluster.NewLocalWithClients(cluster.Info{ID: cluster.DefaultID}, &cluster.Clients{
			Kube: kube, Dynamic: dyn, Discovery: memory.NewMemCacheClient(kube.Discovery()),
		})),
		Auth:        authn,
		TableClient: func(*cluster.Clients) rest.Interface { return tableKube.CoreV1().RESTClient() },
	})
	launch, _ := url.Parse(authn.LaunchURL("http://x", "/"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, launch.RequestURI(), nil))
	return router, rec.Result().Cookies()[0], &seen
}

func TestResourceAPI(t *testing.T) {
	const base = "/api/v1/clusters/default"
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   string
		check      func(t *testing.T, body []byte, upstream []string)
	}{
		{
			name: "types", path: base + "/resources", wantStatus: 200,
			check: func(t *testing.T, body []byte, _ []string) {
				var r typesResponse
				mustDecode(t, body, &r)
				if len(r.Types) != 3 || r.Types[0].Resource != "nodes" {
					t.Fatalf("types = %+v", r.Types)
				}
			},
		},
		{
			name: "list passes paging and selectors to the API server", wantStatus: 200,
			path: base + "/resources/core/v1/pods?namespace=payments&limit=25&continue=p1&labelSelector=app%3Dapi",
			check: func(t *testing.T, body []byte, upstream []string) {
				var tbl resources.Table
				mustDecode(t, body, &tbl)
				if len(tbl.Rows) != 1 || tbl.Rows[0].Object.Name != "api-1" || tbl.Continue != "p2" || tbl.Type.Kind != "Pod" {
					t.Fatalf("table = %+v", tbl)
				}
				if len(upstream) != 1 || !strings.HasPrefix(upstream[0], "/api/v1/namespaces/payments/pods?") ||
					!strings.Contains(upstream[0], "limit=25") || !strings.Contains(upstream[0], "continue=p1") ||
					!strings.Contains(upstream[0], "labelSelector=app%3Dapi") {
					t.Fatalf("upstream = %v", upstream)
				}
			},
		},
		{name: "unknown type", path: base + "/resources/example.com/v1/widgets", wantStatus: 404, wantCode: "unknown_resource"},
		{name: "bad label selector", path: base + "/resources/core/v1/pods?labelSelector=a%3D%3D%3Db", wantStatus: 400, wantCode: "bad_selector"},
		{name: "bad field selector", path: base + "/resources/core/v1/pods?fieldSelector=status.phase", wantStatus: 400, wantCode: "bad_selector"},
		{name: "bad limit", path: base + "/resources/core/v1/pods?limit=9000", wantStatus: 400, wantCode: "bad_limit"},
		{name: "bad namespace", path: base + "/resources/core/v1/pods?namespace=Nope!", wantStatus: 400, wantCode: "bad_namespace"},
		{
			name: "secret is redacted", path: base + "/resources/core/v1/secrets/db?namespace=payments&format=yaml", wantStatus: 200,
			check: func(t *testing.T, body []byte, _ []string) {
				if strings.Contains(string(body), "aHVudGVyMg") {
					t.Fatalf("secret value leaked: %s", body)
				}
				var o resources.Object
				mustDecode(t, body, &o)
				if !o.Redacted || !strings.Contains(o.YAML, "password: \"\"") {
					t.Fatalf("object = %+v", o)
				}
			},
		},
		{
			name: "secret revealed on request", path: base + "/resources/core/v1/secrets/db?namespace=payments&reveal=true", wantStatus: 200,
			check: func(t *testing.T, body []byte, _ []string) {
				if !strings.Contains(string(body), "aHVudGVyMg") || strings.Contains(string(body), `"redacted":true`) {
					t.Fatalf("body = %s", body)
				}
			},
		},
		{name: "namespaced get needs a namespace", path: base + "/resources/core/v1/secrets/db", wantStatus: 400, wantCode: "namespace_required"},
		{name: "missing object", path: base + "/resources/core/v1/pods/nope?namespace=payments", wantStatus: 404, wantCode: "not_found"},
		{name: "secrets can't be watched", path: base + "/watch/core/v1/secrets", wantStatus: 400, wantCode: "not_watchable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, cookie, upstream := resourceRouter(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode != "" {
				var body struct {
					Error errorBody `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != tt.wantCode {
					t.Fatalf("error = %+v (%v), want %s", body.Error, err, tt.wantCode)
				}
			}
			if tt.check != nil {
				tt.check(t, rec.Body.Bytes(), *upstream)
			}
		})
	}
}
