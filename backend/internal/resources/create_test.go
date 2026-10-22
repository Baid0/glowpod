package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var (
	creatableDeploy = Type{
		Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true,
		Verbs: []string{"create", "get", "list", "update", "patch", "delete"},
	}
	creatableSecret = Type{
		Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true,
		Verbs: []string{"create", "get", "list"},
	}
	creatableNamespace = Type{
		Version: "v1", Resource: "namespaces", Kind: "Namespace",
		Verbs: []string{"create", "get", "list"},
	}
)

func createClient(t *testing.T) (*dynamicfake.FakeDynamicClient, *[]k8stesting.CreateAction) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		creatableDeploy.GVR(): "DeploymentList", creatableSecret.GVR(): "SecretList", creatableNamespace.GVR(): "NamespaceList",
	})
	var creates []k8stesting.CreateAction
	client.PrependReactor("create", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		creates = append(creates, a.(k8stesting.CreateAction))
		return false, nil, nil
	})
	return client, &creates
}

func TestCreate(t *testing.T) {
	deploy := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n"
	tests := []struct {
		name      string
		t         Type
		namespace string
		manifest  string
		dryRun    bool
		wantCode  string
		wantNS    string
		check     func(t *testing.T, sent *unstructured.Unstructured, res *CreateResult)
	}{
		{name: "into the page's namespace", t: creatableDeploy, namespace: "shop", manifest: deploy, wantNS: "shop"},
		{name: "the YAML's namespace wins", t: creatableDeploy, namespace: "shop",
			manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: pay\n", wantNS: "pay"},
		{name: "dry run", t: creatableDeploy, namespace: "shop", manifest: deploy, dryRun: true, wantNS: "shop"},
		{name: "cluster-scoped drops a namespace", t: creatableNamespace, namespace: "shop",
			manifest: "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: team-a\n  namespace: shop\n", wantNS: ""},
		{name: "a copied object loses server fields", t: creatableDeploy, namespace: "shop",
			manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  uid: abc\n  resourceVersion: \"42\"\n  creationTimestamp: \"2026-01-01T00:00:00Z\"\nstatus:\n  replicas: 3\n",
			wantNS:   "shop",
			check: func(t *testing.T, sent *unstructured.Unstructured, _ *CreateResult) {
				if sent.GetResourceVersion() != "" || sent.GetUID() != "" || sent.Object["status"] != nil {
					t.Fatalf("server fields sent: %v", sent.Object)
				}
			}},
		{name: "Secret values come back hidden", t: creatableSecret, namespace: "shop",
			manifest: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\nstringData:\n  password: hunter2\n", wantNS: "shop",
			check: func(t *testing.T, sent *unstructured.Unstructured, res *CreateResult) {
				if v, _, _ := unstructured.NestedString(sent.Object, "stringData", "password"); v != "hunter2" {
					t.Fatal("the value must reach the cluster")
				}
				if strings.Contains(res.Object.YAML, "hunter2") {
					t.Fatalf("value shown: %s", res.Object.YAML)
				}
			}},
		{name: "other kind", t: creatableDeploy, namespace: "shop",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n", wantCode: "wrong_kind"},
		{name: "no name", t: creatableDeploy, namespace: "shop",
			manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {}\n", wantCode: "name_required"},
		{name: "no namespace anywhere", t: creatableDeploy, manifest: deploy, wantCode: "namespace_required"},
		{name: "two objects", t: creatableDeploy, namespace: "shop", manifest: deploy + "---\n" + deploy, wantCode: "bad_manifest"},
		{name: "not creatable", t: writableConfigMap, namespace: "shop",
			manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n", wantCode: "not_creatable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, creates := createClient(t)
			res, err := Create(context.Background(), client, tt.t, tt.namespace, tt.manifest, tt.dryRun)
			if tt.wantCode != "" {
				assertInputError(t, err, tt.wantCode)
				if len(*creates) != 0 {
					t.Fatal("nothing should be sent")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			a := (*creates)[0]
			if a.GetNamespace() != tt.wantNS {
				t.Fatalf("created in %q, want %q", a.GetNamespace(), tt.wantNS)
			}
			opts := a.(k8stesting.CreateActionImpl).CreateOptions
			if opts.FieldManager != "glowpod" || (len(opts.DryRun) == 1) != tt.dryRun || res.DryRun != tt.dryRun {
				t.Fatalf("options = %+v, result dryRun %v", opts, res.DryRun)
			}
			if tt.check != nil {
				tt.check(t, a.GetObject().(*unstructured.Unstructured), res)
			}
		})
	}
}

// applyMapper knows Deployments and ConfigMaps (namespaced) and Namespaces.
func applyMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	return m
}

// applyClient simulates server-side apply: an apply creates the object or,
// when its content changed, bumps resourceVersion. forbidden names objects
// the person may not read; refuse names applies the cluster rejects.
func applyClient(t *testing.T, forbidden, refuse map[string]bool, existing ...runtime.Object) (*dynamicfake.FakeDynamicClient, *[]k8stesting.PatchActionImpl) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		creatableDeploy.GVR(): "DeploymentList", writableConfigMap.GVR(): "ConfigMapList", creatableNamespace.GVR(): "NamespaceList",
	}, existing...)
	var applies []k8stesting.PatchActionImpl
	client.PrependReactor("get", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if name := a.(k8stesting.GetAction).GetName(); forbidden[name] {
			return true, nil, apierrors.NewForbidden(a.GetResource().GroupResource(), name, fmt.Errorf("no"))
		}
		return false, nil, nil
	})
	client.PrependReactor("patch", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.PatchActionImpl)
		applies = append(applies, p)
		if p.GetPatchType() != types.ApplyPatchType {
			return true, nil, fmt.Errorf("want server-side apply, got %s", p.GetPatchType())
		}
		if refuse[p.GetName()] {
			return true, nil, apierrors.NewConflict(a.GetResource().GroupResource(), p.GetName(), fmt.Errorf("field manager kubectl owns .spec.replicas"))
		}
		var obj unstructured.Unstructured
		if err := json.Unmarshal(p.GetPatch(), &obj.Object); err != nil {
			return true, nil, err
		}
		cur, err := client.Tracker().Get(a.GetResource(), a.GetNamespace(), p.GetName())
		switch {
		case apierrors.IsNotFound(err):
			obj.SetResourceVersion("1")
		case err != nil:
			return true, nil, err
		default:
			old := cur.(*unstructured.Unstructured)
			obj.SetResourceVersion(old.GetResourceVersion())
			oldSpec, _ := json.Marshal(old.Object["data"])
			newSpec, _ := json.Marshal(obj.Object["data"])
			if string(oldSpec) != string(newSpec) {
				obj.SetResourceVersion(old.GetResourceVersion() + "1")
			}
		}
		return true, &obj, nil
	})
	return client, &applies
}

