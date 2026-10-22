package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"

	"glowpod/internal/resources"
	"glowpod/internal/stream"
)

type typesResponse struct {
	Types []resources.Type `json:"types"`
}

// resourceTypes lists every resource type the identity's discovery sees.
func (s *server) resourceTypes(w http.ResponseWriter, r *http.Request) {
	types, err := resources.Discover(clientsFrom(r).Discovery)
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, typesResponse{Types: types})
}

// resolveType finds {group}/{version}/{resource} or writes a 404.
func (s *server) resolveType(w http.ResponseWriter, r *http.Request) (resources.Type, bool) {
	t, err := resources.Resolve(
		clientsFrom(r).Discovery,
		chi.URLParam(r, "group"), chi.URLParam(r, "version"), chi.URLParam(r, "resource"),
	)
	switch {
	case errors.Is(err, resources.ErrUnknownType):
		writeError(w, http.StatusNotFound, "unknown_resource", "This cluster doesn't have that kind of resource.")
		return resources.Type{}, false
	case err != nil:
		s.clusterError(w, err)
		return resources.Type{}, false
	}
	return t, true
}

// selectors validates ?labelSelector= and ?fieldSelector=.
func selectors(w http.ResponseWriter, r *http.Request) (label, field string, ok bool) {
	q := r.URL.Query()
	label, field = q.Get("labelSelector"), q.Get("fieldSelector")
	if _, err := labels.Parse(label); err != nil {
		writeError(w, http.StatusBadRequest, "bad_selector", "That label selector isn't valid: "+err.Error())
		return "", "", false
	}
	if _, err := fields.ParseSelector(field); err != nil {
		writeError(w, http.StatusBadRequest, "bad_selector", "That field selector isn't valid: "+err.Error())
		return "", "", false
	}
	return label, field, true
}

// listResource returns one page of kubectl-style rows.
func (s *server) listResource(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveType(w, r)
	if !ok {
		return
	}
	label, field, ok := selectors(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := int64(resources.DefaultLimit)
	if v := q.Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > resources.MaxLimit {
			writeError(w, http.StatusBadRequest, "bad_limit", "limit must be between 1 and 500.")
			return
		}
		limit = n
	}
	table, err := resources.ListTable(r.Context(), s.opts.TableClient(clientsFrom(r)), t, resources.ListOptions{
		Namespace:     q.Get("namespace"),
		LabelSelector: label,
		FieldSelector: field,
		Continue:      q.Get("continue"),
		Limit:         limit,
	})
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, table)
}

// getResource returns one object as JSON, and YAML with ?format=yaml.
// Secret values stay hidden unless ?reveal=true, which the UI sends only
// after an explicit click; the read itself still needs the user's RBAC.
func (s *server) getResource(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveType(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	namespace := q.Get("namespace")
	if t.Namespaced && namespace == "" {
		writeError(w, http.StatusBadRequest, "namespace_required", "Say which namespace it's in.")
		return
	}
	obj, err := resources.Get(r.Context(), clientsFrom(r).Dynamic, t, namespace, chi.URLParam(r, "name"),
		q.Get("reveal") == "true", q.Get("format") == "yaml")
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obj)
}

// watchResource streams "changed" messages for an open list.
func (s *server) watchResource(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveType(w, r)
	if !ok {
		return
	}
	if !t.Can("watch") {
		writeError(w, http.StatusBadRequest, "not_watchable", "This resource can't be watched for changes.")
		return
	}
	label, _, ok := selectors(w, r)
	if !ok {
		return
	}
	src := stream.ListSource(clientsFrom(r).Dynamic, t.GVR(), t.Namespaced, r.URL.Query().Get("namespace"), label)
	stream.Serve(w, r, []stream.Source{src}, s.opts.StreamBackoff, s.opts.Logger)
}
