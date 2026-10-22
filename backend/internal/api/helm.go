package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/helm"
)

type helmListResponse struct {
	Releases []helm.Release `json:"releases"`
}

// helmError explains that Helm releases live in Secrets, which need access.
func (s *server) helmError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, helm.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "There's no Helm release by that name here.")
	case apierrors.IsForbidden(err):
		writeError(w, http.StatusForbidden, "forbidden",
			"Helm keeps releases in Secrets. You need permission to list Secrets here to see them.")
	default:
		s.clusterError(w, err)
	}
}

func (s *server) helmReleases(w http.ResponseWriter, r *http.Request) {
	list, err := helm.List(r.Context(), clientsFrom(r).Kube, r.URL.Query().Get("namespace"))
	if err != nil {
		s.helmError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, helmListResponse{Releases: list})
}

func (s *server) helmRelease(w http.ResponseWriter, r *http.Request) {
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if len(validation.IsDNS1123Label(ns)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
		return
	}
	// Helm release names are DNS-1123 subdomains of at most 53 characters.
	if len(validation.IsDNS1123Subdomain(name)) > 0 || len(name) > 53 {
		writeError(w, http.StatusBadRequest, "bad_name", "That isn't a valid release name.")
		return
	}
	d, err := helm.Get(r.Context(), clientsFrom(r).Kube, ns, name)
	if err != nil {
		s.helmError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
