package stream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/summary"
)

var fastBackoff = func() Backoff { return Backoff{Initial: time.Millisecond, Max: 5 * time.Millisecond} }

func testPod(name, rv string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: name, ResourceVersion: rv},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func gone() *metav1.Status {
	return &metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired, Message: "too old resource version"}
}

// watchers hands out fake watchers in order and records the options used.
type watchers struct {
	queue chan *watch.FakeWatcher
	rvs   chan string
}

func newWatchers(n int) *watchers {
	w := &watchers{queue: make(chan *watch.FakeWatcher, n), rvs: make(chan string, n)}
	for range n {
		w.queue <- watch.NewFakeWithChanSize(10, false)
	}
	return w
}

func (w *watchers) reactor(action k8stesting.Action) (bool, watch.Interface, error) {
	if wa, ok := action.(k8stesting.WatchActionImpl); ok {
		w.rvs <- wa.WatchRestrictions.ResourceVersion
	}
	select {
	case fw := <-w.queue:
		return true, fw, nil
	default:
		return true, watch.NewFake(), nil // idle until the test ends
	}
}

func collect(ctx context.Context, t *testing.T, src Source, want int) []Message {
	t.Helper()
	got := make(chan Message, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, src, func(m Message) bool { got <- m; return true }, fastBackoff())
	}()
	var out []Message
	for len(out) < want {
		select {
		case m := <-got:
			out = append(out, m)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out after %d of %d messages: %+v", len(out), want, out)
		}
	}
	return out
}

func TestRunRelistsOn410(t *testing.T) {
	kube := fake.NewClientset()
	var lists atomic.Int32
	kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		n := lists.Add(1)
		return true, &corev1.PodList{ListMeta: metav1.ListMeta{ResourceVersion: map[int32]string{1: "100", 2: "500"}[n]}}, nil
	})
	ws := newWatchers(2)
	kube.PrependWatchReactor("pods", ws.reactor)

	first := <-ws.queue
	second := <-ws.queue
	ws.queue <- first
	ws.queue <- second
	first.Add(testPod("api-1", "101"))
	first.Error(gone())
	second.Modify(testPod("api-1", "501"))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	msgs := collect(ctx, t, PodSource(kube, "payments", time.Now), 3)

	if msgs[0].Type != "pod" || msgs[0].Op != "added" || msgs[0].Pod.Name != "api-1" {
		t.Fatalf("first = %+v", msgs[0])
	}
	if msgs[1].Type != "resync" || msgs[1].Resource != "pods" {
		t.Fatalf("second = %+v, want a resync after 410", msgs[1])
	}
	if msgs[2].Type != "pod" || msgs[2].Op != "modified" {
		t.Fatalf("third = %+v", msgs[2])
	}
	if lists.Load() != 2 {
		t.Fatalf("lists = %d, want a relist after 410", lists.Load())
	}
	if rv1, rv2 := <-ws.rvs, <-ws.rvs; rv1 != "100" || rv2 != "500" {
		t.Fatalf("watched from %q then %q, want 100 then the relisted 500", rv1, rv2)
	}
}

func TestRunReWatchesFromLastVersionWhenAWatchEnds(t *testing.T) {
	kube := fake.NewClientset()
	kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.PodList{ListMeta: metav1.ListMeta{ResourceVersion: "10"}}, nil
	})
	ws := newWatchers(2)
	kube.PrependWatchReactor("pods", ws.reactor)
	first := <-ws.queue
	ws.queue <- first
	ws.queue <- <-ws.queue
	first.Add(testPod("a", "11"))
	first.Action(watch.Bookmark, testPod("", "42"))
	first.Stop() // server timeout

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	collect(ctx, t, PodSource(kube, "", time.Now), 1)
	if rv1, rv2 := <-ws.rvs, <-ws.rvs; rv1 != "10" || rv2 != "42" {
		t.Fatalf("watched from %q then %q, want 10 then the bookmark's 42", rv1, rv2)
	}
}

