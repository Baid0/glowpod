package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"
	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/kerr"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/summary"
)

const (
	defaultEventLimit = 20
	maxEventLimit     = 200
	// maxEventPages caps how many events are read to find the newest ones.
	maxEventPages = 10
	listPage      = 500
)

type clientsKey struct{}

// requireCluster checks the {cluster} id and attaches the identity's clients.
func (s *server) requireCluster(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if chi.URLParam(r, "cluster") != s.opts.Provider.Info().ID {
			writeError(w, http.StatusNotFound, "unknown_cluster", "Glowpod isn't connected to that cluster.")
			return
		}
		id, ok := auth.IdentityFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in again.")
			return
		}
		clients, err := s.opts.Provider.ClientsFor(r.Context(), id)
		if err != nil {
			s.clusterError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientsKey{}, clients)))
	})
}

func clientsFrom(r *http.Request) *cluster.Clients {
	c, _ := r.Context().Value(clientsKey{}).(*cluster.Clients)
	return c
}

// requireNamespace rejects ?namespace= values that aren't valid names.
// Empty means all namespaces.
func requireNamespace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ns := r.URL.Query().Get("namespace"); ns != "" && len(validation.IsDNS1123Label(ns)) > 0 {
			writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// classify returns the HTTP status for a cluster error.
func (s *server) classify(err error) int { return kerr.Classify(err).Status }

func (s *server) clusterError(w http.ResponseWriter, err error) {
	c := kerr.Classify(err)
	s.opts.Logger.Warn("cluster request failed", "code", c.Code, "err", err)
	writeJSON(w, c.Status, map[string]errorBody{"error": {Code: c.Code, Message: c.Message, Detail: c.Detail}})
}

// sessionResponse is GET /api/v1/session.
type sessionResponse struct {
	Mode string `json:"mode"`
	// CSRFToken goes in the X-CSRF-Token header of every write.
	CSRFToken string         `json:"csrfToken"`
	User      sessionUser    `json:"user"`
	Clusters  []cluster.Info `json:"clusters"`
}

type sessionUser struct {
	Username string `json:"username"`
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	info := s.opts.Provider.Info()
	resp := sessionResponse{Mode: "local", CSRFToken: auth.CSRFTokenFrom(r.Context()), Clusters: []cluster.Info{info}}

	// Ask the cluster who we are; this is the name RBAC sees.
	id, _ := auth.IdentityFrom(r.Context())
	if clients, err := s.opts.Provider.ClientsFor(r.Context(), id); err == nil {
		review, err := clients.Kube.AuthenticationV1().SelfSubjectReviews().Create(r.Context(), &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
		if err == nil {
			resp.User.Username = review.Status.UserInfo.Username
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	clients := clientsFrom(r)
	id, _ := auth.IdentityFrom(r.Context())
	deps := overview.Deps{
		Kube:    clients.Kube,
		Metrics: s.opts.Metrics(clients.Kube),
		Prometheus: func(ctx context.Context) (metrics.Prometheus, error) {
			if s.opts.PrometheusURL != "" {
				return &metrics.URL{Base: s.opts.PrometheusURL}, nil
			}
			return s.proms.Get(ctx, id.Key, func(ctx context.Context) (metrics.Prometheus, error) {
				t, err := metrics.Discover(ctx, clients.Kube)
				if err != nil || t == nil {
					return nil, err
				}
				return metrics.NewServiceProxy(clients.Kube, *t), nil
			})
		},
	}
	resp, err := s.opts.Overview.Build(r.Context(), id, s.opts.Provider.Info(), deps, r.URL.Query().Get("namespace"))
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type namespacesResponse struct {
	Namespaces []string `json:"namespaces"`
}

func (s *server) namespaces(w http.ResponseWriter, r *http.Request) {
	kube := clientsFrom(r).Kube
	names := []string{}
	opts := metav1.ListOptions{Limit: listPage}
	for {
		list, err := kube.CoreV1().Namespaces().List(r.Context(), opts)
		if err != nil {
			s.clusterError(w, err)
			return
		}
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		if list.Continue == "" {
			break
		}
		opts.Continue = list.Continue
	}
	slices.Sort(names)
	writeJSON(w, http.StatusOK, namespacesResponse{Namespaces: names})
}

type eventsResponse struct {
	Events []summary.Event `json:"events"`
}

// events returns the newest events, newest first. The API can't sort, so it
// reads up to maxEventPages pages and sorts them here.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	limit := defaultEventLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxEventLimit {
			writeError(w, http.StatusBadRequest, "bad_limit", "limit must be between 1 and 200.")
			return
		}
		limit = n
	}

	kube := clientsFrom(r).Kube
	var all []corev1.Event
	opts := metav1.ListOptions{Limit: listPage}
	for range maxEventPages {
		list, err := kube.CoreV1().Events(r.URL.Query().Get("namespace")).List(r.Context(), opts)
		if err != nil {
			s.clusterError(w, err)
			return
		}
		all = append(all, list.Items...)
		if list.Continue == "" {
			break
		}
		opts.Continue = list.Continue
	}

	slices.SortFunc(all, func(a, b corev1.Event) int {
		return cmp.Compare(summary.EventTime(&b).UnixNano(), summary.EventTime(&a).UnixNano())
	})
	out := make([]summary.Event, 0, min(limit, len(all)))
	for i := range all[:min(limit, len(all))] {
		out = append(out, summary.EventOf(&all[i]))
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: out})
}
