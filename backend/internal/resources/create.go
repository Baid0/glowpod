package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"glowpod/internal/kerr"
)

// MaxApplyObjects caps one Apply: a paste of a whole chart's output fits,
// a runaway generated file doesn't.
const MaxApplyObjects = 50

// fieldManager is how Glowpod's changes appear in managedFields.
const fieldManager = "glowpod"

// CreateResult is what POST .../resources/{g}/{v}/{r} returns: the object as
// the API server made it (or would make it, on a dry run). Secret values are
// blanked, as everywhere else.
type CreateResult struct {
	DryRun bool    `json:"dryRun"`
	Object *Object `json:"object"`
}

// Create makes one new object of type t from manifest (YAML or JSON), like
// `kubectl create -f`. A namespaced object without metadata.namespace goes
// into namespace. With dryRun the API server validates it and runs
// admission, but nothing is saved.
func Create(ctx context.Context, client dynamic.Interface, t Type, namespace, manifest string, dryRun bool) (*CreateResult, error) {
	if !t.Can("create") {
		return nil, inputErr("not_creatable", "This kind of resource can't be created.")
	}
	obj, err := parseManifest(manifest)
	if err != nil {
		return nil, err
	}
	if err := checkNew(obj, t, namespace); err != nil {
		return nil, err
	}
	opts := metav1.CreateOptions{FieldManager: fieldManager}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	made, err := resourceFor(client, t, obj.GetNamespace()).Create(ctx, obj, opts)
	if err != nil {
		return nil, fmt.Errorf("create %s %s: %w", t.Resource, displayName(obj), err)
	}
	o, err := shown(made, IsSecret(t))
	if err != nil {
		return nil, err
	}
	return &CreateResult{DryRun: dryRun, Object: o}, nil
}

// checkNew keeps a create on the page's kind and namespace, and drops the
// fields of an existing object, so a copied manifest works as a template.
func checkNew(obj *unstructured.Unstructured, t Type, namespace string) error {
	apiVersion := t.Version
	if t.Group != "" {
		apiVersion = t.Group + "/" + t.Version
	}
	switch {
	case obj.GetAPIVersion() != apiVersion || obj.GetKind() != t.Kind:
		return inputErr("wrong_kind", fmt.Sprintf("This creates a %s: use apiVersion: %s and kind: %s, or use Apply YAML for other kinds.", t.Kind, apiVersion, t.Kind))
	case obj.GetName() == "" && obj.GetGenerateName() == "":
		return inputErr("name_required", "Give it a name: metadata.name.")
	}
	if t.Namespaced {
		switch {
		case obj.GetNamespace() == "" && namespace == "":
			return inputErr("namespace_required", "Say which namespace it goes in: metadata.namespace.")
		case obj.GetNamespace() == "":
			obj.SetNamespace(namespace)
		}
	} else {
		obj.SetNamespace("")
	}
	forgetServerFields(obj)
	return nil
}

// forgetServerFields removes what the API server sets on an existing object;
// sending them back fails or pins the object to an old version.
func forgetServerFields(obj *unstructured.Unstructured) {
	for _, f := range []string{"resourceVersion", "uid", "creationTimestamp", "generation", "managedFields", "selfLink"} {
		unstructured.RemoveNestedField(obj.Object, "metadata", f)
	}
	unstructured.RemoveNestedField(obj.Object, "status")
}

// shown is u as the UI may see it: no managedFields, Secret values blank.
func shown(u *unstructured.Unstructured, secret bool) (*Object, error) {
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	if secret {
		redactSecret(u)
	}
	y, err := toYAML(u.Object)
	if err != nil {
		return nil, err
	}
	return &Object{Object: u.Object, YAML: y}, nil
}

func displayName(obj *unstructured.Unstructured) string {
	if n := obj.GetName(); n != "" {
		return n
	}
	return obj.GetGenerateName() + "…"
}

// Apply outcomes for one object.
const (
	Created    = "created"
	Configured = "configured"
	Unchanged  = "unchanged"
	// Applied: it worked, but the person may not read the object, so whether
	// it was new isn't known.
	Applied = "applied"
	Failed  = "failed"
)

// ApplyItem is one object of an Apply, in the order it came.
type ApplyItem struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	// Resource is the URL form (group, version, resource) for linking to it.
	Group    string `json:"group,omitempty"`
	Version  string `json:"version,omitempty"`
	Resource string `json:"resource,omitempty"`
	// Result is created, configured, unchanged, applied or failed. On a dry
	// run it says what would happen.
	Result string `json:"result"`
	// Code, Message and Detail explain a failure.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// ApplyResult is what POST .../apply returns.
type ApplyResult struct {
	DryRun bool        `json:"dryRun"`
	Items  []ApplyItem `json:"items"`
}

// Failed reports whether any object failed.
func (r *ApplyResult) Failed() bool {
	for _, it := range r.Items {
		if it.Result == Failed {
			return true
		}
	}
	return false
}

