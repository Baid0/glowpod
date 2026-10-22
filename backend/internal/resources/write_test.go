package resources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var (
	writableDeploy = Type{
		Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true,
		Verbs: []string{"get", "list", "update", "patch", "delete"}, Subresources: []string{"scale", "status"},
	}
	writableSecret = Type{
		Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true,
		Verbs: []string{"get", "list", "update", "delete"},
	}
	writableConfigMap = Type{
		Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true,
		Verbs: []string{"get", "list", "update", "delete"},
	}
)

func writeClient(t *testing.T) (*dynamicfake.FakeDynamicClient, *[]k8stesting.Action) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		writableDeploy.GVR(): "DeploymentList", writableSecret.GVR(): "SecretList", writableConfigMap.GVR(): "ConfigMapList",
	},
		object("apps/v1", "Deployment", "shop", "web", map[string]any{"spec": map[string]any{"replicas": int64(2)}}),
		object("v1", "Secret", "shop", "db", map[string]any{"data": map[string]any{"password": "aHVudGVyMg=="}}),
		object("v1", "ConfigMap", "shop", "settings", map[string]any{"data": map[string]any{"mode": "fast"}}),
	)
	var writes []k8stesting.Action
	client.PrependReactor("*", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			writes = append(writes, a)
		}
		return false, nil, nil
	})
	return client, &writes
}

func TestScale(t *testing.T) {
	tests := []struct {
		name     string
		t        Type
		replicas int64
		wantCode string
	}{
		{name: "deployment", t: writableDeploy, replicas: 3},
		{name: "to zero", t: writableDeploy, replicas: 0},
		{name: "negative", t: writableDeploy, replicas: -1, wantCode: "bad_replicas"},
		{name: "too many", t: writableDeploy, replicas: MaxReplicas + 1, wantCode: "bad_replicas"},
		{name: "no scale subresource", t: writableConfigMap, replicas: 1, wantCode: "not_scalable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, writes := writeClient(t)
			err := Scale(context.Background(), client, tt.t, "shop", "web", tt.replicas)
			if tt.wantCode != "" {
				assertInputError(t, err, tt.wantCode)
				if len(*writes) != 0 {
					t.Fatal("nothing should be sent")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			p := (*writes)[0].(k8stesting.PatchAction)
			if p.GetSubresource() != "scale" || !strings.Contains(string(p.GetPatch()), `"replicas":`) {
				t.Fatalf("patch = %s on %q", p.GetPatch(), p.GetSubresource())
			}
		})
	}
}

func TestRestart(t *testing.T) {
	client, writes := writeClient(t)
	now := time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)
	if err := Restart(context.Background(), client, writableDeploy, "shop", "web", now); err != nil {
		t.Fatal(err)
	}
	p := (*writes)[0].(k8stesting.PatchAction)
	if !strings.Contains(string(p.GetPatch()), `"kubectl.kubernetes.io/restartedAt":"2026-10-22T09:30:00Z"`) {
		t.Fatalf("patch = %s", p.GetPatch())
	}
	assertInputError(t, Restart(context.Background(), client, writableConfigMap, "shop", "settings", now), "not_restartable")
}

func TestDelete(t *testing.T) {
	tests := []struct {
		mode     string
		want     metav1.DeletionPropagation
		wantCode string
	}{
		{mode: "", want: metav1.DeletePropagationBackground},
		{mode: "foreground", want: metav1.DeletePropagationForeground},
		{mode: "orphan", want: metav1.DeletePropagationOrphan},
		{mode: "sideways", wantCode: "bad_propagation"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			client, writes := writeClient(t)
			err := Delete(context.Background(), client, writableDeploy, "shop", "web", tt.mode)
			if tt.wantCode != "" {
				assertInputError(t, err, tt.wantCode)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			d := (*writes)[0].(k8stesting.DeleteActionImpl)
			if *d.DeleteOptions.PropagationPolicy != tt.want {
				t.Fatalf("propagation = %v", *d.DeleteOptions.PropagationPolicy)
			}
		})
	}
	client, _ := writeClient(t)
	if err := Delete(context.Background(), client, writableDeploy, "shop", "missing", ""); err == nil {
		t.Fatal("want not found")
	}
}

const editedConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  namespace: shop
  resourceVersion: "7"
data:
  mode: slow
