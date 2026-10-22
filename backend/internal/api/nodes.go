package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/auth"
	"glowpod/internal/nodes"
)

// nodeName validates {name} or writes a 400.
func nodeName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := chi.URLParam(r, "name")
	if len(validation.IsDNS1123Subdomain(name)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_node", "That isn't a valid node name.")
		return "", false
	}
	return name, true
}

func (s *server) node(w http.ResponseWriter, r *http.Request) {
	name, ok := nodeName(w, r)
	if !ok {
		return
	}
	kube := clientsFrom(r).Kube
	d, err := nodes.Build(r.Context(), kube, s.opts.Metrics(kube), name, s.opts.Now())
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type cordonRequest struct {
	Cordon *bool `json:"cordon"`
}

func (s *server) cordonNode(w http.ResponseWriter, r *http.Request) {
	name, ok := nodeName(w, r)
	if !ok {
		return
	}
	var req cordonRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Cordon == nil {
		writeError(w, http.StatusBadRequest, "bad_body", "Say whether to cordon (true) or uncordon (false).")
		return
	}
	if err := nodes.Cordon(r.Context(), clientsFrom(r).Kube, name, *req.Cordon); err != nil {
		s.clusterError(w, err)
		return
	}
	action := "uncordon"
	if *req.Cordon {
		action = "cordon"
	}
	s.auditNode(r, action, name)
	w.WriteHeader(http.StatusNoContent)
}

// drainPlan previews a drain for the confirmation dialog.
func (s *server) drainPlan(w http.ResponseWriter, r *http.Request) {
	name, ok := nodeName(w, r)
	if !ok {
		return
	}
	pods, err := nodes.PodsOn(r.Context(), clientsFrom(r).Kube, name)
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes.PlanFor(pods))
}

func (s *server) drainNode(w http.ResponseWriter, r *http.Request) {
	name, ok := nodeName(w, r)
	if !ok {
		return
	}
	res, err := nodes.Drain(r.Context(), clientsFrom(r).Kube, name)
	if err != nil {
		s.clusterError(w, err)
		return
	}
	s.auditNode(r, "drain", name)
	writeJSON(w, http.StatusOK, res)
}

func (s *server) auditNode(r *http.Request, action, name string) {
	id, _ := auth.IdentityFrom(r.Context())
	s.opts.Logger.Info("changed node", "action", action, "identity", id.Key, "name", name)
}
