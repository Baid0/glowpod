package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// MaxReplicas guards against a typo scaling something to a huge count.
const MaxReplicas = 10000

// restartedAt is the annotation `kubectl rollout restart` sets.
const restartedAt = "kubectl.kubernetes.io/restartedAt"

// InputError is a problem with what the person sent, not with the cluster.
type InputError struct {
	Code    string
	Message string
}

func (e *InputError) Error() string { return e.Message }

func inputErr(code, msg string) error { return &InputError{Code: code, Message: msg} }

func resourceFor(client dynamic.Interface, t Type, namespace string) dynamic.ResourceInterface {
	ri := client.Resource(t.GVR())
	if t.Namespaced {
		return ri.Namespace(namespace)
	}
	return ri
}

// CanRestart reports whether t rolls out new pods when its template changes.
func CanRestart(t Type) bool {
	if t.Group != "apps" {
		return false
	}
	switch t.Resource {
	case "deployments", "statefulsets", "daemonsets":
		return true
	}
	return false
}

// Scale sets the replica count through the scale subresource, so it works
// for Deployments, StatefulSets, ReplicaSets and CRDs that support it.
func Scale(ctx context.Context, client dynamic.Interface, t Type, namespace, name string, replicas int64) error {
	if !t.Has("scale") {
		return inputErr("not_scalable", "This kind of resource can't be scaled.")
	}
	if replicas < 0 || replicas > MaxReplicas {
		return inputErr("bad_replicas", fmt.Sprintf("Replicas must be between 0 and %d.", MaxReplicas))
	}
	patch := fmt.Appendf(nil, `{"spec":{"replicas":%d}}`, replicas)
	if _, err := resourceFor(client, t, namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}, "scale"); err != nil {
		return fmt.Errorf("scale %s %s: %w", t.Resource, name, err)
	}
	return nil
}

// Restart replaces every pod with a rolling update, like `kubectl rollout restart`.
func Restart(ctx context.Context, client dynamic.Interface, t Type, namespace, name string, now time.Time) error {
	if !CanRestart(t) {
		return inputErr("not_restartable", "Only Deployments, StatefulSets and DaemonSets can be restarted.")
	}
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{restartedAt: now.UTC().Format(time.RFC3339)}},
	}}})
	if err != nil {
		return fmt.Errorf("encode restart patch: %w", err)
	}
	if _, err := resourceFor(client, t, namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("restart %s %s: %w", t.Resource, name, err)
	}
	return nil
}

// Propagation values the UI may send for Delete.
var propagation = map[string]metav1.DeletionPropagation{
	"":           metav1.DeletePropagationBackground,
	"background": metav1.DeletePropagationBackground,
	"foreground": metav1.DeletePropagationForeground,
	"orphan":     metav1.DeletePropagationOrphan,
}

// Delete removes one object. Dependents (a Deployment's pods) go too unless
// mode is "orphan".
func Delete(ctx context.Context, client dynamic.Interface, t Type, namespace, name, mode string) error {
	if !t.Can("delete") {
		return inputErr("not_deletable", "This kind of resource can't be deleted.")
	}
	p, ok := propagation[mode]
	if !ok {
		return inputErr("bad_propagation", "propagation must be background, foreground or orphan.")
	}
	if err := resourceFor(client, t, namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &p}); err != nil {
		return fmt.Errorf("delete %s %s: %w", t.Resource, name, err)
	}
	return nil
}

