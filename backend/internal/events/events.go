// Package events finds the newest Kubernetes events, for "Recent events" and
// an object's own events.
package events

import (
	"cmp"
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/paging"
	"glowpod/internal/summary"
)

const (
	// maxPages caps how many pages are read per namespace to find the newest.
	maxPages = 10
	pageSize = 500
)

// About narrows events to one object's: its kind and name.
func About(kind, name string) string {
	return fields.Set{"involvedObject.kind": kind, "involvedObject.name": name}.AsSelector().String()
}

// Newest returns up to limit events in namespaces ("" = all), newest first.
// fieldSelector narrows them (see About). The API can't sort, so it reads up
// to maxPages pages per namespace and sorts them here. Namespaces the user
// may not read are skipped as long as another one works.
func Newest(ctx context.Context, kube kubernetes.Interface, namespaces []string, fieldSelector string, limit int) ([]summary.Event, error) {
	all, _, err := paging.Across(ctx, namespaces,
		func(ctx context.Context, namespace string, _ metav1.ListOptions) ([]corev1.Event, string, error) {
			return pages(ctx, kube, namespace, metav1.ListOptions{Limit: pageSize, FieldSelector: fieldSelector})
		})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b corev1.Event) int {
		return cmp.Compare(summary.EventTime(&b).UnixNano(), summary.EventTime(&a).UnixNano())
	})
	out := make([]summary.Event, 0, min(limit, len(all)))
	for i := range all[:min(limit, len(all))] {
		out = append(out, summary.EventOf(&all[i]))
	}
	return out, nil
}

// pages reads up to maxPages pages of one namespace's events. (paging.Across
// calls it once per namespace; it pages on its own so the cap applies per
// namespace.)
func pages(ctx context.Context, kube kubernetes.Interface, namespace string, opts metav1.ListOptions) ([]corev1.Event, string, error) {
	var all []corev1.Event
	for range maxPages {
		list, err := kube.CoreV1().Events(namespace).List(ctx, opts)
		if err != nil {
			return nil, "", err
		}
		all = append(all, list.Items...)
		if list.Continue == "" {
			break
		}
		opts.Continue = list.Continue
	}
	return all, "", nil
}