`

func TestUpdate(t *testing.T) {
	tests := []struct {
		name     string
		t        Type
		obj      string
		manifest string
		dryRun   bool
		wantCode string
		check    func(t *testing.T, r *UpdateResult, writes []k8stesting.Action)
	}{
		{
			name: "dry run shows before and after", t: writableConfigMap, obj: "settings", manifest: editedConfigMap, dryRun: true,
			check: func(t *testing.T, r *UpdateResult, writes []k8stesting.Action) {
				u := writes[0].(k8stesting.UpdateActionImpl)
				if len(u.UpdateOptions.DryRun) != 1 || u.UpdateOptions.DryRun[0] != metav1.DryRunAll {
					t.Fatalf("dryRun = %v", u.UpdateOptions.DryRun)
				}
				if !r.DryRun || !strings.Contains(r.Before, "mode: fast") || !strings.Contains(r.After, "mode: slow") || r.Object != nil {
					t.Fatalf("result = %+v", r)
				}
			},
		},
		{
			name: "apply returns the saved object", t: writableConfigMap, obj: "settings", manifest: editedConfigMap,
			check: func(t *testing.T, r *UpdateResult, writes []k8stesting.Action) {
				u := writes[0].(k8stesting.UpdateActionImpl)
				if len(u.UpdateOptions.DryRun) != 0 || u.UpdateOptions.FieldManager != "glowpod" {
					t.Fatalf("options = %+v", u.UpdateOptions)
				}
				if r.DryRun || r.Object == nil || !strings.Contains(r.Object.YAML, "mode: slow") {
					t.Fatalf("result = %+v", r)
				}
			},
		},
		{
			name: "namespace filled in", t: writableConfigMap, obj: "settings",
			manifest: strings.Replace(editedConfigMap, "  namespace: shop\n", "", 1), dryRun: true,
		},
		{name: "renamed", t: writableConfigMap, obj: "settings", manifest: strings.Replace(editedConfigMap, "name: settings", "name: other", 1), wantCode: "identity_changed"},
		{name: "moved", t: writableConfigMap, obj: "settings", manifest: strings.Replace(editedConfigMap, "namespace: shop", "namespace: prod", 1), wantCode: "identity_changed"},
		{name: "other kind", t: writableConfigMap, obj: "settings", manifest: strings.Replace(editedConfigMap, "kind: ConfigMap", "kind: Secret", 1), wantCode: "identity_changed"},
		{name: "other version", t: writableDeploy, obj: "web", manifest: "apiVersion: apps/v1beta1\nkind: Deployment\nmetadata: {name: web, resourceVersion: '1'}\n", wantCode: "identity_changed"},
		{name: "no resourceVersion", t: writableConfigMap, obj: "settings", manifest: strings.Replace(editedConfigMap, "  resourceVersion: \"7\"\n", "", 1), wantCode: "no_resource_version"},
		{name: "bad yaml", t: writableConfigMap, obj: "settings", manifest: "data: [unclosed", wantCode: "bad_manifest"},
		{name: "not an object", t: writableConfigMap, obj: "settings", manifest: "- a\n- b\n", wantCode: "bad_manifest"},
		{name: "two documents", t: writableConfigMap, obj: "settings", manifest: editedConfigMap + "---\n" + editedConfigMap, wantCode: "bad_manifest"},
		{name: "empty", t: writableConfigMap, obj: "settings", manifest: "  \n", wantCode: "bad_manifest"},
		{name: "not editable", t: podsType, obj: "api-1", manifest: "apiVersion: v1\nkind: Pod\n", wantCode: "not_editable"},
		{
			name: "hidden secret refused", t: writableSecret, obj: "db",
			manifest: "apiVersion: v1\nkind: Secret\nmetadata: {name: db, resourceVersion: '1'}\ndata: {password: ''}\n", wantCode: "secret_hidden",
		},
		{
			name: "hidden annotation refused", t: writableSecret, obj: "db",
			manifest: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  resourceVersion: '1'\n  annotations: {kubectl.kubernetes.io/last-applied-configuration: '" + hiddenPlaceholder + "'}\ndata: {password: 'bmV3'}\n",
			wantCode: "secret_hidden",
		},
		{
			name: "revealed secret saved", t: writableSecret, obj: "db",
			manifest: "apiVersion: v1\nkind: Secret\nmetadata: {name: db, resourceVersion: '1'}\ndata: {password: 'bmV3'}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, writes := writeClient(t)
			r, err := Update(context.Background(), client, tt.t, "shop", tt.obj, tt.manifest, tt.dryRun)
			if tt.wantCode != "" {
				assertInputError(t, err, tt.wantCode)
				if len(*writes) != 0 {
					t.Fatal("nothing should be sent")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, r, *writes)
			}
		})
	}
}

func assertInputError(t *testing.T, err error, code string) {
	t.Helper()
	var ie *InputError
	if !errors.As(err, &ie) || ie.Code != code {
		t.Fatalf("err = %v, want input error %s", err, code)
	}
}
