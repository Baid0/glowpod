// Package access asks the API server what the signed-in user may do, with
// SelfSubjectAccessReviews, so the web UI and the terminal app can hide
// actions the user can't take. The cluster still decides on the real
// request; this only shapes what's shown.
package access

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/resources"
)

// workers bounds how many reviews run at once.
const workers = 8

// Verbs are the verbs a check may ask about.
var Verbs = map[string]bool{
	"get": true, "list": true, "watch": true, "create": true, "update": true,
	"patch": true, "delete": true, "deletecollection": true,
}

// Check asks "may I verb this?". Group uses the URL form ("core").
type Check struct {
	Verb        string `json:"verb"`
	Group       string `json:"group"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
}

// Review runs one SelfSubjectAccessReview per check, as kube's identity.
// allowed lines up with checks.
func Review(ctx context.Context, kube kubernetes.Interface, checks []Check) ([]bool, error) {
	reviews := kube.AuthorizationV1().SelfSubjectAccessReviews()
	allowed := make([]bool, len(checks))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i, c := range checks {
		g.Go(func() error {
			group := c.Group
			if group == resources.CoreGroup {
				group = ""
			}
			review, err := reviews.Create(ctx, &authzv1.SelfSubjectAccessReview{
				Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authzv1.ResourceAttributes{
					Verb: c.Verb, Group: group, Resource: c.Resource, Subresource: c.Subresource,
					Namespace: c.Namespace, Name: c.Name,
				}},
			}, metav1.CreateOptions{})
			if err != nil {
				return err //nolint:wrapcheck // wrapped below, once
			}
			allowed[i] = review.Status.Allowed && !review.Status.Denied
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("access review: %w", err)
	}
	return allowed, nil
}