func TestRunStopsWhenForbidden(t *testing.T) {
	kube := fake.NewClientset()
	kube.PrependReactor("list", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("rbac"))
	})
	done := make(chan struct{})
	var got []Message
	go func() {
		defer close(done)
		Run(t.Context(), EventSource(kube, ""), func(m Message) bool { got = append(got, m); return true }, fastBackoff())
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run kept going after a forbidden list")
	}
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "forbidden" || got[0].Resource != "events" {
		t.Fatalf("got %+v", got)
	}
}

func TestRunRetriesWatchErrors(t *testing.T) {
	kube := fake.NewClientset()
	var attempts atomic.Int32
	fw := watch.NewFakeWithChanSize(10, false)
	fw.Add(&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", ResourceVersion: "2"}, Reason: "Scheduled"})
	fw.Delete(&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e0", ResourceVersion: "3"}})
	fw.Modify(&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", ResourceVersion: "4"}, Reason: "Scheduled", Count: 2})
	kube.PrependWatchReactor("events", func(k8stesting.Action) (bool, watch.Interface, error) {
		if attempts.Add(1) < 3 {
			return true, nil, errors.New("connection reset")
		}
		return true, fw, nil
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	msgs := collect(ctx, t, EventSource(kube, ""), 2)
	if msgs[0].Op != "added" || msgs[1].Op != "modified" || msgs[1].Event.Count != 2 {
		t.Fatalf("got %+v; deletions should be skipped", msgs)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
}

func TestHandler(t *testing.T) {
	kube := fake.NewClientset()
	fw := watch.NewFakeWithChanSize(10, false)
	kube.PrependWatchReactor("pods", func(k8stesting.Action) (bool, watch.Interface, error) { return true, fw, nil })

	h := &Handler{
		Clients: func(*http.Request) *cluster.Clients { return &cluster.Clients{Kube: kube} },
		Backoff: fastBackoff,
	}
	withIdentity := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), auth.LocalIdentity)))
	})
	srv := httptest.NewServer(withIdentity)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/?namespace=payments"

	t.Run("streams pod changes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		c, resp, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Body != nil {
			resp.Body.Close()
		}
		defer c.CloseNow() //nolint:errcheck

		var m Message
		if err := wsjson.Read(ctx, c, &m); err != nil || m.Type != "ready" {
			t.Fatalf("first message = %+v, %v", m, err)
		}
		fw.Add(testPod("api-1", "7"))
		for m.Type != "pod" {
			if err := wsjson.Read(ctx, c, &m); err != nil {
				t.Fatal(err)
			}
		}
		if m.Pod.Name != "api-1" || m.Pod.Namespace != "payments" {
			t.Fatalf("pod message = %+v", m.Pod)
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	})

	t.Run("rejects other origins", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"https://evil.example.com"}},
		})
		if err == nil {
			t.Fatal("cross-origin WebSocket was accepted")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("response = %+v", resp)
		}
		resp.Body.Close()
	})

	t.Run("needs an identity", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestListSourceSaysWhatChanged(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "DeploymentList"})
	fw := watch.NewFakeWithChanSize(10, false)
	var selector string
	client.PrependWatchReactor("deployments", func(a k8stesting.Action) (bool, watch.Interface, error) {
		selector = a.(k8stesting.WatchActionImpl).WatchRestrictions.Labels.String()
		return true, fw, nil
	})
	dep := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "web", "namespace": "shop", "resourceVersion": "9"},
	}}
	fw.Modify(dep)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	msgs := collect(ctx, t, ListSource(client, gvr, true, "shop", "app=web", ""), 1)
	m := msgs[0]
	if m.Type != "changed" || m.Op != "modified" || m.Ref == nil || *m.Ref != (summary.ObjectRef{Kind: "Deployment", Namespace: "shop", Name: "web"}) {
		t.Fatalf("message = %+v ref %+v", m, m.Ref)
	}
	if selector != "app=web" {
		t.Fatalf("watch selector = %q", selector)
	}
}