func TestApply(t *testing.T) {
	existing := object("v1", "ConfigMap", "shop", "settings", map[string]any{"data": map[string]any{"mode": "fast"}})
	existing.SetResourceVersion("7")
	manifest := `# Glowpod's sample app
apiVersion: v1
kind: Namespace
metadata:
  name: team-a
  namespace: ignored
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  mode: fast
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  namespace: shop
data:
  mode: slow
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: hidden
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: contested
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: w
---
apiVersion: v1
kind: ConfigMap
metadata:
  generateName: cm-
---
kind: ConfigMap
metadata:
  name: x
---
apiVersion: v1
kind: List
items: []
---
# only a comment
`
	client, applies := applyClient(t, map[string]bool{"hidden": true}, map[string]bool{"contested": true}, existing)
	res, err := Apply(context.Background(), client, applyMapper(), "shop", manifest, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ kind, ns, name, result, code string }{
		{"Namespace", "", "team-a", Created, ""},
		{"Deployment", "shop", "web", Created, ""},
		{"ConfigMap", "shop", "settings", Unchanged, ""},
		{"ConfigMap", "shop", "settings", Configured, ""},
		{"ConfigMap", "shop", "hidden", Applied, ""},
		{"ConfigMap", "shop", "contested", Failed, "conflict"},
		{"Widget", "", "w", Failed, "unknown_kind"},
		{"ConfigMap", "", "", Failed, "name_required"},
		{"ConfigMap", "", "x", Failed, "bad_manifest"},
		{"List", "", "", Failed, "bad_manifest"},
	}
	if len(res.Items) != len(want) {
		t.Fatalf("items = %+v", res.Items)
	}
	for i, w := range want {
		it := res.Items[i]
		if it.Kind != w.kind || it.Namespace != w.ns || it.Name != w.name || it.Result != w.result || it.Code != w.code {
			t.Errorf("item %d = %+v, want %+v", i, it, w)
		}
	}
	if res.Items[1].Group != "apps" || res.Items[2].Group != "core" || res.Items[2].Resource != "configmaps" {
		t.Fatalf("links = %+v, %+v", res.Items[1], res.Items[2])
	}
	if !strings.Contains(res.Items[5].Message, "set another way") {
		t.Fatalf("conflict message = %q", res.Items[5].Message)
	}
	if !res.Failed() {
		t.Fatal("Failed() should see the failures")
	}
	for _, p := range *applies {
		if p.PatchOptions.FieldManager != "glowpod" || len(p.PatchOptions.DryRun) != 0 || *p.PatchOptions.Force {
			t.Fatalf("options = %+v", p.PatchOptions)
		}
	}
}

func TestApplyDryRun(t *testing.T) {
	client, applies := applyClient(t, nil, nil)
	res, err := Apply(context.Background(), client, applyMapper(), "shop",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Items[0].Result != Created {
		t.Fatalf("result = %+v", res)
	}
	if got := (*applies)[0].PatchOptions.DryRun; len(got) != 1 || got[0] != metav1.DryRunAll {
		t.Fatalf("dryRun = %v", got)
	}
}

func TestApplyLimits(t *testing.T) {
	client, _ := applyClient(t, nil, nil)
	for name, manifest := range map[string]string{
		"empty":    "  \n---\n# nothing\n",
		"too many": strings.Repeat("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n", MaxApplyObjects+1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Apply(context.Background(), client, applyMapper(), "shop", manifest, false, false)
			var ie *InputError
			if err == nil || !errors.As(err, &ie) {
				t.Fatalf("err = %v, want an input error", err)
			}
		})
	}
}

func TestApplyForce(t *testing.T) {
	// The fake refuses "contested" only without force, like the API server.
	client, applies := applyClient(t, nil, nil)
	client.PrependReactor("patch", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.PatchActionImpl)
		if p.PatchOptions.Force == nil || !*p.PatchOptions.Force {
			return true, nil, apierrors.NewConflict(a.GetResource().GroupResource(), p.GetName(), fmt.Errorf("conflict with kubectl: .data.mode"))
		}
		return false, nil, nil
	})
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: contested\n"
	without, err := Apply(context.Background(), client, applyMapper(), "shop", manifest, true, false)
	if err != nil || without.Items[0].Code != "conflict" {
		t.Fatalf("without force = %+v, %v", without, err)
	}
	with, err := Apply(context.Background(), client, applyMapper(), "shop", manifest, true, true)
	if err != nil || with.Items[0].Result == Failed {
		t.Fatalf("with force = %+v, %v", with, err)
	}
	if last := (*applies)[len(*applies)-1]; !*last.PatchOptions.Force {
		t.Fatal("force wasn't sent")
	}
}
