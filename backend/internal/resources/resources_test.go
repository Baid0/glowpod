package resources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
)

var (
	podsType    = Type{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}}
	nodesType   = Type{Group: "", Version: "v1", Resource: "nodes", Kind: "Node", Verbs: []string{"get", "list"}}
	secretsType = Type{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true, Verbs: []string{"get", "list"}}
	deployType  = Type{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"list"}}
)

func apiResource(name, kind string, namespaced bool, verbs ...string) metav1.APIResource {
	return metav1.APIResource{Name: name, Kind: kind, Namespaced: namespaced, Verbs: verbs}
}

func fakeDiscovery() *fake.Clientset {
	kube := fake.NewClientset()
	kube.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			apiResource("pods", "Pod", true, "get", "list", "watch"),
			apiResource("pods/log", "Pod", true, "get"),
			apiResource("bindings", "Binding", true, "create"),
			apiResource("nodes", "Node", false, "get", "list"),
		}},
		// apps/v1 is listed first, so it's the preferred version.
		{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{apiResource("deployments", "Deployment", true, "list")}},
		{GroupVersion: "apps/v1beta1", APIResources: []metav1.APIResource{apiResource("deployments", "Deployment", true, "list")}},
		{GroupVersion: "argoproj.io/v1alpha1", APIResources: []metav1.APIResource{apiResource("applications", "Application", true, "list", "watch")}},
	}
	return kube
}

func TestDiscover(t *testing.T) {
	types, err := Discover(fakeDiscovery().Discovery())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ty := range types {
		got = append(got, ty.URLGroup()+"/"+ty.Version+"/"+ty.Resource)
	}
	want := "core/v1/nodes,core/v1/pods,apps/v1/deployments,argoproj.io/v1alpha1/applications"
	if strings.Join(got, ",") != want {
		t.Fatalf("types = %v, want %s (subresources, non-listable and non-preferred versions dropped)", got, want)
	}
	if pods, _ := Find(types, CoreGroup, "v1", "pods"); !pods.Has("log") || pods.Has("exec") {
		t.Fatalf("pod subresources = %v", pods.Subresources)
	}
}

func TestFindAndResolve(t *testing.T) {
	kube := fakeDiscovery()
	d := memory.NewMemCacheClient(kube.Discovery())

	tests := []struct {
		group, version, resource string
		wantErr                  error
		wantKind                 string
	}{
		{"core", "v1", "pods", nil, "Pod"},
		{"apps", "v1", "deployments", nil, "Deployment"},
		{"apps", "v1beta1", "deployments", ErrUnknownType, ""},
		{"", "v1", "pods", nil, "Pod"},
		{"core", "v1", "pods/log", ErrUnknownType, ""},
		{"example.com", "v1", "widgets", ErrUnknownType, ""},
	}
	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.version+"/"+tt.resource, func(t *testing.T) {
			got, err := Resolve(d, tt.group, tt.version, tt.resource)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.wantKind)
			}
		})
	}

	// A CRD installed after the first lookup is found after one refresh.
	kube.Resources = append(kube.Resources, &metav1.APIResourceList{
		GroupVersion: "example.com/v1", APIResources: []metav1.APIResource{apiResource("widgets", "Widget", true, "list")},
	})
	if got, err := Resolve(d, "example.com", "v1", "widgets"); err != nil || got.Kind != "Widget" {
		t.Fatalf("new CRD: %+v, %v", got, err)
	}
}

