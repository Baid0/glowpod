package api

import (
	"net/http"

	"golang.org/x/sync/errgroup"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/resources"
)

const (
	// maxAccessChecks bounds one request; a page asks for a handful.
	maxAccessChecks = 50
	accessWorkers   = 8
)

var accessVerbs = map[string]bool{
	"get": true, "list": true, "watch": true, "create": true, "update": true,
	"patch": true, "delete": true, "deletecollection": true,
}

// accessCheck asks "may I verb this?". Group uses the URL form ("core").
type accessCheck struct {
	Verb        string `json:"verb"`
	Group       string `json:"group"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
}

type accessRequest struct {
	Checks []accessCheck `json:"checks"`
}

type accessResponse struct {
	// Allowed lines up with the request's checks.
	Allowed []bool `json:"allowed"`
}

// access answers permission checks with SelfSubjectAccessReviews, so the
// UI can hide actions the signed-in user can't take. The cluster still
// decides on the real request; this only shapes what's shown.
func (s *server) access(w http.ResponseWriter, r *http.Request) {
	var req accessRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Checks) == 0 || len(req.Checks) > maxAccessChecks {
		writeError(w, http.StatusBadRequest, "bad_checks", "Send between 1 and 50 checks.")
		return
	}
	for _, c := range req.Checks {
		if !accessVerbs[c.Verb] || c.Resource == "" {
			writeError(w, http.StatusBadRequest, "bad_checks", "Each check needs a known verb and a resource.")
			return
		}
	}

	reviews := clientsFrom(r).Kube.AuthorizationV1().SelfSubjectAccessReviews()
	allowed := make([]bool, len(req.Checks))
	g, ctx := errgroup.WithContext(r.Context())
	g.SetLimit(accessWorkers)
	for i, c := range req.Checks {
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
				return err
			}
			allowed[i] = review.Status.Allowed && !review.Status.Denied
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accessResponse{Allowed: allowed})
}
