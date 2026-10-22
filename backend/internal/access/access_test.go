package access

import (
	"context"
	"errors"
	"slices"
	"testing"

	authzv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestReview(t *testing.T) {
	// The fake API server allows deleting pods in "team" and patching
	// deployments/scale anywhere; "boom" makes the review itself fail.
	tests := []struct {
		name    string
		checks  []Check
		want    []bool
		wantErr bool
	}{
		{
			name: "allowed and denied line up with the checks",
			checks: []Check{
				{Verb: "delete", Group: "core", Resource: "pods", Namespace: "team"},
				{Verb: "delete", Group: "core", Resource: "pods", Namespace: "other"},
				{Verb: "patch", Group: "apps", Resource: "deployments", Subresource: "scale", Namespace: "x", Name: "web"},
			},
			want: []bool{true, false, true},
		},
		{
			name:   "core group goes to the API as empty",
			checks: []Check{{Verb: "get", Group: "core", Resource: "core-only"}},
			want:   []bool{true},
		},
		{
			name:    "a failed review fails the call",
			checks:  []Check{{Verb: "get", Resource: "boom"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kube := fake.NewClientset()
			kube.PrependReactor("create", "selfsubjectaccessreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
				review := a.(k8stesting.CreateAction).GetObject().(*authzv1.SelfSubjectAccessReview)
				attr := review.Spec.ResourceAttributes
				switch {
				case attr.Resource == "boom":
					return true, nil, errors.New("boom")
				case attr.Resource == "core-only":
					review.Status.Allowed = attr.Group == ""
				case attr.Verb == "delete":
					review.Status.Allowed = attr.Namespace == "team"
				case attr.Subresource == "scale":
					review.Status.Allowed = attr.Group == "apps" && attr.Name == "web"
				}
				return true, review, nil
			})
			got, err := Review(context.Background(), kube, tt.checks)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !slices.Equal(got, tt.want) {
				t.Errorf("Review = %v, want %v", got, tt.want)
			}
		})
	}
}
