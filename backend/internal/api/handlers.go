package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/kerr"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/paging"
	"glowpod/internal/scope"
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
	CSRFToken string         `json:"csrfToken"`
	User      sessionUser    `json:"user"`
	Clusters  []cluster.Info `json:"clusters"`
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
		Clusters:  []cluster.Info{s.opts.Provider.Info()},
	}
	// Local mode doesn't know the kubeconfig user's name; ask the cluster.
	if resp.User.Username == "" {
		id, _ := auth.IdentityFrom(r.Context())
		if clients, err := s.opts.Provider.ClientsFor(r.Context(), id); err == nil {
			review, err := clients.Kube.AuthenticationV1().SelfSubjectReviews().Create(r.Context(), &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
			if err == nil {
				resp.User.Username = review.Status.UserInfo.Username
			}
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
	resp, err := s.opts.Overview.Build(r.Context(), id, s.opts.Provider.Info(), deps, namespacesFrom(r))
	if err != nil {
		s.clusterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	opts := metav1.ListOptions{Limit: listPage}
	// ?kind=&name= narrows to one object's events.
	if kind, name := r.URL.Query().Get("kind"), r.URL.Query().Get("name"); kind != "" || name != "" {
		if kind == "" || name == "" || len(kind) > 63 || len(name) > 253 {
			writeError(w, http.StatusBadRequest, "bad_object", "Give both kind and name to see one object's events.")
			return
		}
		opts.FieldSelector = fields.Set{"involvedObject.kind": kind, "involvedObject.name": name}.AsSelector().String()
	}
	all, _, err := paging.Across(r.Context(), namespacesFrom(r).Targets(),
		func(ctx context.Context, namespace string, _ metav1.ListOptions) ([]corev1.Event, string, error) {
			return newestEvents(ctx, kube, namespace, opts)
		})
	if err != nil {
		s.clusterError(w, err)
		return
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

// newestEvents reads up to maxEventPages pages of a namespace's events.
// (paging.Across calls it once per namespace; it pages on its own so the
// cap applies per namespace.)
func newestEvents(ctx context.Context, kube kubernetes.Interface, namespace string, opts metav1.ListOptions) ([]corev1.Event, string, error) {
	var all []corev1.Event
	for range maxEventPages {
		list, err := kube.CoreV1().Events(namespace).List(ctx, opts)
		if err != nil {
			return nil, "", err
		}
		all = append(all, list.Items...)
		if list.Continue == "" {
			break
		}
		opts.Continue = list.Continue
	}
	return all, "", nil
}
