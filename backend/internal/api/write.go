package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"glowpod/internal/auth"
	"glowpod/internal/resources"
)

// maxBody is above etcd's 1.5 MiB object limit, plus room for JSON quoting.
const maxBody = 4 << 20

// readJSON decodes a small JSON body or writes a 400.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", "The request body isn't valid JSON for this action.")
		return false
	}
	return true
}

// target resolves the type and namespace for a write on one object.
func (s *server) target(w http.ResponseWriter, r *http.Request) (resources.Type, string, bool) {
	t, ok := s.resolveType(w, r)
	if !ok {
		return t, "", false
	}
	ns := r.URL.Query().Get("namespace")
	if t.Namespaced && ns == "" {
		writeError(w, http.StatusBadRequest, "namespace_required", "Say which namespace it's in.")
		return t, "", false
	}
	if !t.Namespaced {
		ns = ""
	}
	return t, ns, true
}

// writeFailure reports a bad request as 400 and anything else as a cluster error.
func (s *server) writeFailure(w http.ResponseWriter, err error) {
	var ie *resources.InputError
	if errors.As(err, &ie) {
		writeError(w, http.StatusBadRequest, ie.Code, ie.Message)
		return
	}
	s.clusterError(w, err)
}

// audit records who changed what. Never the content.
func (s *server) audit(r *http.Request, action string, t resources.Type, ns, name string) {
	id, _ := auth.IdentityFrom(r.Context())
	s.opts.Logger.Info("changed resource", "action", action, "identity", id.Key,
		"group", t.Group, "resource", t.Resource, "namespace", ns, "name", name)
}

type updateRequest struct {
	YAML string `json:"yaml"`
}

// updateResource saves an edited manifest. ?dryRun=true checks it with the
// API server and returns before/after for the diff, saving nothing.
func (s *server) updateResource(w http.ResponseWriter, r *http.Request) {
	t, ns, ok := s.target(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := chi.URLParam(r, "name")
	dryRun := r.URL.Query().Get("dryRun") == "true"
	res, err := resources.Update(r.Context(), clientsFrom(r).Dynamic, t, ns, name, req.YAML, dryRun)
	if err != nil {
		s.writeFailure(w, err)
		return
	}
	if !dryRun {
		s.audit(r, "update", t, ns, name)
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *server) deleteResource(w http.ResponseWriter, r *http.Request) {
	t, ns, ok := s.target(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if err := resources.Delete(r.Context(), clientsFrom(r).Dynamic, t, ns, name, r.URL.Query().Get("propagation")); err != nil {
		s.writeFailure(w, err)
		return
	}
	s.audit(r, "delete", t, ns, name)
	w.WriteHeader(http.StatusNoContent)
}

type scaleRequest struct {
	Replicas *int64 `json:"replicas"`
}

func (s *server) scaleResource(w http.ResponseWriter, r *http.Request) {
	t, ns, ok := s.target(w, r)
	if !ok {
		return
	}
	var req scaleRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Replicas == nil {
		writeError(w, http.StatusBadRequest, "bad_replicas", "Say how many replicas you want.")
		return
	}
	name := chi.URLParam(r, "name")
	if err := resources.Scale(r.Context(), clientsFrom(r).Dynamic, t, ns, name, *req.Replicas); err != nil {
		s.writeFailure(w, err)
		return
	}
	s.audit(r, "scale", t, ns, name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) restartResource(w http.ResponseWriter, r *http.Request) {
	t, ns, ok := s.target(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if err := resources.Restart(r.Context(), clientsFrom(r).Dynamic, t, ns, name, s.opts.Now()); err != nil {
		s.writeFailure(w, err)
		return
	}
	s.audit(r, "restart", t, ns, name)
	w.WriteHeader(http.StatusNoContent)
}
