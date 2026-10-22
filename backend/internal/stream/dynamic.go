package stream

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"glowpod/internal/summary"
)

// ListSource watches any resource and says which object changed. Open lists
// use it to refresh; the rows themselves come from the Table API.
func ListSource(client dynamic.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace, labelSelector string) Source {
	var res dynamic.ResourceInterface = client.Resource(gvr)
	if namespaced && namespace != "" {
		res = client.Resource(gvr).Namespace(namespace)
	}
	return Source{
		Name: gvr.Resource,
		List: listVersion(func(ctx context.Context, o metav1.ListOptions) (string, error) {
			o.LabelSelector = labelSelector
			l, err := res.List(ctx, o)
			if err != nil {
				return "", err //nolint:wrapcheck // classified by Run
			}
			return l.GetResourceVersion(), nil
		}),
		Watch: func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
			o.LabelSelector = labelSelector
			return res.Watch(ctx, o) //nolint:wrapcheck // classified by Run
		},
		Convert: func(t watch.EventType, obj runtime.Object) (Message, bool) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return Message{}, false
			}
			return Message{Type: "changed", Op: op(t), Ref: &summary.ObjectRef{
				Kind: u.GetKind(), Namespace: u.GetNamespace(), Name: u.GetName(),
			}}, true
		},
	}
}
