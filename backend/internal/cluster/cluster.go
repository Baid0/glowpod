// Package cluster connects to Kubernetes and hands out clients that act as
// the signed-in identity.
//
// A Registry holds every cluster Glowpod serves: each kubeconfig context in
// local mode, the one it runs in otherwise. Routes and caches carry the
// cluster id.
package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// dialTimeout bounds connecting to a kubeconfig's API server.
const dialTimeout = 10 * time.Second

// DefaultID is the id of the cluster an in-cluster Glowpod runs in.
const DefaultID = "default"

// Info describes a cluster connection. It never holds credentials.
type Info struct {
	ID string `json:"id"`
	// Name is the kubeconfig context name, shown in the cluster picker.
	Name string `json:"name"`
	// Server is the API server host, without credentials or path.
	Server string `json:"server"`
	// Current marks the cluster to open first: the kubeconfig's current
	// context, or the one given with -context.
	Current bool `json:"current,omitempty"`
}

// Identity is who a request acts as. Key separates per-user state such as
// caches and metric history; it is never a credential.
//
// In server mode an identity is either impersonated (User and Groups, for
// SSO and LDAP sign-in) or carries the user's own bearer Token (a
// service-account token, or an OIDC ID token passed through). Token is a
// credential: Identity redacts it when logged or printed.
type Identity struct {
	Key    string
	User   string
	Groups []string
	Token  string
}

// String keeps the token out of fmt output.
func (i Identity) String() string { return i.Key }

// LogValue keeps the token out of structured logs.
func (i Identity) LogValue() slog.Value { return slog.StringValue(i.Key) }

// TokenIdentity identifies a bearer token by its hash, so the key can be
// logged and used for caches without revealing the token.
func TokenIdentity(token string) Identity {
	sum := sha256.Sum256([]byte(token))
	return Identity{Key: "token:" + hex.EncodeToString(sum[:16]), Token: token}
}

// UserIdentity is someone to impersonate. The key covers the groups, so a
// change in group membership gets fresh clients and caches.
func UserIdentity(user string, groups []string) Identity {
	sorted := slices.Clone(groups)
	slices.Sort(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return Identity{Key: "user:" + user + "|" + hex.EncodeToString(sum[:8]), User: user, Groups: sorted}
}

// Clients are Kubernetes clients bound to one identity.
type Clients struct {
	Kube kubernetes.Interface
	// Dynamic reaches any resource, CRDs included.
	Dynamic dynamic.Interface
	// Discovery caches the API's resource list for this identity.
	Discovery discovery.CachedDiscoveryInterface
	// Config is the identity's REST config, for streaming calls such as exec.
	Config *rest.Config
}

// Provider returns clients for an identity on a cluster.
type Provider interface {
	Info() Info
	ClientsFor(ctx context.Context, id Identity) (*Clients, error)
}

// ErrNoContext means the kubeconfig has no current context and none was given.
var ErrNoContext = errors.New("the kubeconfig has no current context; pass -context")

// LoadKubeconfig reads a kubeconfig the way kubectl does: path, then
// $KUBECONFIG, then ~/.kube/config. An empty context uses the current one.
func LoadKubeconfig(path, contextName string) (*rest.Config, Info, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path != "" {
		rules.ExplicitPath = path
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	raw, err := loader.RawConfig()
	if err != nil {
		return nil, Info{}, fmt.Errorf("read kubeconfig: %w", err)
	}
	name := contextName
	if name == "" {
		name = raw.CurrentContext
	}
	if name == "" {
		return nil, Info{}, ErrNoContext
	}
	if _, ok := raw.Contexts[name]; !ok {
		return nil, Info{}, fmt.Errorf("context %q is not in the kubeconfig", name)
	}

	cfg, err := loader.ClientConfig()
	if err != nil {
		return nil, Info{}, fmt.Errorf("build client config for context %q: %w", name, err)
	}
	cfg.UserAgent = "glowpod"
	cfg.QPS = 50
	cfg.Burst = 100
	// A context for a cluster that's gone shouldn't hang its page: give up
	// connecting after a while. Streams, once open, aren't limited.
	cfg.Dial = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext

	return cfg, Info{ID: DefaultID, Name: name, Server: serverHost(cfg.Host)}, nil
}

func serverHost(host string) string {
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return host
	}
	return u.Host
}

// Local serves local mode: every request acts as the kubeconfig's user, so
// there is exactly one identity and one set of clients.
type Local struct {
	info    Info
	clients *Clients
}

// NewLocal builds clients from cfg.
func NewLocal(cfg *rest.Config, info Info) (*Local, error) {
	c, err := newClients(cfg)
	if err != nil {
		return nil, err
	}
	return &Local{info: info, clients: c}, nil
}

// NewLocalWithClients is for tests.
func NewLocalWithClients(info Info, clients *Clients) *Local {
	return &Local{info: info, clients: clients}
}

// Info implements Provider.
func (l *Local) Info() Info { return l.info }

// ClientsFor implements Provider.
func (l *Local) ClientsFor(context.Context, Identity) (*Clients, error) {
	return l.clients, nil
}
