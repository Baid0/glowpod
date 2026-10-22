package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/pods"
)

type noMetrics struct{}

func (noMetrics) NodeUsage(context.Context) (map[string]metrics.Usage, error) {
	return nil, metrics.ErrNoMetrics
}

func (noMetrics) PodUsage(context.Context, string) ([]metrics.PodUsage, error) {
	return nil, metrics.ErrNoMetrics
}

func (noMetrics) VolumeUsage(context.Context, []string) (map[metrics.PVCKey]float64, error) {
	return map[metrics.PVCKey]float64{}, nil
}

func event(ns, name, reason string, at time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: ns, Name: name},
		Reason:         reason,
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: ns, Name: "api"},
		LastTimestamp:  metav1.NewTime(at),
	}
}

func objects() []runtime.Object {
	now := time.Now()
	return []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "w1"},
			Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi"), corev1.ResourcePods: resource.MustParse("110"),
			}},
		},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "zeta"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "alpha", Name: "api-1", UID: "u1"},
			Spec:       corev1.PodSpec{NodeName: "w1", Containers: []corev1.Container{{Name: "app", Image: "api:v1"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}},
		event("alpha", "old", "Pulled", now.Add(-time.Hour)),
		event("alpha", "new", "BackOff", now.Add(-time.Minute)),
		event("zeta", "mid", "Scheduled", now.Add(-10*time.Minute)),
	}
}

// testRouter returns a router and a signed-in cookie.
func testRouter(t *testing.T, kube *fake.Clientset) (http.Handler, *http.Cookie) {
	t.Helper()
	authn, err := auth.NewLocal()
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{
		Frontend: http.NotFoundHandler(),
		Provider: cluster.NewLocalWithClients(cluster.Info{ID: cluster.DefaultID, Name: "lab"}, &cluster.Clients{Kube: kube}),
		Auth:     authn,
		Hosts:    auth.LoopbackHosts("7171"),
		Metrics:  func(kubernetes.Interface) overview.MetricsSource { return noMetrics{} },
		PodUsage: func(kubernetes.Interface) pods.UsageSource { return nil },
	})

	launch, err := url.Parse(authn.LaunchURL("http://127.0.0.1:7171"))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, launch.RequestURI(), nil)
	req.Host = "127.0.0.1:7171"
	router.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if rec.Code != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatalf("sign-in failed: %d %v", rec.Code, cookies)
	}
	return router, cookies[0]
}

