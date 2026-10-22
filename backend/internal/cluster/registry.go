package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
)

// Registry is every cluster one Glowpod serves, in the order they're listed.
type Registry struct {
	list []Provider
	byID map[string]Provider
}

// NewRegistry lists providers in order. Ids must be unique.
func NewRegistry(providers ...Provider) (*Registry, error) {
	r := &Registry{byID: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		id := p.Info().ID
		if id == "" {
			return nil, fmt.Errorf("cluster %q has no id", p.Info().Name)
		}
		if _, dup := r.byID[id]; dup {
			return nil, fmt.Errorf("two clusters share the id %q", id)
		}
		r.byID[id] = p
		r.list = append(r.list, p)
	}
	return r, nil
}

// Single is a registry of one cluster.
func Single(p Provider) *Registry {
	return &Registry{list: []Provider{p}, byID: map[string]Provider{p.Info().ID: p}}
}

// List describes every cluster, in order.
func (r *Registry) List() []Info {
	out := make([]Info, len(r.list))
	for i, p := range r.list {
		out[i] = p.Info()
	}
	return out
}

// Get finds a cluster by id.
func (r *Registry) Get(id string) (Provider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// Current is the cluster to open first: the one marked Current, else the first.
func (r *Registry) Current() Provider {
	for _, p := range r.list {
		if p.Info().Current {
			return p
		}
	}
	if len(r.list) == 0 {
		return nil
	}
	return r.list[0]
}

// Contexts returns the kubeconfig's context names, sorted, and its current
// context. path is resolved like LoadKubeconfig.
func Contexts(path string) (names []string, current string, err error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path != "" {
		rules.ExplicitPath = path
	}
	raw, err := rules.Load()
	if err != nil {
		return nil, "", fmt.Errorf("read kubeconfig: %w", err)
	}
	for name := range raw.Contexts {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, raw.CurrentContext, nil
}

// maxIDLength keeps ids short enough for a URL segment that people read.
const maxIDLength = 40

// ContextIDs turns kubeconfig context names into ids for URLs: lowercase
// letters, digits and dashes ("kind-lab", "gke-shop-europe-west1-prod").
// Names that come out empty, too long or the same as another get a short
// hash of the full name, so every id is unique and stable.
func ContextIDs(names []string) map[string]string {
	ids := make(map[string]string, len(names))
	count := map[string]int{}
	slugs := make(map[string]string, len(names))
	for _, n := range names {
		s := slug(n)
		slugs[n] = s
		count[s]++
	}
	for _, n := range names {
		s := slugs[n]
		if s == "" || count[s] > 1 || len(s) > maxIDLength {
			sum := sha256.Sum256([]byte(n))
			s = strings.Trim(s[:min(len(s), maxIDLength-7)], "-")
			if s != "" {
				s += "-"
			}
			s += hex.EncodeToString(sum[:3])
		}
		ids[n] = s
	}
	return ids
}

func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// Unavailable is a cluster that's listed but can't be reached, such as a
// kubeconfig context whose client config doesn't build. Every request gets
// its error.
type Unavailable struct {
	info Info
	err  error
}

// NewUnavailable lists info and answers every request with err.
func NewUnavailable(info Info, err error) *Unavailable { return &Unavailable{info: info, err: err} }

// Info implements Provider.
func (u *Unavailable) Info() Info { return u.info }

// ClientsFor implements Provider.
func (u *Unavailable) ClientsFor(context.Context, Identity) (*Clients, error) { return nil, u.err }

// LoadContexts lists every context in the kubeconfig as a cluster, in
// local mode. current names the one to open first; empty uses the
// kubeconfig's current context. A context whose client config doesn't
// build (a missing certificate file, say) is still listed, and says why.
func LoadContexts(path, current string) (*Registry, error) {
	names, kubeCurrent, err := Contexts(path)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, ErrNoContexts
	}
	if current == "" {
		current = kubeCurrent
	} else if !slices.Contains(names, current) {
		return nil, fmt.Errorf("context %q is not in the kubeconfig", current)
	}
	ids := ContextIDs(names)
	providers := make([]Provider, 0, len(names))
	for _, name := range names {
		cfg, info, err := LoadKubeconfig(path, name)
		info.ID, info.Name, info.Current = ids[name], name, name == current
		if err != nil {
			providers = append(providers, NewUnavailable(info, err))
			continue
		}
		p, err := NewLocal(cfg, info)
		if err != nil {
			providers = append(providers, NewUnavailable(info, err))
			continue
		}
		providers = append(providers, p)
	}
	return NewRegistry(providers...)
}

// ErrNoContexts means the kubeconfig has no contexts at all.
var ErrNoContexts = errors.New("the kubeconfig has no contexts; add a cluster with kubectl first")
