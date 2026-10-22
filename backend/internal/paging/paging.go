// Package paging reads whole Kubernetes lists a page at a time.
package paging

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PageSize is how many items each request asks for.
const PageSize = 500

// All pages through a list with limit/continue. list returns one page's
// items and its continue token.
func All[T any](ctx context.Context, list func(context.Context, metav1.ListOptions) ([]T, string, error)) ([]T, error) {
	var out []T
	opts := metav1.ListOptions{Limit: PageSize}
	for {
		items, next, err := list(ctx, opts)
		if err != nil {
			return nil, err //nolint:wrapcheck // callers wrap with what they listed
		}
		out = append(out, items...)
		if next == "" {
			return out, nil
		}
		opts.Continue = next
	}
}