func TestAPI(t *testing.T) {
	forbidNodes := func(k *fake.Clientset) {
		k.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "", errors.New("rbac"))
		})
	}

	tests := []struct {
		name       string
		method     string
		path       string
		host       string
		noCookie   bool
		setup      func(*fake.Clientset)
		wantStatus int
		wantCode   string
		check      func(t *testing.T, body []byte)
	}{
		{name: "session needs sign-in", path: "/api/v1/session", noCookie: true, wantStatus: 401, wantCode: "unauthenticated"},
		{
			name: "session", path: "/api/v1/session", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var s sessionResponse
				mustDecode(t, body, &s)
				if s.Mode != "local" || len(s.Clusters) != 1 || s.Clusters[0].ID != "default" || s.Clusters[0].Name != "lab" {
					t.Fatalf("session = %+v", s)
				}
			},
		},
		{name: "wrong host", path: "/api/v1/session", host: "evil.example.com:7171", wantStatus: http.StatusMisdirectedRequest},
		{name: "unknown cluster", path: "/api/v1/clusters/prod/overview", wantStatus: 404, wantCode: "unknown_cluster"},
		{name: "bad namespace", path: "/api/v1/clusters/default/overview?namespace=Not_Valid", wantStatus: 400, wantCode: "bad_namespace"},
		{
			name: "overview", path: "/api/v1/clusters/default/overview", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var o overview.Response
				mustDecode(t, body, &o)
				if o.Cluster.Nodes != 1 || o.Gauges.CPU.State != metrics.NoMetrics || o.Gauges.Network != nil || o.Gauges.Pods.Max != 110 {
					t.Fatalf("overview = %+v", o)
				}
				if o.Problems == nil {
					t.Fatal("problems must be [] not null")
				}
			},
		},
		{name: "overview when nodes are forbidden", path: "/api/v1/clusters/default/overview", setup: forbidNodes, wantStatus: 403, wantCode: "forbidden"},
		{
			name: "namespaces are sorted", path: "/api/v1/clusters/default/namespaces", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var n namespacesResponse
				mustDecode(t, body, &n)
				if strings.Join(n.Namespaces, ",") != "alpha,zeta" {
					t.Fatalf("namespaces = %v", n.Namespaces)
				}
			},
		},
		{
			name: "events newest first", path: "/api/v1/clusters/default/events?limit=2", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var e eventsResponse
				mustDecode(t, body, &e)
				if len(e.Events) != 2 || e.Events[0].Reason != "BackOff" || e.Events[1].Reason != "Scheduled" {
					t.Fatalf("events = %+v", e.Events)
				}
			},
		},
		{
			name: "events in one namespace", path: "/api/v1/clusters/default/events?namespace=zeta", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var e eventsResponse
				mustDecode(t, body, &e)
				if len(e.Events) != 1 || e.Events[0].Object.Namespace != "zeta" {
					t.Fatalf("events = %+v", e.Events)
				}
			},
		},
		{name: "bad event limit", path: "/api/v1/clusters/default/events?limit=5000", wantStatus: 400, wantCode: "bad_limit"},
		{
			name: "pod detail", path: "/api/v1/clusters/default/pods/alpha/api-1", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var d pods.Detail
				mustDecode(t, body, &d)
				if d.Pod.Name != "api-1" || len(d.Containers) != 1 || d.Containers[0].Image != "api:v1" || d.Story == nil {
					t.Fatalf("detail = %+v", d)
				}
			},
		},
		{name: "missing pod", path: "/api/v1/clusters/default/pods/alpha/nope", wantStatus: 404, wantCode: "not_found"},
		{
			name: "apps", path: "/api/v1/clusters/default/apps?namespace=alpha", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var r appsResponse
				mustDecode(t, body, &r)
				if r.Apps == nil {
					t.Fatal("apps must be [] not null")
				}
			},
		},
		{name: "missing app", path: "/api/v1/clusters/default/apps/alpha/nope", wantStatus: 404, wantCode: "not_found"},
		{name: "app in a bad namespace", path: "/api/v1/clusters/default/apps/Bad_NS/x", wantStatus: 400, wantCode: "bad_namespace"},
		{
			name: "log download", path: "/api/v1/clusters/default/logs/alpha/api-1/download?container=app", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				if string(body) != "fake logs" {
					t.Fatalf("body = %q", body)
				}
			},
		},
		{name: "bad container name", path: "/api/v1/clusters/default/logs/alpha/api-1?container=Bad_Name", wantStatus: 400, wantCode: "bad_container"},
		{name: "bad tail", path: "/api/v1/clusters/default/logs/alpha/api-1?tailLines=0", wantStatus: 400, wantCode: "bad_tail"},
		{
			name: "node detail", path: "/api/v1/clusters/default/nodes/w1", wantStatus: 200,
			check: func(t *testing.T, body []byte) {
				var got struct {
					Name   string
					Gauges struct{ Pods struct{ Max float64 } }
				}
				mustDecode(t, body, &got)
				if got.Name != "w1" || got.Gauges.Pods.Max != 110 {
					t.Fatalf("node = %+v", got)
				}
			},
		},
		{name: "node missing", path: "/api/v1/clusters/default/nodes/w9", wantStatus: 404, wantCode: "not_found"},
		{name: "node bad name", path: "/api/v1/clusters/default/nodes/Bad_Node", wantStatus: 400, wantCode: "bad_node"},
		{name: "drain plan", path: "/api/v1/clusters/default/nodes/w1/drain", wantStatus: 200},
		{name: "events need kind and name together", path: "/api/v1/clusters/default/events?kind=Pod", wantStatus: 400, wantCode: "bad_object"},
		{
			name: "one object's events", path: "/api/v1/clusters/default/events?kind=Pod&name=api&namespace=alpha", wantStatus: 200,
			setup: func(k *fake.Clientset) {
				k.PrependReactor("list", "events", func(a k8stesting.Action) (bool, runtime.Object, error) {
					if got := a.(k8stesting.ListAction).GetListRestrictions().Fields.String(); got != "involvedObject.kind=Pod,involvedObject.name=api" {
						t.Errorf("field selector = %q", got)
					}
					return false, nil, nil
				})
			},
		},
		{name: "exec needs a container", path: "/api/v1/clusters/default/exec/alpha/api-1", wantStatus: 400, wantCode: "bad_container"},
		{name: "exec bad pod name", path: "/api/v1/clusters/default/exec/alpha/Bad_Pod?container=app", wantStatus: 400, wantCode: "bad_pod"},
		{name: "pod in a bad namespace", path: "/api/v1/clusters/default/pods/Not_Valid/x", wantStatus: 400, wantCode: "bad_namespace"},
		{name: "unknown api path", path: "/api/v1/nope", wantStatus: 404, wantCode: "not_found"},
		{name: "write without csrf token", method: http.MethodPost, path: "/api/v1/clusters/default/overview", wantStatus: 403, wantCode: "csrf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kube := fake.NewClientset(objects()...)
			if tt.setup != nil {
				tt.setup(kube)
			}
			router, cookie := testRouter(t, kube)

			method := cmpOr(tt.method, http.MethodGet)
			req := httptest.NewRequestWithContext(t.Context(), method, tt.path, nil)
			req.Host = cmpOr(tt.host, "127.0.0.1:7171")
			if !tt.noCookie {
				req.AddCookie(cookie)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode != "" {
				var body struct {
					Error errorBody `json:"error"`
				}
				mustDecode(t, rec.Body.Bytes(), &body)
				if body.Error.Code != tt.wantCode || body.Error.Message == "" {
					t.Fatalf("error = %+v, want code %q", body.Error, tt.wantCode)
				}
			}
			if tt.check != nil {
				tt.check(t, rec.Body.Bytes())
			}
		})
	}
}

func mustDecode(t *testing.T, body []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func cmpOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
