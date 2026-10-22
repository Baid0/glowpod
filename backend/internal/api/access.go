package api

import (
	"net/http"

	"glowpod/internal/access"
)

// maxAccessChecks bounds one request; a page asks for a handful.
const maxAccessChecks = 50

type accessRequest struct {
	Checks []access.Check `json:"checks"`
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
		if !access.Verbs[c.Verb] || c.Resource == "" {
			writeError(w, http.StatusBadRequest, "bad_checks", "Each check needs a known verb and a resource.")
			return
		}
	}

	allowed, err := access.Review(r.Context(), clientsFrom(r).Kube, req.Checks)
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accessResponse{Allowed: allowed})
}
