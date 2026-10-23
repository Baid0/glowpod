package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	authnv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/events"
	"glowpod/internal/kerr"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/scope"
	"glowpod/internal/summary"
)

const (
	defaultEventLimit = 20
	maxEventLimit     = 200
	listPage          = 500
)

type clientsKey struct{}

type clusterKey struct{}

// requireCluster finds the {cluster} and attaches it and the identity's
// clients for it.
func (s *server) requireCluster(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider, ok := s.opts.Clusters.Get(chi.URLParam(r, "cluster"))
		if !ok {
			writeError(w, http.StatusNotFound, "unknown_cluster", "Glowpod isn't connected to that cluster.")
			return
		}
		id, ok := auth.IdentityFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in again.")
			return
		}
		clients, err := provider.ClientsFor(r.Context(), id)
		if err != nil {
			s.clusterError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), clientsKey{}, clients)
		ctx = context.WithValue(ctx, clusterKey{}, provider.Info())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func clientsFrom(r *http.Request) *cluster.Clients {
	c, _ := r.Context().Value(clientsKey{}).(*cluster.Clients)
	return c
}

// clusterFrom is the request's cluster.
func clusterFrom(r *http.Request) cluster.Info {
	c, _ := r.Context().Value(clusterKey{}).(cluster.Info)
	return c
}

type scopeKey struct{}

// requireNamespace checks ?namespace=: empty for all namespaces, one name,
// or a comma-separated list of up to scope.Max names.
func requireNamespace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns, err := scope.Parse(r.URL.Query().Get("namespace"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name, or list of up to 20.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopeKey{}, ns)))
	})
}

// namespacesFrom is the request's namespaces; none means all.
func namespacesFrom(r *http.Request) scope.Namespaces {
	ns, _ := r.Context().Value(scopeKey{}).(scope.Namespaces)
	return ns
}

// oneNamespace is for requests about a single object or namespace: at most
// one name in ?namespace=.
func oneNamespace(w http.ResponseWriter, r *http.Request) (string, bool) {
	ns := namespacesFrom(r)
	if len(ns) > 1 {
		writeError(w, http.StatusBadRequest, "one_namespace", "Say which one namespace it's in.")
		return "", false
	}
	single, _ := ns.Single()
	return single, true
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
	Version string `json:"version,omitempty"`
	Mode    string `json:"mode"`
	Method  string `json:"method"`
	// CSRFToken goes in the X-CSRF-Token header of every write.
	CSRFToken string      `json:"csrfToken"`
	User      sessionUser `json:"user"`
	// Hub is set on a Glowpod Hub: the person's role, and first start.
	Hub any `json:"hub,omitempty"`
}

