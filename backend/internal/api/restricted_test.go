package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/overview"
	"glowpod/internal/pods"
)

// A person who may list things in team-a only: nothing cluster-wide, no
// nodes, no namespaces.
func restrictedKube() *fake.Clientset {
	kube := fake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "api-1"},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app", RestartCount: 9,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}}},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "other-1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	)
	kube.PrependReactor("*", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetVerb() == "list" && a.GetNamespace() == "team-a" {
			return false, nil, nil
		}
		gr := schema.GroupResource{Group: a.GetResource().Group, Resource: a.GetResource().Resource}
		return true, nil, apierrors.NewForbidden(gr, "", errors.New("rbac"))
	})
	return kube
}

func restrictedRouter(t *testing.T, info cluster.Info) (http.Handler, *http.Cookie) {
	t.Helper()
	authn, err := auth.NewLocal()
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{
		Frontend: http.NotFoundHandler(),
		Clusters: cluster.Single(cluster.NewLocalWithClients(info, &cluster.Clients{Kube: restrictedKube()})),
		Auth:     authn,
		Metrics:  func(kubernetes.Interface) overview.MetricsSource { return noMetrics{} },
		PodUsage: func(kubernetes.Interface) pods.UsageSource { return nil },
	})
	launch, _ := url.Parse(authn.LaunchURL("http://x", "/"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, launch.RequestURI(), nil))
	return router, rec.Result().Cookies()[0]
}

func TestSomeNamespacesOnly(t *testing.T) {
	tests := []struct {
		name       string
		contextNS  string
		path       string
		wantStatus int
		wantCode   string
		// wantProblems are the pods "Needs attention" lists.
		wantProblems []string
	}{
		{
			name: "the Clusters page row says to pick a namespace when none is known",
			path: "summary", wantStatus: 403, wantCode: "namespace_needed",
		},
		{
			name: "the Overview says so too",
			path: "overview", wantStatus: 403, wantCode: "namespace_needed",
		},
		{
			name: "the row uses the namespaces picked in the cluster",
			path: "summary?namespace=team-a", wantStatus: 200, wantProblems: []string{"api-1"},
		},
		{
			name:      "the kubeconfig context's namespace is theirs",
			contextNS: "team-a", path: "summary", wantStatus: 200, wantProblems: []string{"api-1"},
		},
		{
			name:      "so is the Overview's",
			contextNS: "team-a", path: "overview", wantStatus: 200, wantProblems: []string{"api-1"},
		},
		{
			name: "a picked namespace they can't see is still refused",
			path: "summary?namespace=team-b", wantStatus: 403, wantCode: "forbidden",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, cookie := restrictedRouter(t, cluster.Info{ID: cluster.DefaultID, Name: "lab", Namespace: tt.contextNS})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/clusters/default/"+tt.path, nil)
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
			var body struct {
				Problems []struct {
					Name string `json:"name"`
				} `json:"problems"`
			}
			mustDecode(t, rec.Body.Bytes(), &body)
			var got []string
			for _, p := range body.Problems {
				got = append(got, p.Name)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantProblems, ",") {
				t.Fatalf("problems = %v, want %v", got, tt.wantProblems)
			}
		})
	}
}
