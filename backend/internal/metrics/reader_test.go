package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// apiServer fakes the few API server paths the reader uses.
func apiServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) kubernetes.Interface {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))
	}))
	t.Cleanup(srv.Close)
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return kube
}

func body(s string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s))
	}
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","code":` + strconv.Itoa(code) + `}`))
	}
}

const nodeMetrics = `{"kind":"NodeMetricsList","items":[
 {"metadata":{"name":"cp1"},"usage":{"cpu":"187113504n","memory":"1961184Ki"}},
 {"metadata":{"name":"w1"},"usage":{"cpu":"250m","memory":"1Gi"}}]}`

const podMetrics = `{"kind":"PodMetricsList","items":[
 {"metadata":{"name":"api","namespace":"payments"},"containers":[
   {"name":"app","usage":{"cpu":"100m","memory":"64Mi"}},
   {"name":"proxy","usage":{"cpu":"50m","memory":"32Mi"}}]}]}`

func TestNodeUsage(t *testing.T) {
	tests := []struct {
		name    string
		route   func(http.ResponseWriter, *http.Request)
		wantErr error
		anyErr  bool
	}{
		{name: "metrics-server answers", route: body(nodeMetrics)},
		{name: "API not registered", route: nil, wantErr: ErrNoMetrics},
		{name: "APIService unavailable", route: status(http.StatusServiceUnavailable), wantErr: ErrNoMetrics},
		{name: "forbidden", route: status(http.StatusForbidden), anyErr: true},
		{name: "garbage", route: body("{"), anyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := map[string]func(http.ResponseWriter, *http.Request){}
			if tt.route != nil {
				routes["/apis/metrics.k8s.io/v1beta1/nodes"] = tt.route
			}
			got, err := NewReader(apiServer(t, routes)).NodeUsage(context.Background())
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			case tt.anyErr:
				if err == nil || errors.Is(err, ErrNoMetrics) {
					t.Fatalf("err = %v, want a real error", err)
				}
				return
			case err != nil:
				t.Fatal(err)
			}
			if !near(got["cp1"].CPU, 0.187113504) || !near(got["cp1"].Memory, 1961184*1024) {
				t.Fatalf("cp1 = %+v", got["cp1"])
			}
			if !near(got["w1"].CPU, 0.25) || !near(got["w1"].Memory, gib) {
				t.Fatalf("w1 = %+v", got["w1"])
			}
		})
	}
}

func TestPodUsage(t *testing.T) {
	kube := apiServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/apis/metrics.k8s.io/v1beta1/pods":                     body(podMetrics),
		"/apis/metrics.k8s.io/v1beta1/namespaces/payments/pods": body(podMetrics),
	})
	for _, ns := range []string{"", "payments"} {
		got, err := NewReader(kube).PodUsage(context.Background(), ns)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "api" || !near(got[0].CPU, 0.15) || !near(got[0].Memory, 96*1024*1024) {
			t.Fatalf("namespace %q: got %+v", ns, got)
		}
	}
}

func TestVolumeUsage(t *testing.T) {
	summary := `{"pods":[{"volume":[
	  {"name":"data","usedBytes":1073741824,"pvcRef":{"name":"data-0","namespace":"db"}},
	  {"name":"tmp","usedBytes":5},
	  {"name":"hostpath","pvcRef":{"name":"local","namespace":"db"}}]}]}`
	routes := map[string]func(http.ResponseWriter, *http.Request){
		"/api/v1/nodes/w1/proxy/stats/summary": body(summary),
		"/api/v1/nodes/w2/proxy/stats/summary": status(http.StatusBadGateway),
	}
	kube := apiServer(t, routes)

	got, err := NewReader(kube).VolumeUsage(context.Background(), []string{"w1", "w2"})
	if err != nil {
		t.Fatalf("one healthy node is enough: %v", err)
	}
	if len(got) != 1 || got[PVCKey{"db", "data-0"}] != gib {
		t.Fatalf("got %v", got)
	}

	if _, err := NewReader(kube).VolumeUsage(context.Background(), []string{"w2"}); err == nil {
		t.Fatal("want an error when no kubelet answers")
	}
}

func TestDiscover(t *testing.T) {
	svc := func(ns, name string, labels map[string]string, ports ...corev1.ServicePort) *corev1.Service {
		return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels}, Spec: corev1.ServiceSpec{Ports: ports}}
	}
	tests := []struct {
		name    string
		objects []*corev1.Service
		want    *Target
	}{
		{name: "none", objects: []*corev1.Service{svc("default", "web", map[string]string{"app": "web"}, corev1.ServicePort{Port: 80})}},
		{
			name: "operator service preferred",
			objects: []*corev1.Service{
				svc("other", "prometheus-server", map[string]string{"app.kubernetes.io/name": "prometheus"}, corev1.ServicePort{Name: "http", Port: 80}),
				svc("monitoring", "prometheus-operated", map[string]string{"operated-prometheus": "true"}, corev1.ServicePort{Name: "http-web", Port: 9090}),
			},
			want: &Target{Namespace: "monitoring", Service: "prometheus-operated", Port: 9090, Scheme: "http"},
		},
		{
			name:    "community chart on port 80",
			objects: []*corev1.Service{svc("obs", "prometheus-server", map[string]string{"app.kubernetes.io/name": "prometheus"}, corev1.ServicePort{Name: "http", Port: 80})},
			want:    &Target{Namespace: "obs", Service: "prometheus-server", Port: 80, Scheme: "http"},
		},
		{
			name:    "service without a usable port",
			objects: []*corev1.Service{svc("obs", "prometheus", map[string]string{"app": "prometheus"}, corev1.ServicePort{Name: "grpc", Port: 10901})},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kube := fake.NewClientset()
			for _, s := range tt.objects {
				if _, err := kube.CoreV1().Services(s.Namespace).Create(context.Background(), s, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Discover(context.Background(), kube)
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
