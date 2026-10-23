package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// fakeCluster lists objects by "resource/namespace/name", as one person.
type fakeCluster struct {
	objects []string
	// forbidden are "resource" (everywhere) or "resource/namespace".
	forbidden []string
	err       error
	calls     atomic.Int32
}

func (f *fakeCluster) list(_ context.Context, k Kind, namespace, cont string) ([]entry, string, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, "", f.err
	}
	gr := schema.GroupResource{Group: k.Group, Resource: k.Resource}
	if slices.Contains(f.forbidden, k.Resource) || slices.Contains(f.forbidden, k.Resource+"/"+namespace) {
		return nil, "", apierrors.NewForbidden(gr, "", errors.New("rbac"))
	}
	var out []entry
	for _, o := range f.objects {
		parts := strings.SplitN(o, "/", 3)
		if parts[0] == k.Resource && (namespace == "" || parts[1] == namespace) {
			out = append(out, entry{namespace: parts[1], name: parts[2]})
		}
	}
	// Two pages: the first object, then the rest.
	if cont == "" && len(out) > 1 {
		return out[:1], "next", nil
	}
	if cont == "next" {
		return out[1:], "", nil
	}
	return out, "", nil
}

func names(r Result) []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, fmt.Sprintf("%s %s/%s", h.Kind, h.Namespace, h.Name))
	}
	return out
}

func TestMatch(t *testing.T) {
	f := &fakeCluster{objects: []string{
		"pods/payments/payments-api-7d9f-x2kq9",
		"pods/checkout/checkout-web-1",
		"deployments/payments/payments-api",
		"deployments/payments/api",
		"services/payments/payments-api",
		"services/search/search-api",
		"secrets/payments/payments-api-db",
		"namespaces//payments",
		"nodes//ip-10-0-1-21",
	}}
	idx, err := build(t.Context(), f.list, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		query string
		want  []string
	}{
		// Exact name first, then names that start with it, then the rest;
		// closer (shorter) names before longer ones, then by kind.
		{"api", []string{
			"Deployment payments/api",
			"Service search/search-api",
			"Deployment payments/payments-api",
			"Service payments/payments-api",
			"Secret payments/payments-api-db",
			"Pod payments/payments-api-7d9f-x2kq9",
		}},
		// Every word must match: the name, the namespace or the kind.
		{"deploy api", []string{"Deployment payments/api", "Deployment payments/payments-api"}},
		{"svc search", []string{"Service search/search-api"}},
		{"PAYMENTS-API", []string{
			"Deployment payments/payments-api",
			"Service payments/payments-api",
			"Secret payments/payments-api-db",
			"Pod payments/payments-api-7d9f-x2kq9",
		}},
		// A namespace or kind alone lists what's in it.
		{"pod checkout", []string{"Pod checkout/checkout-web-1"}},
		{"ip-10", []string{"Node /ip-10-0-1-21"}},
		{"nothing-like-this", []string{}},
		{"   ", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := names(idx.match(tt.query))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("match(%q) =\n%v\nwant\n%v", tt.query, got, tt.want)
			}
		})
	}
}

