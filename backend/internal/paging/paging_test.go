package paging

import (
	"context"
	"errors"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fake has two pages per namespace; "secret" is forbidden, "broken" fails.
func fake(_ context.Context, ns string, o metav1.ListOptions) ([]string, string, error) {
	switch ns {
	case "secret":
		return nil, "", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("rbac"))
	case "broken":
		return nil, "", errors.New("connection reset")
	}
	if o.Continue == "" {
		return []string{ns + "/1"}, "next", nil
	}
	return []string{ns + "/2"}, "", nil
}

func TestAcross(t *testing.T) {
	items, skipped, err := Across(context.Background(), []string{"a", "b", "secret"}, fake)
	slices.Sort(items)
	if err != nil || !slices.Equal(items, []string{"a/1", "a/2", "b/1", "b/2"}) || !slices.Equal(skipped, []string{"secret"}) {
		t.Fatalf("items %v skipped %v err %v", items, skipped, err)
	}
	if _, _, err := Across(context.Background(), []string{"secret"}, fake); !apierrors.IsForbidden(err) {
		t.Fatalf("one forbidden namespace = %v", err)
	}
	if _, _, err := Across(context.Background(), []string{"secret", "secret2"}, func(ctx context.Context, _ string, o metav1.ListOptions) ([]string, string, error) {
		return fake(ctx, "secret", o)
	}); !apierrors.IsForbidden(err) {
		t.Fatalf("all forbidden = %v", err)
	}
	if _, _, err := Across(context.Background(), []string{"a", "broken"}, fake); err == nil || apierrors.IsForbidden(err) {
		t.Fatalf("other failures fail the read: %v", err)
	}
}