type sessionUser struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups,omitempty"`
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	info := auth.SessionInfoFrom(r.Context())
	resp := sessionResponse{
		Version:   s.opts.Version,
		Mode:      info.Mode,
		Method:    info.Method,
		CSRFToken: auth.CSRFTokenFrom(r.Context()),
		User:      sessionUser{Username: info.User, Groups: info.Groups},
	}
	if s.opts.Hub != nil {
		resp.Hub = s.opts.Hub.Session(r.Context(), info)
	}
	// Local mode doesn't know the kubeconfig user's name; ask the current cluster.
	if current := s.opts.Clusters.Current(); resp.User.Username == "" && current != nil {
		id, _ := auth.IdentityFrom(r.Context())
		if clients, err := current.ClientsFor(r.Context(), id); err == nil {
			review, err := clients.Kube.AuthenticationV1().SelfSubjectReviews().Create(r.Context(), &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
			if err == nil {
				resp.User.Username = review.Status.UserInfo.Username
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// clustersResponse is GET /api/v1/clusters.
type clustersResponse struct {
	Clusters []cluster.Info `json:"clusters"`
}

func (s *server) clusters(w http.ResponseWriter, r *http.Request) {
	list := s.opts.Clusters.List()
	if s.opts.Hub != nil {
		info := auth.SessionInfoFrom(r.Context())
		list = slices.DeleteFunc(list, func(c cluster.Info) bool { return !s.opts.Hub.Visible(info, c.ID) })
	}
	writeJSON(w, http.StatusOK, clustersResponse{Clusters: list})
}

// overviewDeps are the request's clients for an Overview. Prometheus
// lookups are cached per cluster and identity.
func (s *server) overviewDeps(r *http.Request) overview.Deps {
	clients := clientsFrom(r)
	info := clusterFrom(r)
	id, _ := auth.IdentityFrom(r.Context())
	return overview.Deps{
		Kube:    clients.Kube,
		Metrics: s.opts.Metrics(clients.Kube),
		Prometheus: func(ctx context.Context) (metrics.Prometheus, error) {
			if s.opts.PrometheusURL != "" && info.ID == s.opts.Clusters.Current().Info().ID {
				return &metrics.URL{Base: s.opts.PrometheusURL}, nil
			}
			return s.proms.Get(ctx, info.ID+"|"+id.Key, func(ctx context.Context) (metrics.Prometheus, error) {
				t, err := metrics.Discover(ctx, clients.Kube)
				if err != nil || t == nil {
					return nil, err
				}
				return metrics.NewServiceProxy(clients.Kube, *t), nil
			})
		},
	}
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	resp, err := s.opts.Overview.Build(r.Context(), id, clusterFrom(r), s.overviewDeps(r), namespacesFrom(r))
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// summaryTimeout bounds one cluster's row on the Clusters page, so a
// cluster that doesn't answer shows as unreachable instead of loading forever.
// A variable for tests.
var summaryTimeout = 10 * time.Second

// clusterSummary is GET /api/v1/clusters/{cluster}/summary: one row of the
// Clusters page, the same numbers as the cluster's Overview.
type clusterSummary struct {
	Cluster      overview.ClusterInfo `json:"cluster"`
	Gauges       summaryGauges        `json:"gauges"`
	Health       overview.Health      `json:"health"`
	ProblemCount int                  `json:"problemCount"`
	Thresholds   [2]float64           `json:"thresholds"`
	GeneratedAt  time.Time            `json:"generatedAt"`
}

type summaryGauges struct {
	CPU    metrics.Gauge `json:"cpu"`
	Memory metrics.Gauge `json:"memory"`
	Pods   metrics.Gauge `json:"pods"`
}

func (s *server) summary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), summaryTimeout)
	defer cancel()
	id, _ := auth.IdentityFrom(r.Context())
	deps := s.overviewDeps(r)
	// The row has no network gauge; don't look for Prometheus.
	deps.Prometheus = func(context.Context) (metrics.Prometheus, error) { return nil, nil }
	// Some calls (the server version) can't be cancelled, so don't wait on
	// Build past the deadline; it finishes on its own when the dial gives up.
	type built struct {
		resp *overview.Response
		err  error
	}
	done := make(chan built, 1)
	go func() {
		resp, err := s.opts.Overview.Build(ctx, id, clusterFrom(r), deps, nil)
		done <- built{resp, err}
	}()
	var resp *overview.Response
	select {
	case b := <-done:
		if b.err != nil && ctx.Err() == nil {
			s.clusterError(w, b.err)
			return
		}
		resp = b.resp
	case <-ctx.Done():
	}
	if resp == nil {
		writeError(w, http.StatusGatewayTimeout, "cluster_timeout", "The cluster didn't answer within 10 seconds.")
		return
	}
	writeJSON(w, http.StatusOK, clusterSummary{
		Cluster:      resp.Cluster,
		Gauges:       summaryGauges{CPU: resp.Gauges.CPU, Memory: resp.Gauges.Memory, Pods: resp.Gauges.Pods},
		Health:       resp.Health,
		ProblemCount: resp.ProblemCount,
		Thresholds:   resp.Thresholds,
		GeneratedAt:  resp.GeneratedAt,
	})
}

type namespacesResponse struct {
	Namespaces []string `json:"namespaces"`
	// Restricted is set when the user may not list namespaces: the list
	// holds only the ones Glowpod could guess, and the UI lets them type one.
	Restricted bool `json:"restricted,omitempty"`
}

func (s *server) namespaces(w http.ResponseWriter, r *http.Request) {
	kube := clientsFrom(r).Kube
	names := []string{}
	opts := metav1.ListOptions{Limit: listPage}
	for {
		list, err := kube.CoreV1().Namespaces().List(r.Context(), opts)
		if apierrors.IsForbidden(err) {
			writeJSON(w, http.StatusOK, namespacesResponse{Namespaces: guessNamespaces(r), Restricted: true})
			return
		}
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

// guessNamespaces offers a service account its own namespace.
func guessNamespaces(r *http.Request) []string {
	user := auth.SessionInfoFrom(r.Context()).User
	if rest, ok := strings.CutPrefix(user, "system:serviceaccount:"); ok {
		if ns, _, ok := strings.Cut(rest, ":"); ok && ns != "" {
			return []string{ns}
		}
	}
	return []string{}
}

type eventsResponse struct {
	Events []summary.Event `json:"events"`
}

// events returns the newest events, newest first.
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

	// ?kind=&name= narrows to one object's events.
	var selector string
	if kind, name := r.URL.Query().Get("kind"), r.URL.Query().Get("name"); kind != "" || name != "" {
		if kind == "" || name == "" || len(kind) > 63 || len(name) > 253 {
			writeError(w, http.StatusBadRequest, "bad_object", "Give both kind and name to see one object's events.")
			return
		}
		selector = events.About(kind, name)
	}
	out, err := events.Newest(r.Context(), clientsFrom(r).Kube, namespacesFrom(r).Targets(), selector, limit)
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: out})
}
