// Package cluster connects to Kubernetes and hands out clients that act as
// the signed-in identity.
//
// v1 serves one cluster, always addressed as DefaultID, so routes and caches
// already carry a cluster id when more are added.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// DefaultID is the id of the only cluster in v1.
const DefaultID = "default"

// Info describes a cluster connection. It never holds credentials.
type Info struct {
	ID string `json:"id"`
	// Name is the kubeconfig context name, shown in the cluster picker.
	Name string `json:"name"`
	// Server is the API server host, without credentials or path.
	Server string `json:"server"`
}

// Identity is who a request acts as. Key separates per-user state such as
// caches and metric history; it is never a credential.
type Identity struct {
	Key string
}

// Clients are Kubernetes clients bound to one identity.
type Clients struct {
	Kube kubernetes.Interface
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
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return &Local{info: info, clients: &Clients{Kube: kube}}, nil
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
