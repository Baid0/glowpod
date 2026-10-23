// Package api wires Glowpod's HTTP routes and middleware.
//
// API routes live under /api/v1/clusters/{cluster}/... so a second cluster can
// be added without changing URLs. Everything outside /api is the frontend.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/pods"
	"glowpod/internal/stream"
	"glowpod/internal/terminal"
)

// Options configures the router. Without Clusters and Auth only the
// frontend and /healthz are served.
type Options struct {
	// Version is Glowpod's version, shown in Settings.
	Version string
	// Frontend serves the single-page app for every non-API path.
	Frontend http.Handler
	// Clusters are the clusters served, under /api/v1/clusters/{cluster}.
	Clusters *cluster.Registry
	// Hub, when set, makes this Glowpod a Hub: agents connect to it, and
	// people see only the clusters they have access to.
	Hub      Hub
	Auth     auth.Authenticator
	Overview *overview.Service
	// Hosts are the Host header values to accept; nil skips the check.
	// /healthz is always answered, for kubelet probes.
	Hosts []string
	// HSTS sends Strict-Transport-Security; for HTTPS installs.
	HSTS bool
	// PrometheusURL, when set, is used instead of discovering Prometheus on
	// the current cluster (Clusters.Current).
	PrometheusURL string
	// Metrics builds the usage reader for a client; defaults to metrics.NewReader.
	Metrics func(kube kubernetes.Interface) overview.MetricsSource
	// TableClient is the REST client for Table output; defaults to the core client.
	TableClient func(c *cluster.Clients) rest.Interface
	// PodUsage reads per-container usage; defaults to metrics.NewReader.
	PodUsage func(kube kubernetes.Interface) pods.UsageSource
	// Now is the clock; defaults to time.Now.
	Now func() time.Time
	// Terminal starts shells; defaults to terminal.Remote (pods/exec).
	Terminal func(c *cluster.Clients) terminal.Starter
	// StreamBackoff paces watch retries; nil uses the default.
	StreamBackoff func() stream.Backoff
	Logger        *slog.Logger
}

// promDiscoveryTTL is how long a Prometheus lookup (or its absence) is reused.
const promDiscoveryTTL = 5 * time.Minute

// Hub is what the router needs from a Glowpod Hub (internal/hub).
type Hub interface {
	// MountPublic adds the agents' routes, outside the browser session.
	MountPublic(r chi.Router)
	// MountAPI adds /api/v1/hub/… for signed-in people.
	MountAPI(r chi.Router)
	// Session is the Hub's part of GET /api/v1/session.
	Session(ctx context.Context, info auth.SessionInfo) any
	// Visible says whether to list a cluster for someone.
	Visible(info auth.SessionInfo, clusterID string) bool
}

type server struct {
	opts  Options
	proms *metrics.PromCache
}

// NewRouter returns the root HTTP handler.
func NewRouter(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Overview == nil {
		opts.Overview = overview.NewService()
	}
	if opts.TableClient == nil {
		opts.TableClient = func(c *cluster.Clients) rest.Interface { return c.Kube.CoreV1().RESTClient() }
	}
	if opts.PodUsage == nil {
		opts.PodUsage = func(kube kubernetes.Interface) pods.UsageSource { return metrics.NewReader(kube) }
	}
	if opts.Terminal == nil {
		opts.Terminal = func(c *cluster.Clients) terminal.Starter { return terminal.Remote(c.Config, c.Kube) }
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Metrics == nil {
		opts.Metrics = func(kube kubernetes.Interface) overview.MetricsSource { return metrics.NewReader(kube) }
	}
	s := &server{opts: opts, proms: metrics.NewPromCache(promDiscoveryTTL)}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders(opts.HSTS))
	r.Get("/healthz", healthz)
	// Agents reach these with the address in their manifest, which may not
	// be the one people use; they carry their own secret instead of a cookie.
	if opts.Hub != nil {
		opts.Hub.MountPublic(r)
	}

	r.Group(func(r chi.Router) {
		if opts.Hosts != nil {
			r.Use(auth.RequireHost(opts.Hosts))
		}
		if opts.Clusters != nil && opts.Auth != nil {
			opts.Auth.MountRoot(r)
			r.Route("/api/v1", func(r chi.Router) {
				opts.Auth.MountAPI(r)
				r.Group(func(r chi.Router) {
					r.Use(opts.Auth.Require, auth.RequireCSRF)
					r.Get("/session", s.session)
					r.Get("/clusters", s.clusters)
					if opts.Hub != nil {
						r.Route("/hub", opts.Hub.MountAPI)
					}
					r.Route("/clusters/{cluster}", func(r chi.Router) {
						r.Use(s.requireCluster, requireNamespace)
						r.Get("/overview", s.overview)
						r.Get("/summary", s.summary)
						r.Get("/namespaces", s.namespaces)
						r.Get("/events", s.events)
						r.Method(http.MethodGet, "/stream", &stream.Handler{Clients: clientsFrom, Logger: opts.Logger})
						r.Get("/apps", s.listApps)
						r.Get("/apps/{namespace}/{name}", s.getApp)
						r.Get("/pods/{namespace}/{name}", s.pod)
						r.Get("/map", s.resourceMap)
						r.Get("/helm", s.helmReleases)
						r.Get("/helm/{namespace}/{name}", s.helmRelease)
						r.Get("/nodes/{name}", s.node)
						r.Post("/nodes/{name}/cordon", s.cordonNode)
						r.Get("/nodes/{name}/drain", s.drainPlan)
						r.Post("/nodes/{name}/drain", s.drainNode)
						r.Get("/logs/{namespace}/{pod}", s.streamLogs)
						r.Get("/logs/{namespace}/{pod}/download", s.downloadLogs)
						r.Get("/exec/{namespace}/{pod}", s.openTerminal)
						r.Get("/resources", s.resourceTypes)
						r.Get("/resources/{group}/{version}/{resource}", s.listResource)
						r.Post("/resources/{group}/{version}/{resource}", s.createResource)
						r.Post("/apply", s.applyManifest)
						r.Get("/resources/{group}/{version}/{resource}/{name}", s.getResource)
						r.Put("/resources/{group}/{version}/{resource}/{name}", s.updateResource)
						r.Delete("/resources/{group}/{version}/{resource}/{name}", s.deleteResource)
						r.Put("/resources/{group}/{version}/{resource}/{name}/scale", s.scaleResource)
						r.Post("/resources/{group}/{version}/{resource}/{name}/restart", s.restartResource)
						r.Post("/access", s.access)
						r.Get("/watch/{group}/{version}/{resource}", s.watchResource)
					})
				})
				r.NotFound(apiNotFound)
				r.MethodNotAllowed(apiMethodNotAllowed)
			})
		}
		r.Handle("/api/*", http.HandlerFunc(apiNotFound))
		if opts.Frontend != nil {
			r.Handle("/*", opts.Frontend)
		}
	})
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "There's nothing at this address.")
}

func apiMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "That action isn't supported here.")
}

// errorBody is the JSON shape of every API error.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Detail is the cluster's own explanation, when it has one worth showing.
	Detail string `json:"detail,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]errorBody{"error": {Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the status line is already sent; nothing useful to do on error
}
