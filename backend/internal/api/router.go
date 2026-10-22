// Package api wires Glowpod's HTTP routes and middleware.
//
// API routes live under /api/v1/clusters/{cluster}/... so a second cluster can
// be added without changing URLs. Everything outside /api is the frontend.
package api

import (
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
)

// Options configures the router. Without Provider and Auth only the
// frontend and /healthz are served.
type Options struct {
	// Frontend serves the single-page app for every non-API path.
	Frontend http.Handler
	Provider cluster.Provider
	Auth     *auth.Local
	Overview *overview.Service
	// Hosts are the Host header values to accept; nil skips the check.
	Hosts []string
	// PrometheusURL, when set, is used instead of discovering Prometheus.
	PrometheusURL string
	// Metrics builds the usage reader for a client; defaults to metrics.NewReader.
	Metrics func(kube kubernetes.Interface) overview.MetricsSource
	// TableClient is the REST client for Table output; defaults to the core client.
	TableClient func(c *cluster.Clients) rest.Interface
	// PodUsage reads per-container usage; defaults to metrics.NewReader.
	PodUsage func(kube kubernetes.Interface) pods.UsageSource
	// Now is the clock; defaults to time.Now.
	Now func() time.Time
	// StreamBackoff paces watch retries; nil uses the default.
	StreamBackoff func() stream.Backoff
	Logger        *slog.Logger
}

// promDiscoveryTTL is how long a Prometheus lookup (or its absence) is reused.
const promDiscoveryTTL = 5 * time.Minute

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
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Metrics == nil {
		opts.Metrics = func(kube kubernetes.Interface) overview.MetricsSource { return metrics.NewReader(kube) }
	}
	s := &server{opts: opts, proms: metrics.NewPromCache(promDiscoveryTTL)}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	if opts.Hosts != nil {
		r.Use(auth.RequireHost(opts.Hosts))
	}

	r.Get("/healthz", healthz)
	if opts.Provider != nil && opts.Auth != nil {
		r.Get(auth.LoginPath, opts.Auth.HandleLogin)
		r.Route("/api/v1", func(r chi.Router) {
			r.Use(opts.Auth.Require)
			r.Get("/session", s.session)
			r.Route("/clusters/{cluster}", func(r chi.Router) {
				r.Use(s.requireCluster, requireNamespace)
				r.Get("/overview", s.overview)
				r.Get("/namespaces", s.namespaces)
				r.Get("/events", s.events)
				r.Method(http.MethodGet, "/stream", &stream.Handler{Provider: opts.Provider, Logger: opts.Logger})
				r.Get("/apps", s.listApps)
				r.Get("/apps/{namespace}/{name}", s.getApp)
				r.Get("/pods/{namespace}/{name}", s.pod)
				r.Get("/logs/{namespace}/{pod}", s.streamLogs)
				r.Get("/logs/{namespace}/{pod}/download", s.downloadLogs)
				r.Get("/resources", s.resourceTypes)
				r.Get("/resources/{group}/{version}/{resource}", s.listResource)
				r.Get("/resources/{group}/{version}/{resource}/{name}", s.getResource)
				r.Get("/watch/{group}/{version}/{resource}", s.watchResource)
			})
			r.NotFound(apiNotFound)
			r.MethodNotAllowed(apiMethodNotAllowed)
		})
	}
	r.Handle("/api/*", http.HandlerFunc(apiNotFound))
	if opts.Frontend != nil {
		r.Handle("/*", opts.Frontend)
	}
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
