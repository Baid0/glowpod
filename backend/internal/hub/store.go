package hub

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/subjects"
)

// ClusterGVR is the Hub's record of an imported cluster. It's
// cluster-scoped: kubectl get clusters.hub.glowpod.dev.
var ClusterGVR = schema.GroupVersionResource{Group: "hub.glowpod.dev", Version: "v1alpha1", Resource: "clusters"}

const (
	managedBy = "glowpod-hub"
	// settingsName is the ConfigMap with the Hub's settings.
	settingsName = "glowpod-hub"
	// BootstrapSecret holds the first-start password until setup is done.
	BootstrapSecret     = "glowpod-bootstrap" //nolint:gosec // a Secret's name, not a credential
	userSecretPrefix    = "glowpod-user-"     //nolint:gosec // a Secret's name, not a credential
	clusterSecretPrefix = "glowpod-cluster-"  //nolint:gosec // a Secret's name, not a credential
)

// Keys in the Hub's Secrets and ConfigMap.
const (
	keyServerURL      = "server-url"
	keySetupDone      = "setup-done"
	keyPassword       = "password"
	keyPasswordHash   = "password-hash"
	keyRole           = "role"
	keyTokenHash      = "import-token-hash"
	keyTokenExpires   = "import-expires"
	keyCredentialHash = "credential-hash" //nolint:gosec // a key's name, not a credential
	keyOwner          = "owner"
)

// Record is what the Hub keeps about an imported cluster. Its secrets
// (token and credential hashes) are in a Secret, not here.
type Record struct {
	ID                string
	Name              string
	ImportedBy        string
	Created           time.Time
	KubernetesVersion string
	Subjects          subjects.Set
	LastSeen          *time.Time
}

// store keeps the Hub's state as Kubernetes objects: Cluster records, and
// Secrets and a ConfigMap in the Hub's namespace. There's no database.
type store struct {
	kube      kubernetes.Interface
	dyn       dynamic.Interface
	namespace string
}

func (s *store) labels(kind string) map[string]string {
	return map[string]string{"app.kubernetes.io/managed-by": managedBy, "hub.glowpod.dev/kind": kind}
}

// Settings.

type settings struct {
	ServerURL string
	SetupDone bool
}

func (s *store) settings(ctx context.Context) (settings, error) {
	cm, err := s.kube.CoreV1().ConfigMaps(s.namespace).Get(ctx, settingsName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return settings{}, nil
	}
	if err != nil {
		return settings{}, fmt.Errorf("read the Hub's settings: %w", err)
	}
	return settings{ServerURL: cm.Data[keyServerURL], SetupDone: cm.Data[keySetupDone] == "true"}, nil
}

func (s *store) saveSettings(ctx context.Context, v settings) error {
	data := map[string]string{keyServerURL: v.ServerURL, keySetupDone: fmt.Sprint(v.SetupDone)}
	cm, err := s.kube.CoreV1().ConfigMaps(s.namespace).Get(ctx, settingsName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = s.kube.CoreV1().ConfigMaps(s.namespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: settingsName, Labels: s.labels("settings")},
			Data:       data,
		}, metav1.CreateOptions{})
	} else if err == nil {
		cm.Data = data
		_, err = s.kube.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("save the Hub's settings: %w", err)
	}
	return nil
}

// Secrets.

func (s *store) secret(ctx context.Context, name string) (map[string][]byte, error) {
	sec, err := s.kube.CoreV1().Secrets(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Secret %s: %w", name, err)
	}
	if sec.Data == nil {
		sec.Data = map[string][]byte{}
	}
	return sec.Data, nil
}

// putSecret creates or replaces a Secret's data.
func (s *store) putSecret(ctx context.Context, name, kind string, data map[string][]byte) error {
	secrets := s.kube.CoreV1().Secrets(s.namespace)
	sec, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: s.labels(kind)},
			Type:       corev1.SecretTypeOpaque,
			Data:       data,
		}, metav1.CreateOptions{})
	} else if err == nil {
		sec.Data = data
		_, err = secrets.Update(ctx, sec, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("save Secret %s: %w", name, err)
	}
	return nil
}

func (s *store) deleteSecret(ctx context.Context, name string) error {
	err := s.kube.CoreV1().Secrets(s.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Secret %s: %w", name, err)
	}
	return nil
}

// Cluster records.

func (s *store) clusters(ctx context.Context) ([]Record, error) {
	l, err := s.dyn.Resource(ClusterGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list clusters.hub.glowpod.dev (is the CRD installed?): %w", err)
	}
	out := make([]Record, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, recordFrom(&l.Items[i]))
	}
	return out, nil
}

func (s *store) createCluster(ctx context.Context, r Record) error {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": ClusterGVR.GroupVersion().String(),
		"kind":       "Cluster",
		"metadata": map[string]any{
			"name":   r.ID,
			"labels": toAny(s.labels("cluster")),
		},
		"spec": map[string]any{"displayName": r.Name, "importedBy": r.ImportedBy},
	}}
	if _, err := s.dyn.Resource(ClusterGVR).Create(ctx, u, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("save cluster %s: %w", r.ID, err)
	}
	return nil
}

// saveStatus records what the agent last said about its cluster.
func (s *store) saveStatus(ctx context.Context, r Record) error {
	res := s.dyn.Resource(ClusterGVR)
	u, err := res.Get(ctx, r.ID, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read cluster %s: %w", r.ID, err)
	}
	status := map[string]any{
		"kubernetesVersion": r.KubernetesVersion,
		"subjects": map[string]any{
			"users":  toAnySlice(r.Subjects.Users),
			"groups": toAnySlice(r.Subjects.Groups),
		},
	}
	if r.LastSeen != nil {
		status["lastSeen"] = r.LastSeen.UTC().Format(time.RFC3339)
	}
	u.Object["status"] = status
	if _, err := res.Update(ctx, u, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("save cluster %s: %w", r.ID, err)
	}
	return nil
}

func (s *store) deleteCluster(ctx context.Context, id string) error {
	err := s.dyn.Resource(ClusterGVR).Delete(ctx, id, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete cluster %s: %w", id, err)
	}
	return s.deleteSecret(ctx, clusterSecretPrefix+id)
}

func recordFrom(u *unstructured.Unstructured) Record {
	r := Record{ID: u.GetName(), Created: u.GetCreationTimestamp().Time}
	r.Name, _, _ = unstructured.NestedString(u.Object, "spec", "displayName")
	if r.Name == "" {
		r.Name = r.ID
	}
	r.ImportedBy, _, _ = unstructured.NestedString(u.Object, "spec", "importedBy")
	r.KubernetesVersion, _, _ = unstructured.NestedString(u.Object, "status", "kubernetesVersion")
	r.Subjects.Users, _, _ = unstructured.NestedStringSlice(u.Object, "status", "subjects", "users")
	r.Subjects.Groups, _, _ = unstructured.NestedStringSlice(u.Object, "status", "subjects", "groups")
	if seen, ok, _ := unstructured.NestedString(u.Object, "status", "lastSeen"); ok {
		if t, err := time.Parse(time.RFC3339, seen); err == nil {
			r.LastSeen = &t
		}
	}
	return r
}

func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toAnySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
