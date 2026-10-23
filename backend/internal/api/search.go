package api

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"glowpod/internal/auth"
	"glowpod/internal/search"
)

// searchTTL is how long one person's name index for a cluster is reused.
const searchTTL = 30 * time.Second

// search is GET /api/v1/clusters/{cluster}/search?q=: objects whose names
// match, for the ⌘K palette, which asks every cluster at once. It lists as
// the signed-in user; ?namespace= are the namespaces to try when they may
// not list a kind in all of them.
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > search.MaxQuery {
		writeError(w, http.StatusBadRequest, "bad_query", "Search for 1 to 100 characters.")
		return
	}
	fallback := slices.Concat([]string(namespacesFrom(r)), s.guessNamespaces(r))
	slices.Sort(fallback)
	fallback = slices.Compact(fallback)

	id, _ := auth.IdentityFrom(r.Context())
	key := clusterFrom(r).ID + "\x00" + id.Key + "\x00" + strings.Join(fallback, ",")
	res, err := s.searches.Search(r.Context(), key, q, search.RESTLister(s.opts.TableClient(clientsFrom(r))), fallback)
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
