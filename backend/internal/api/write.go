package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/restmapper"

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
	ns, ok := oneNamespace(w, r)
	if !ok {
		return t, "", false
	}
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

// createResource makes a new object of the URL's type from a manifest.
// ?namespace= is where a namespaced object goes when its YAML doesn't say;
// ?dryRun=true checks it with the API server and saves nothing.
func (s *server) createResource(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveType(w, r)
	if !ok {
		return
	}
	ns, ok := oneNamespace(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !readJSON(w, r, &req) {
		return
	}
	dryRun := r.URL.Query().Get("dryRun") == "true"
	res, err := resources.Create(r.Context(), clientsFrom(r).Dynamic, t, ns, req.YAML, dryRun)
	if err != nil {
		s.writeFailure(w, err)
		return
	}
	if !dryRun {
		made := unstructured.Unstructured{Object: res.Object.Object}
		s.audit(r, "create", t, made.GetNamespace(), made.GetName())
	}
	writeJSON(w, http.StatusCreated, res)
}

// applyManifest creates or updates every object in a multi-document manifest
// with server-side apply. Each object has its own result; the request only
// fails when the manifest itself can't be read. ?force=true overwrites
// fields set another way, like kubectl apply --force-conflicts.
func (s *server) applyManifest(w http.ResponseWriter, r *http.Request) {
	ns, ok := oneNamespace(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !readJSON(w, r, &req) {
		return
	}
	dryRun := r.URL.Query().Get("dryRun") == "true"
	clients := clientsFrom(r)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(clients.Discovery)
	force := r.URL.Query().Get("force") == "true"
	res, err := resources.Apply(r.Context(), clients.Dynamic, mapper, ns, req.YAML, dryRun, force)
	if err != nil {
		s.writeFailure(w, err)
		return
	}
	if !dryRun {
		for _, it := range res.Items {
			if it.Result != resources.Failed {
				s.audit(r, "apply", resources.Type{Group: it.Group, Version: it.Version, Resource: it.Resource}, it.Namespace, it.Name)
			}
		}
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