func TestPath(t *testing.T) {
	tests := []struct {
		t    Type
		ns   string
		want string
	}{
		{podsType, "payments", "/api/v1/namespaces/payments/pods"},
		{podsType, "", "/api/v1/pods"},
		{nodesType, "payments", "/api/v1/nodes"},
		{deployType, "web", "/apis/apps/v1/namespaces/web/deployments"},
		{Type{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications", Namespaced: true}, "", "/apis/argoproj.io/v1alpha1/applications"},
	}
	for _, tt := range tests {
		if got := Path(tt.t, tt.ns); got != tt.want {
			t.Errorf("Path(%s, %q) = %s, want %s", tt.t.Resource, tt.ns, got, tt.want)
		}
	}
}

const podTable = `{
  "kind": "Table", "apiVersion": "meta.k8s.io/v1",
  "metadata": {"resourceVersion": "4242", "continue": "next-page", "remainingItemCount": 8},
  "columnDefinitions": [
    {"name": "Name", "type": "string", "format": "name", "priority": 0},
    {"name": "Status", "type": "string", "priority": 0},
    {"name": "Node", "type": "string", "priority": 1}
  ],
  "rows": [{
    "cells": ["api-1", "Running", "w1"],
    "object": {"kind": "PartialObjectMetadata", "apiVersion": "meta.k8s.io/v1",
      "metadata": {"name": "api-1", "namespace": "payments", "uid": "u-1", "creationTimestamp": "2026-10-22T10:00:00Z",
        "labels": {"app": "api"}, "annotations": {"note": "not returned"}}}
  }]
}`

func tableServer(t *testing.T, handler http.HandlerFunc) rest.Interface {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return kube.CoreV1().RESTClient()
}

func TestListTable(t *testing.T) {
	var got *http.Request
	client := tableServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(podTable))
	})

	table, err := ListTable(context.Background(), client, podsType, ListOptions{
		Namespace: "payments", LabelSelector: "app=api", FieldSelector: "status.phase=Running", Continue: "abc", Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.URL.Path != "/api/v1/namespaces/payments/pods" {
		t.Fatalf("path = %s", got.URL.Path)
	}
	if !strings.Contains(got.Header.Get("Accept"), "as=Table") {
		t.Fatalf("Accept = %q, want Table output", got.Header.Get("Accept"))
	}
	q := got.URL.Query()
	for k, v := range map[string]string{
		"includeObject": "Metadata", "limit": "50", "labelSelector": "app=api",
		"fieldSelector": "status.phase=Running", "continue": "abc",
	} {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), v)
		}
	}

	if len(table.Columns) != 3 || table.Columns[2].Priority != 1 {
		t.Fatalf("columns = %+v", table.Columns)
	}
	if table.Continue != "next-page" || table.ResourceVersion != "4242" || *table.RemainingItemCount != 8 {
		t.Fatalf("paging = %q %q %v", table.Continue, table.ResourceVersion, table.RemainingItemCount)
	}
	row := table.Rows[0]
	if row.Object.Name != "api-1" || row.Object.Namespace != "payments" || row.Object.UID != "u-1" ||
		row.Object.Labels["app"] != "api" || row.Object.CreationTimestamp == nil {
		t.Fatalf("row object = %+v", row.Object)
	}
	if len(row.Cells) != 3 || row.Cells[1] != "Running" {
		t.Fatalf("cells = %v", row.Cells)
	}
}

func TestListTableErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`},
		{"not a table", http.StatusOK, `{"kind":"PodList","apiVersion":"v1","items":[]}`},
		{"garbage", http.StatusOK, `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tableServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			if _, err := ListTable(context.Background(), client, podsType, ListOptions{}); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func object(apiVersion, kind, ns, name string, extra map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name": name, "namespace": ns,
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
			"annotations":   map[string]any{lastApplied: `{"data":{"password":"aHVudGVyMg=="}}`},
		},
	}}
	for k, v := range extra {
		u.Object[k] = v
	}
	return u
}

func TestGet(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		podsType.GVR(): "PodList", secretsType.GVR(): "SecretList",
	},
		object("v1", "Pod", "payments", "api-1", map[string]any{"spec": map[string]any{"nodeName": "w1"}}),
		object("v1", "Secret", "payments", "db", map[string]any{
			"data":       map[string]any{"password": "aHVudGVyMg==", "user": "YWRtaW4="},
			"stringData": map[string]any{"token": "plain"},
		}),
	)

	tests := []struct {
		name         string
		t            Type
		obj          string
		reveal       bool
		wantRedacted bool
		check        func(t *testing.T, o *Object)
	}{
		{
			name: "pod, managed fields dropped, YAML too", t: podsType, obj: "api-1",
			check: func(t *testing.T, o *Object) {
				if _, ok, _ := unstructured.NestedFieldNoCopy(o.Object, "metadata", "managedFields"); ok {
					t.Fatal("managedFields should be removed")
				}
				if !strings.Contains(o.YAML, "nodeName: w1") || strings.Contains(o.YAML, "managedFields") {
					t.Fatalf("yaml = %s", o.YAML)
				}
			},
		},
		{
			name: "secret values hidden by default", t: secretsType, obj: "db", wantRedacted: true,
			check: func(t *testing.T, o *Object) {
				data, _, _ := unstructured.NestedStringMap(o.Object, "data")
				str, _, _ := unstructured.NestedStringMap(o.Object, "stringData")
				if data["password"] != "" || data["user"] != "" || str["token"] != "" || len(data) != 2 {
					t.Fatalf("data = %v, stringData = %v", data, str)
				}
				ann, _, _ := unstructured.NestedStringMap(o.Object, "metadata", "annotations")
				if strings.Contains(ann[lastApplied], "aHVudGVyMg") {
					t.Fatal("last-applied annotation leaks the secret")
				}
				if strings.Contains(o.YAML, "aHVudGVyMg") || strings.Contains(o.YAML, "plain") {
					t.Fatalf("yaml leaks values: %s", o.YAML)
				}
			},
		},
		{
			name: "secret revealed on request", t: secretsType, obj: "db", reveal: true,
			check: func(t *testing.T, o *Object) {
				data, _, _ := unstructured.NestedStringMap(o.Object, "data")
				if data["password"] != "aHVudGVyMg==" {
					t.Fatalf("data = %v", data)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := Get(context.Background(), client, tt.t, "payments", tt.obj, tt.reveal, true)
			if err != nil {
				t.Fatal(err)
			}
			if o.Redacted != tt.wantRedacted {
				t.Fatalf("redacted = %v", o.Redacted)
			}
			tt.check(t, o)
		})
	}

	if _, err := Get(context.Background(), client, podsType, "payments", "missing", false, false); err == nil {
		t.Fatal("want not found")
	}
}