// Apply creates or updates every object in a multi-document manifest with
// server-side apply, like `kubectl apply --server-side`: any kind the
// cluster serves, CRDs included, each as the person applying. Namespaced
// objects without metadata.namespace go into namespace. One object failing
// doesn't stop the others; each has its own result.
func Apply(ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, namespace, manifest string, dryRun bool) (*ApplyResult, error) {
	docs, err := splitManifest(manifest)
	if err != nil {
		return nil, err
	}
	res := &ApplyResult{DryRun: dryRun, Items: make([]ApplyItem, 0, len(docs))}
	for _, doc := range docs {
		res.Items = append(res.Items, applyOne(ctx, client, mapper, namespace, doc, dryRun))
	}
	return res, nil
}

// splitManifest returns each non-empty document of manifest.
func splitManifest(manifest string) ([]string, error) {
	var docs []string
	for _, d := range docSeparator.Split(manifest, -1) {
		if strings.TrimSpace(stripComments(d)) != "" {
			docs = append(docs, d)
		}
	}
	switch {
	case len(docs) == 0:
		return nil, inputErr("bad_manifest", "There's nothing to apply.")
	case len(docs) > MaxApplyObjects:
		return nil, inputErr("too_many_objects", fmt.Sprintf("Apply up to %d objects at a time; this has %d.", MaxApplyObjects, len(docs)))
	}
	return docs, nil
}

func stripComments(doc string) string {
	var b strings.Builder
	for line := range strings.Lines(doc) {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			b.WriteString(line)
		}
	}
	return b.String()
}

func applyOne(ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, namespace, doc string, dryRun bool) ApplyItem {
	fail := func(it ApplyItem, code, msg, detail string) ApplyItem {
		it.Result, it.Code, it.Message, it.Detail = Failed, code, msg, detail
		return it
	}
	obj, err := parseManifest(doc)
	if err != nil {
		var ie *InputError
		if errors.As(err, &ie) {
			return fail(ApplyItem{}, ie.Code, ie.Message, "")
		}
		return fail(ApplyItem{}, "bad_manifest", err.Error(), "")
	}
	it := ApplyItem{APIVersion: obj.GetAPIVersion(), Kind: obj.GetKind(), Name: obj.GetName(), Namespace: obj.GetNamespace()}
	switch {
	case it.APIVersion == "" || it.Kind == "":
		return fail(it, "bad_manifest", "Every object needs apiVersion and kind.", "")
	case strings.HasSuffix(it.Kind, "List"):
		return fail(it, "bad_manifest", "Lists aren't supported: put each object in its own document, separated by ---.", "")
	case it.Name == "":
		return fail(it, "name_required", "Give it a name: metadata.name. (generateName only works with New.)", "")
	}

	gv, err := schema.ParseGroupVersion(it.APIVersion)
	if err != nil {
		return fail(it, "bad_manifest", "apiVersion "+it.APIVersion+" isn't valid.", "")
	}
	mapping, err := mapper.RESTMapping(gv.WithKind(it.Kind).GroupKind(), gv.Version)
	if err != nil {
		return fail(it, "unknown_kind", fmt.Sprintf("This cluster doesn't have %s %s.", it.APIVersion, it.Kind), "")
	}
	it.Group, it.Version, it.Resource = urlGroup(mapping.Resource.Group), mapping.Resource.Version, mapping.Resource.Resource
	namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace
	switch {
	case namespaced && it.Namespace == "" && namespace == "":
		return fail(it, "namespace_required", "Say which namespace it goes in: metadata.namespace.", "")
	case namespaced && it.Namespace == "":
		it.Namespace = namespace
	case !namespaced:
		it.Namespace = ""
	}
	obj.SetNamespace(it.Namespace)
	forgetServerFields(obj)

	ri := client.Resource(mapping.Resource)
	var target dynamic.ResourceInterface = ri
	if namespaced {
		target = ri.Namespace(it.Namespace)
	}

	// Whether it exists decides created or configured. Someone who may apply
	// but not read still gets an answer from the apply itself.
	before := ""
	existing, err := target.Get(ctx, it.Name, metav1.GetOptions{})
	switch {
	case err == nil:
		before = existing.GetResourceVersion()
	case apierrors.IsNotFound(err):
	case apierrors.IsForbidden(err):
		before = "?"
	default:
		c := kerr.Classify(err)
		return fail(it, c.Code, c.Message, c.Detail)
	}

	body, err := json.Marshal(obj.Object)
	if err != nil {
		return fail(it, "bad_manifest", "This object can't be encoded.", "")
	}
	opts := metav1.PatchOptions{FieldManager: fieldManager}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	saved, err := target.Patch(ctx, it.Name, types.ApplyPatchType, body, opts)
	if err != nil {
		c := kerr.Classify(err)
		if c.Code == "conflict" {
			c.Message = "Another tool manages some of these fields. Change them there, or edit this object instead."
		}
		return fail(it, c.Code, c.Message, c.Detail)
	}
	switch {
	case before == "":
		it.Result = Created
	case before == "?":
		it.Result = Applied
	case !dryRun && saved.GetResourceVersion() == before:
		it.Result = Unchanged
	default:
		it.Result = Configured
	}
	return it
}

func urlGroup(g string) string {
	if g == "" {
		return CoreGroup
	}
	return g
}
