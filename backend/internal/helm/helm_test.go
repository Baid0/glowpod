package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/scope"
)

const manifest = `---
# Source: web/templates/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: web-db
data:
  password: aHVudGVyMg==
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: web-reader
`

// release builds a Secret the way Helm 3 stores one.
func release(t *testing.T, ns, name string, rev int, status, chartVersion string) *corev1.Secret {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name": name, "namespace": ns, "version": rev,
		"info": map[string]any{
			"status": status, "description": "Upgrade complete",
			"last_deployed": "2026-10-20T10:00:00Z", "notes": "Your password is hunter2",
		},
		"chart": map[string]any{
			"metadata": map[string]any{"name": "web", "version": chartVersion, "appVersion": "2.4.1"},
			"values":   map[string]any{"password": "hunter2"},
		},
		"config":   map[string]any{"password": "hunter2"},
		"manifest": manifest,
	})
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(body)
	_ = zw.Close()
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns, Name: "sh.helm.release.v1." + name + ".v" + strconv.Itoa(rev),
			Labels: map[string]string{"owner": "helm", "name": name, "version": strconv.Itoa(rev), "status": status, "modifiedAt": "1792500000"},
		},
		Type: secretType,
		Data: map[string][]byte{"release": []byte(base64.StdEncoding.EncodeToString(gz.Bytes()))},
	}
}

func TestList(t *testing.T) {
	kube := fake.NewClientset(
		release(t, "shop", "web", 1, "superseded", "1.0.0"),
		release(t, "shop", "web", 2, "deployed", "1.1.0"),
		release(t, "ops", "backup", 1, "failed", "0.3.0"),
	)
	list, err := List(context.Background(), kube, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("releases = %+v", list)
	}
	web := list[1]
	if web.Name != "web" || web.Revision != 2 || web.Status != "deployed" || web.ChartVersion != "1.1.0" || web.AppVersion != "2.4.1" {
		t.Fatalf("web = %+v", web)
	}
	if list[0].Name != "backup" || list[0].Status != "failed" {
		t.Fatalf("backup = %+v", list[0])
	}
}

func TestGetKeepsValuesOut(t *testing.T) {
	kube := fake.NewClientset(
		release(t, "shop", "web", 1, "superseded", "1.0.0"),
		release(t, "shop", "web", 2, "deployed", "1.1.0"),
	)
	d, err := Get(context.Background(), kube, "shop", "web")
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != 2 || len(d.History) != 2 || d.History[0].Revision != 2 {
		t.Fatalf("detail = %+v", d)
	}
	want := []Object{
		{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "web-reader"},
		{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "shop", Name: "web"},
		{APIVersion: "v1", Kind: "Secret", Namespace: "shop", Name: "web-db"},
	}
	if len(d.Objects) != 3 || d.Objects[0] != want[0] || d.Objects[1] != want[1] || d.Objects[2] != want[2] {
		t.Fatalf("objects = %+v", d.Objects)
	}
	out, _ := json.Marshal(d)
	for _, leak := range []string{"hunter2", "aHVudGVyMg", "password"} {
		if strings.Contains(string(out), leak) {
			t.Fatalf("response leaks %q: %s", leak, out)
		}
	}
	if _, err := Get(context.Background(), kube, "shop", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestForbidden(t *testing.T) {
	kube := fake.NewClientset()
	kube.PrependReactor("list", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("rbac"))
	})
	if _, err := List(context.Background(), kube, scope.One("shop")); !apierrors.IsForbidden(err) {
		t.Fatalf("err = %v", err)
	}
}