// UpdateResult is what PUT .../resources/{g}/{v}/{r}/{name} returns. A dry
// run fills Before and After so the UI can show the diff; a real update
// returns the saved object.
type UpdateResult struct {
	DryRun bool   `json:"dryRun"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// Object is the saved object, after a real update.
	Object *Object `json:"object,omitempty"`
}

var docSeparator = regexp.MustCompile(`(?m)^---\s*$`)

// Update replaces one object with manifest (YAML or JSON), the way
// `kubectl edit` does. metadata.resourceVersion must be the one the person
// started from, so a change someone else made in between is a conflict
// instead of being overwritten. With dryRun the API server validates and
// runs admission, but nothing is saved.
func Update(ctx context.Context, client dynamic.Interface, t Type, namespace, name, manifest string, dryRun bool) (*UpdateResult, error) {
	if !t.Can("update") {
		return nil, inputErr("not_editable", "This kind of resource can't be edited.")
	}
	obj, err := parseManifest(manifest)
	if err != nil {
		return nil, err
	}
	if err := checkIdentity(obj, t, namespace, name); err != nil {
		return nil, err
	}
	res := resourceFor(client, t, namespace)

	current, err := res.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s %s: %w", t.Resource, name, err)
	}
	if IsSecret(t) && looksRedacted(obj, current) {
		return nil, inputErr("secret_hidden", "Reveal this Secret's values before editing it, or the saved values would be blank.")
	}

	opts := metav1.UpdateOptions{FieldManager: "glowpod"}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	saved, err := res.Update(ctx, obj, opts)
	if err != nil {
		return nil, fmt.Errorf("update %s %s: %w", t.Resource, name, err)
	}
	unstructured.RemoveNestedField(saved.Object, "metadata", "managedFields")

	if !dryRun {
		y, err := toYAML(saved.Object)
		if err != nil {
			return nil, err
		}
		return &UpdateResult{Object: &Object{Object: saved.Object, YAML: y}}, nil
	}
	unstructured.RemoveNestedField(current.Object, "metadata", "managedFields")
	before, err := toYAML(current.Object)
	if err != nil {
		return nil, err
	}
	after, err := toYAML(saved.Object)
	if err != nil {
		return nil, err
	}
	return &UpdateResult{DryRun: true, Before: before, After: after}, nil
}

func parseManifest(manifest string) (*unstructured.Unstructured, error) {
	docs := 0
	for _, d := range docSeparator.Split(manifest, -1) {
		if strings.TrimSpace(d) != "" {
			docs++
		}
	}
	if docs != 1 {
		return nil, inputErr("bad_manifest", "Send exactly one object.")
	}
	j, err := yaml.YAMLToJSON([]byte(manifest))
	if err != nil {
		return nil, inputErr("bad_manifest", "This isn't valid YAML: "+err.Error())
	}
	var m map[string]any
	if err := json.Unmarshal(j, &m); err != nil || m == nil {
		return nil, inputErr("bad_manifest", "This YAML isn't an object.")
	}
	return &unstructured.Unstructured{Object: m}, nil
}

// checkIdentity keeps an edit on the object it was opened for: renaming or
// moving it would create something else instead.
func checkIdentity(obj *unstructured.Unstructured, t Type, namespace, name string) error {
	apiVersion := t.Version
	if t.Group != "" {
		apiVersion = t.Group + "/" + t.Version
	}
	switch {
	case obj.GetAPIVersion() != apiVersion:
		return inputErr("identity_changed", "apiVersion must stay "+apiVersion+".")
	case obj.GetKind() != t.Kind:
		return inputErr("identity_changed", "kind must stay "+t.Kind+".")
	case obj.GetName() != name:
		return inputErr("identity_changed", "metadata.name must stay "+name+". To rename, create a copy instead.")
	case t.Namespaced && obj.GetNamespace() != "" && obj.GetNamespace() != namespace:
		return inputErr("identity_changed", "metadata.namespace must stay "+namespace+".")
	case obj.GetResourceVersion() == "":
		return inputErr("no_resource_version", "Keep metadata.resourceVersion so Glowpod can tell if someone else changed this.")
	}
	if t.Namespaced {
		obj.SetNamespace(namespace)
	}
	return nil
}

// looksRedacted spots a Secret sent back exactly as the hidden view shows
// it: every value blank while the cluster has real ones.
func looksRedacted(sent, current *unstructured.Unstructured) bool {
	if ann := sent.GetAnnotations(); ann[lastApplied] == hiddenPlaceholder {
		return true
	}
	data, _, _ := unstructured.NestedMap(sent.Object, "data")
	if len(data) == 0 {
		return false
	}
	for _, v := range data {
		if v != "" {
			return false
		}
	}
	have, _, _ := unstructured.NestedMap(current.Object, "data")
	for _, v := range have {
		if v != "" {
			return true
		}
	}
	return false
}

func toYAML(obj map[string]any) (string, error) {
	j, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("encode object: %w", err)
	}
	y, err := yaml.JSONToYAML(j)
	if err != nil {
		return "", fmt.Errorf("convert object to YAML: %w", err)
	}
	return string(y), nil
}
