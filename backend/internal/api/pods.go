package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/pods"
)

// pod returns the pod detail page's data.
func (s *server) pod(w http.ResponseWriter, r *http.Request) {
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if len(validation.IsDNS1123Label(ns)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
		return
	}
	clients := clientsFrom(r)
	d, err := pods.Build(r.Context(), clients.Kube, s.opts.PodUsage(clients.Kube), ns, name, s.opts.Now())
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
