package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/http/httpguts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

// KubeAPIPrefix is where kubectl reaches a cluster through the Hub, with a
// kubeconfig token: /k8s/clusters/<id>/api/v1/pods.
const KubeAPIPrefix = "/k8s/clusters/"

// upstream is how the Hub reaches one cluster's API server as itself;
// each request then impersonates the token's owner.
type upstream struct {
	target *url.URL
	// normal speaks HTTP/2 where it can; upgrades (exec, attach,
	// port-forward) need HTTP/1.1.
	normal, upgrades http.RoundTripper
}

type upstreams struct {
	mu sync.Mutex
	m  map[string]*upstream
}

var errUnknownCluster = errors.New("no such cluster")

// baseConfiger is a cluster the Hub reaches with its own identity.
type baseConfiger interface{ Base() *rest.Config }

func (h *Hub) upstream(id string) (*upstream, error) {
	h.upstreams.mu.Lock()
	defer h.upstreams.mu.Unlock()
	if u, ok := h.upstreams.m[id]; ok {
		return u, nil
	}
	p, ok := h.cfg.Registry.Get(id)
	if !ok {
		return nil, errUnknownCluster
	}
	b, ok := p.(baseConfiger)
	if !ok {
		return nil, fmt.Errorf("cluster %s can't be proxied", id)
	}
	base := b.Base()
	target, err := url.Parse(base.Host)
	if err != nil {
		return nil, fmt.Errorf("API server address: %w", err)
	}
	if target.Scheme == "" {
		target.Scheme = "https"
	}
	normal, err := rest.TransportFor(base)
	if err != nil {
		return nil, fmt.Errorf("transport to %s: %w", id, err)
	}
	h1 := rest.CopyConfig(base)
	h1.NextProtos = []string{"http/1.1"}
	upgrades, err := rest.TransportFor(h1)
	if err != nil {
		return nil, fmt.Errorf("transport to %s: %w", id, err)
	}
	u := &upstream{target: target, normal: normal, upgrades: upgrades}
	if h.upstreams.m == nil {
		h.upstreams.m = map[string]*upstream{}
	}
	h.upstreams.m[id] = u
	return u, nil
}

// forgetUpstream drops a removed cluster's transports.
func (h *Hub) forgetUpstream(id string) {
	h.upstreams.mu.Lock()
	defer h.upstreams.mu.Unlock()
	delete(h.upstreams.m, id)
}

// kubeAPI proxies kubectl to a cluster as the token's owner: the Hub's own
// identity impersonating them, exactly as the UI does. Nothing the client
// sends can change who that is.
func (h *Hub) kubeAPI(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cluster")
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeStatus(w, http.StatusUnauthorized, metav1.StatusReasonUnauthorized, "Use a kubeconfig from Glowpod Hub (account menu, Kubeconfig).")
		return
	}
	tok, ok := h.checkKubeToken(r.Context(), bearer)
	if !ok {
		writeStatus(w, http.StatusUnauthorized, metav1.StatusReasonUnauthorized, "This kubeconfig expired or was revoked. Download a new one from Glowpod Hub.")
		return
	}
	who := tokenSession(tok)
	if !h.Visible(who, id) {
		writeStatus(w, http.StatusNotFound, metav1.StatusReasonNotFound, fmt.Sprintf("Glowpod Hub has no cluster %q that you can see.", id))
		return
	}
	up, err := h.upstream(id)
	if err != nil {
		h.cfg.Logger.Warn("couldn't proxy to a cluster", "cluster", id, "err", err)
		writeStatus(w, http.StatusBadGateway, metav1.StatusReasonServiceUnavailable, "Glowpod Hub can't reach this cluster.")
		return
	}
	prefix := KubeAPIPrefix + id
	impersonate := transport.ImpersonationConfig{UserName: tok.User, Groups: tok.Groups}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			pr.Out.URL.RawPath = strings.TrimPrefix(pr.In.URL.RawPath, prefix)
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.SetURL(up.target)
			pr.Out.Host = up.target.Host
			// The token, a browser's cookie and any impersonation the client
			// asked for all stop here; the transport adds the Hub's own.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
			for k := range pr.Out.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(k), "Impersonate-") {
					pr.Out.Header.Del(k)
				}
			}
		},
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			rt := up.normal
			if httpguts.HeaderValuesContainsToken(req.Header["Connection"], "Upgrade") {
				rt = up.upgrades
			}
			return transport.NewImpersonatingRoundTripper(impersonate, rt).RoundTrip(req)
		}),
		// Watches and logs stream: pass every chunk on at once.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			h.cfg.Logger.Warn("kubectl request through the Hub failed", "cluster", id, "err", err)
			writeStatus(w, http.StatusBadGateway, metav1.StatusReasonServiceUnavailable, "Glowpod Hub can't reach this cluster right now.")
		},
	}
	proxy.ServeHTTP(w, r)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// writeStatus answers the way an API server does, so kubectl shows the
// message.
func writeStatus(w http.ResponseWriter, code int, reason metav1.StatusReason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure, Message: message, Reason: reason, Code: int32(code), //nolint:gosec // HTTP status codes fit
	})
}