func TestMatchLimit(t *testing.T) {
	f := &fakeCluster{}
	for i := range MaxHits + 5 {
		f.objects = append(f.objects, fmt.Sprintf("configmaps/apps/web-%02d", i))
	}
	idx, err := build(t.Context(), f.list, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := idx.match("web")
	if len(r.Hits) != MaxHits || !r.More || r.Hits[0].Name != "web-00" {
		t.Fatalf("hits = %d more = %v first = %+v", len(r.Hits), r.More, r.Hits[0])
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name        string
		cluster     *fakeCluster
		fallback    []string
		query       string
		want        []string
		wantSkipped []string
		wantDenied  bool
		wantErr     bool
	}{
		{
			name: "kinds they can't list are skipped, the rest still found",
			cluster: &fakeCluster{
				objects:   []string{"pods/a/web-1", "secrets/a/web-tls"},
				forbidden: []string{"secrets", "nodes"},
			},
			query: "web", want: []string{"Pod a/web-1"}, wantSkipped: []string{"nodes", "secrets"},
		},
		{
			name: "people who can't list all namespaces are searched in theirs",
			cluster: &fakeCluster{
				objects:   []string{"pods/team-a/web-1", "pods/team-b/web-2", "deployments/team-a/web"},
				forbidden: []string{"pods/", "deployments/", "pods/team-b", "nodes", "namespaces"},
			},
			fallback: []string{"team-a", "team-b"},
			query:    "web", want: []string{"Deployment team-a/web", "Pod team-a/web-1"},
			// team-b's pods were forbidden: pods are searched in part.
			wantSkipped: []string{"namespaces", "nodes", "pods"},
		},
		{
			name: "someone who may list nothing in all namespaces, and gave none, is told",
			cluster: &fakeCluster{
				objects:   []string{"pods/team-a/web-1"},
				forbidden: []string{"pods", "deployments", "statefulsets", "daemonsets", "cronjobs", "jobs", "services", "ingresses", "configmaps", "secrets", "persistentvolumeclaims", "namespaces", "nodes"},
			},
			query: "web", want: []string{}, wantDenied: true,
			wantSkipped: []string{"configmaps", "cronjobs", "daemonsets", "deployments", "ingresses", "jobs", "namespaces", "nodes", "persistentvolumeclaims", "pods", "secrets", "services", "statefulsets"},
		},
		{
			name:    "a cluster that doesn't answer is an error",
			cluster: &fakeCluster{err: errors.New("dial tcp: connection refused")},
			query:   "web", wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, err := build(t.Context(), tt.cluster.list, tt.fallback)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := idx.match(tt.query)
			if got := names(r); !slices.Equal(got, tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
			if !slices.Equal(r.Skipped, tt.wantSkipped) || r.Denied != tt.wantDenied {
				t.Fatalf("skipped = %v denied = %v, want %v %v", r.Skipped, r.Denied, tt.wantSkipped, tt.wantDenied)
			}
		})
	}
}

func TestCache(t *testing.T) {
	f := &fakeCluster{objects: []string{"pods/a/web-1"}}
	now := time.Unix(1_000_000, 0)
	c := NewCache(30 * time.Second)
	c.now = func() time.Time { return now }

	search := func(key, q string) Result {
		t.Helper()
		r, err := c.Search(t.Context(), key, q, f.list, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	search("lab\x00alice", "web")
	perBuild := f.calls.Load()
	// Typing more reuses the index.
	if r := search("lab\x00alice", "web-1"); len(r.Hits) != 1 || f.calls.Load() != perBuild {
		t.Fatalf("hits = %v, calls = %d", r.Hits, f.calls.Load())
	}
	// Someone else gets their own.
	search("lab\x00bob", "web")
	if f.calls.Load() != 2*perBuild {
		t.Fatalf("bob reused alice's index: %d calls", f.calls.Load())
	}
	// After the TTL, it's listed again.
	now = now.Add(31 * time.Second)
	search("lab\x00alice", "web")
	if f.calls.Load() != 3*perBuild {
		t.Fatalf("stale index reused: %d calls", f.calls.Load())
	}

	// Errors aren't kept.
	f.err = errors.New("down")
	if _, err := c.Search(t.Context(), "lab\x00carol", "web", f.list, nil); err == nil {
		t.Fatal("want an error")
	}
	f.err = nil
	if _, err := c.Search(t.Context(), "lab\x00carol", "web", f.list, nil); err != nil {
		t.Fatalf("error was kept: %v", err)
	}
}

func TestCacheBuildOutlivesRequest(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	slow := func(ctx context.Context, _ Kind, _, _ string) ([]entry, string, error) {
		calls.Add(1)
		<-release
		return []entry{{namespace: "a", name: "web-1"}}, "", ctx.Err()
	}
	c := NewCache(time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	errs := make(chan error, 1)
	go func() {
		_, err := c.Search(ctx, "k", "web", slow, nil)
		errs <- err
	}()
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel() // the person typed another letter
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	close(release)
	r, err := c.Search(t.Context(), "k", "web", slow, nil)
	if err != nil || len(r.Hits) != len(Kinds) {
		t.Fatalf("hits = %v, err = %v", r.Hits, err)
	}
	if n := calls.Load(); n != int32(len(Kinds)) {
		t.Fatalf("listed %d times; the build should have finished and been reused", n)
	}
}

func TestRESTLister(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Accept")+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PartialObjectMetadataList","apiVersion":"meta.k8s.io/v1","metadata":{"continue":"c2"},
			"items":[{"metadata":{"name":"db","namespace":"payments"}}]}`))
	}))
	t.Cleanup(srv.Close)
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	list := RESTLister(kube.CoreV1().RESTClient())
	secrets := Kinds[slices.IndexFunc(Kinds, func(k Kind) bool { return k.Resource == "secrets" })]
	got, next, err := list(t.Context(), secrets, "payments", "c1")
	if err != nil || len(got) != 1 || got[0].name != "db" || next != "c2" {
		t.Fatalf("got %v %q %v", got, next, err)
	}
	ingresses := Kinds[slices.IndexFunc(Kinds, func(k Kind) bool { return k.Resource == "ingresses" })]
	if _, _, err := list(t.Context(), ingresses, "", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{
		// Metadata only, never a Secret's data.
		metadataAccept + " /api/v1/namespaces/payments/secrets?continue=c1&limit=500",
		metadataAccept + " /apis/networking.k8s.io/v1/ingresses?limit=500",
	}
	if !slices.Equal(seen, want) {
		t.Fatalf("requests =\n%v\nwant\n%v", seen, want)
	}
}
