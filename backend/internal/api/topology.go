package api

import (
	"net/http"

	"glowpod/internal/topology"
)

// resourceMap draws one namespace: a whole cluster would be unreadable.
func (s *server) resourceMap(w http.ResponseWriter, r *http.Request) {
	ns, one := namespacesFrom(r).Single()
	if !one {
		writeError(w, http.StatusBadRequest, "namespace_required", "Pick one namespace to see its map.")
		return
	}
	m, err := topology.Build(r.Context(), clientsFrom(r).Kube, ns, s.opts.Now())
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}
