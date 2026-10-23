package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/search"
)

func TestSearchAPI(t *testing.T) {
	// The cluster: one deployment and one pod called web; every other list empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		items := ""
		switch r.URL.Path {
		case "/apis/apps/v1/deployments":
			items = `{"metadata":{"name":"web","namespace":"shop"}}`
		case "/api/v1/pods":
			items = `{"metadata":{"name":"web-5c8d7-q4x2z","namespace":"shop"}}`
		}
		_, _ = w.Write([]byte(`{"kind":"PartialObjectMetadataList","apiVersion":"meta.k8s.io/v1","metadata":{},"items":[` + items + `]}`))
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
		Frontend:    http.NotFoundHandler(),
		Clusters:    cluster.Single(cluster.NewLocalWithClients(cluster.Info{ID: cluster.DefaultID}, &cluster.Clients{Kube: fake.NewClientset()})),
		Auth:        authn,
		TableClient: func(*cluster.Clients) rest.Interface { return tableKube.CoreV1().RESTClient() },
	})
	launch, _ := url.Parse(authn.LaunchURL("http://x", "/"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, launch.RequestURI(), nil))
	cookie := rec.Result().Cookies()[0]

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
		wantHits   []string
	}{
		{name: "finds by name, closest first", query: "?q=web", wantStatus: 200, wantHits: []string{"Deployment shop/web", "Pod shop/web-5c8d7-q4x2z"}},
		{name: "kind words narrow it", query: "?q=pod+web", wantStatus: 200, wantHits: []string{"Pod shop/web-5c8d7-q4x2z"}},
		{name: "no query", query: "?q=+", wantStatus: 400, wantCode: "bad_query"},
		{name: "too long", query: "?q=" + strings.Repeat("a", search.MaxQuery+1), wantStatus: 400, wantCode: "bad_query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/clusters/default/search"+tt.query, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if tt.wantCode != "" {
				if !strings.Contains(rec.Body.String(), `"code":"`+tt.wantCode+`"`) {
					t.Fatalf("body = %s", rec.Body)
				}
				return
			}
			var res search.Result
			mustDecode(t, rec.Body.Bytes(), &res)
			var got []string
			for _, h := range res.Hits {
				got = append(got, h.Kind+" "+h.Namespace+"/"+h.Name)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantHits, ",") {
				t.Fatalf("hits = %v, want %v", got, tt.wantHits)
			}
		})
	}
}
