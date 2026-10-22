package resources

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// lastApplied holds the full previous manifest, Secret values included.
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration"

// hiddenPlaceholder replaces lastApplied on a hidden Secret.
const hiddenPlaceholder = "(hidden: contains Secret values)"

// Object is GET .../resources/{g}/{v}/{r}/{name}.
type Object struct {
	Object map[string]any `json:"object"`
	// YAML is set when asked for; same content as Object.
	YAML string `json:"yaml,omitempty"`
	// Redacted is true when Secret values were hidden.
	Redacted bool `json:"redacted"`
}

// IsSecret reports whether t is core/v1 Secrets, whose values stay hidden by default.
func IsSecret(t Type) bool { return t.Group == "" && t.Resource == "secrets" }

// Get reads one object. Managed fields are dropped (noise for people), and
// Secret values are hidden unless reveal is set.
func Get(ctx context.Context, client dynamic.Interface, t Type, namespace, name string, reveal, asYAML bool) (*Object, error) {
	u, err := resourceFor(client, t, namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s %s: %w", t.Resource, name, err)
	}

	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	out := &Object{}
	if IsSecret(t) && !reveal {
		redactSecret(u)
		out.Redacted = true
	}
	out.Object = u.Object

	if asYAML {
		y, err := toYAML(u.Object)
		if err != nil {
			return nil, err
		}
		out.YAML = y
	}
	return out, nil
}

// redactSecret keeps the keys (people need to see what's there) but blanks
// every value, including the copy kubectl keeps in an annotation.
func redactSecret(u *unstructured.Unstructured) {
	for _, field := range []string{"data", "stringData"} {
		m, ok, _ := unstructured.NestedMap(u.Object, field)
		if !ok {
			continue
		}
		for k := range m {
			m[k] = ""
		}
		_ = unstructured.SetNestedMap(u.Object, m, field)
	}
	if ann := u.GetAnnotations(); ann != nil {
		if _, ok := ann[lastApplied]; ok {
			ann[lastApplied] = hiddenPlaceholder
			u.SetAnnotations(ann)
		}
	}
}
