package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/apps"
)

type appsResponse struct {
	Apps []apps.App `json:"apps"`
}

// listApps returns the apps in ?namespace= (all namespaces when empty).
func (s *server) listApps(w http.ResponseWriter, r *http.Request) {
	list, err := apps.List(r.Context(), clientsFrom(r).Kube, namespacesFrom(r), s.opts.Now())
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, appsResponse{Apps: list})
}

// getApp returns one app's workloads, pods, problems and recent deploys.
func (s *server) getApp(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "namespace")
	if len(validation.IsDNS1123Label(ns)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
		return
	}
	d, err := apps.Get(r.Context(), clientsFrom(r).Kube, ns, chi.URLParam(r, "name"), s.opts.Now())
	switch {
	case errors.Is(err, apps.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "There's no app by that name in "+ns+".")
	case err != nil:
		s.clusterError(w, err)
	default:
		writeJSON(w, http.StatusOK, d)
	}
}
