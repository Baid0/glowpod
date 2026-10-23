package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpguts"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"glowpod/internal/subjects"
	"glowpod/internal/tunnel"
)

// Prefixes are the only user and group names the agent impersonates: the
// Hub's local users, SSO and LDAP. Anything else, Kubernetes' own system:
// names above all, is refused, so even a Hub someone broke into can't act
// as cluster admin here, only as people who already have rights.
var Prefixes = []string{"glowpod:", "oidc:", "ldap:"}

// checkImpersonation accepts a request only if it impersonates exactly one
// prefixed user, with prefixed groups, and nothing else.
func checkImpersonation(h http.Header) error {
	for name := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "Impersonate-") &&
			name != "Impersonate-User" && name != "Impersonate-Group" {
			return fmt.Errorf("%s isn't allowed", name)
		}
	}
	users := h.Values("Impersonate-User")
	if len(users) != 1 || users[0] == "" {
		return errors.New("every request must impersonate one user")
	}
	if !prefixed(users[0]) {
		return fmt.Errorf("user %q has no Glowpod prefix", users[0])
	}
	for _, g := range h.Values("Impersonate-Group") {
		if g != subjects.Everyone && !prefixed(g) {
			return fmt.Errorf("group %q has no Glowpod prefix", g)
		}
	}
	return nil
}

func prefixed(name string) bool {
	for _, p := range Prefixes {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			return true
		}
	}
	return false
}

// Status is what the agent tells the Hub about its cluster.
type Status struct {
	KubernetesVersion string       `json:"kubernetesVersion"`
	Subjects          subjects.Set `json:"subjects"`
}

// statusTTL limits how often the bindings are listed.
const statusTTL = 15 * time.Second

// handler is what the Hub reaches through the tunnel: the agent's status,
// or the API server as the person the Hub impersonates.
type handler struct {
	kube   kubernetes.Interface
	proxy  *httputil.ReverseProxy
	logger *slog.Logger

	mu      sync.Mutex
	status  Status
	checked time.Time
}

// newHandler proxies to the API server in upstream with the agent's own
// credentials. Upgrades (exec, attach, port-forward) use HTTP/1.1, which
// is the only protocol they work over.
func newHandler(upstream *rest.Config, kube kubernetes.Interface, logger *slog.Logger) (*handler, error) {
	target, err := url.Parse(upstream.Host)
	if err != nil {
		return nil, fmt.Errorf("API server address: %w", err)
	}
	if target.Scheme == "" {
		target.Scheme = "https"
	}
	normal, err := rest.TransportFor(upstream)
	if err != nil {
		return nil, fmt.Errorf("transport to the API server: %w", err)
	}
	h1cfg := rest.CopyConfig(upstream)
	h1cfg.NextProtos = []string{"http/1.1"}
	upgrades, err := rest.TransportFor(h1cfg)
	if err != nil {
		return nil, fmt.Errorf("transport to the API server: %w", err)
	}
	h := &handler{kube: kube, logger: logger}
	h.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			// The agent's service-account token replaces whatever came in.
			pr.Out.Header.Del("Authorization")
		},
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if httpguts.HeaderValuesContainsToken(r.Header["Connection"], "Upgrade") {
				return upgrades.RoundTrip(r)
			}
			return normal.RoundTrip(r)
		}),
		// Watches and logs stream: pass every chunk on at once.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			logger.Warn("API server request failed", "err", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return h, nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == tunnel.StatusPath {
		h.serveStatus(w, r)
		return
	}
	if err := checkImpersonation(r.Header); err != nil {
		h.logger.Warn("refused a request from the Hub", "reason", err.Error(), "path", r.URL.Path)
		http.Error(w, "glowpod-agent: "+err.Error(), http.StatusForbidden)
		return
	}
	h.proxy.ServeHTTP(w, r)
}

func (h *handler) serveStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.checked) > statusTTL {
		set, err := subjects.List(r.Context(), h.kube)
		if err != nil {
			h.logger.Warn("couldn't list role bindings", "err", err)
			http.Error(w, "couldn't list role bindings", http.StatusBadGateway)
			return
		}
		version := ""
		if v, err := h.kube.Discovery().ServerVersion(); err == nil {
			version = v.GitVersion
		}
		h.status = Status{KubernetesVersion: version, Subjects: set}
		h.checked = time.Now()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.status)
}
