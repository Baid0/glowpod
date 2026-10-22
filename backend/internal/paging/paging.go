// Package paging reads whole Kubernetes lists a page at a time.
package paging

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// Across reads a whole list in each namespace in parallel and joins them.
// "" in namespaces means all namespaces. A namespace the user may not read
// is skipped, and named in skipped, as long as another one could be read;
// any other failure fails the whole read.
func Across[T any](
	ctx context.Context,
	namespaces []string,
	list func(ctx context.Context, namespace string, o metav1.ListOptions) ([]T, string, error),
) (items []T, skipped []string, err error) {
	if len(namespaces) == 1 {
		ns := namespaces[0]
		items, err = All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]T, string, error) { return list(ctx, ns, o) })
		return items, nil, err
	}
	var mu sync.Mutex
	var forbidden error
	g, gctx := errgroup.WithContext(ctx)
	for _, ns := range namespaces {
		g.Go(func() error {
			got, err := All(gctx, func(ctx context.Context, o metav1.ListOptions) ([]T, string, error) { return list(ctx, ns, o) })
			mu.Lock()
			defer mu.Unlock()
			switch {
			case apierrors.IsForbidden(err):
				skipped = append(skipped, ns)
				forbidden = err
				return nil
			case err != nil:
				return err
			}
			items = append(items, got...)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err //nolint:wrapcheck // callers wrap with what they listed
	}
	if len(skipped) == len(namespaces) {
		return nil, nil, forbidden
	}
	return items, skipped, nil
}
