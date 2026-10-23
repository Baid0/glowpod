// Package search finds objects by name in one cluster, for the ⌘K palette.
// It lists metadata only (never a Secret's data), as the signed-in user, and
// keeps each person's index for a short while, so typing doesn't relist.
package search

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

// Kind is one kind of object the search looks through.
type Kind struct {
	Group      string
	Version    string
	Resource   string
	Kind       string
	Namespaced bool
	// Aliases are words that mean "this kind" in a query: "deploy web".
	Aliases []string
}

// Kinds are what the search looks through, in the order hits are shown.
var Kinds = []Kind{
	{"", "v1", "pods", "Pod", true, []string{"po", "pod", "pods"}},
	{"apps", "v1", "deployments", "Deployment", true, []string{"deploy", "deployment", "deployments"}},
	{"apps", "v1", "statefulsets", "StatefulSet", true, []string{"sts", "statefulset", "statefulsets"}},
	{"apps", "v1", "daemonsets", "DaemonSet", true, []string{"ds", "daemonset", "daemonsets"}},
	{"batch", "v1", "cronjobs", "CronJob", true, []string{"cj", "cronjob", "cronjobs"}},
	{"batch", "v1", "jobs", "Job", true, []string{"job", "jobs"}},
	{"", "v1", "services", "Service", true, []string{"svc", "service", "services"}},
	{"networking.k8s.io", "v1", "ingresses", "Ingress", true, []string{"ing", "ingress", "ingresses"}},
	{"", "v1", "configmaps", "ConfigMap", true, []string{"cm", "configmap", "configmaps"}},
	{"", "v1", "secrets", "Secret", true, []string{"secret", "secrets"}},
	{"", "v1", "persistentvolumeclaims", "PersistentVolumeClaim", true, []string{"pvc", "pvcs", "persistentvolumeclaim", "persistentvolumeclaims"}},
	{"", "v1", "namespaces", "Namespace", false, []string{"ns", "namespace", "namespaces"}},
	{"", "v1", "nodes", "Node", false, []string{"no", "node", "nodes"}},
}

// Limits on one search.
const (
	// MaxHits is how many hits one cluster returns.
	MaxHits = 20
	// MaxQuery is the longest query, in bytes.
	MaxQuery = 100
	// maxPerKind bounds the objects listed of one kind; past it the kind is
	// searched in part and the result says so.
	maxPerKind = 10_000
	page       = 500
)

// metadataAccept asks for names only: no specs, no Secret data.
const metadataAccept = "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1"

// Hit is one object whose name matched.
type Hit struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Result is one cluster's answer.
type Result struct {
	Hits []Hit `json:"hits"`
	// More is set when more objects matched than were returned.
	More bool `json:"more,omitempty"`
	// Skipped lists the kinds (resources) that weren't searched in full:
	// the person may not list them, or there were too many.
	Skipped []string `json:"skipped,omitempty"`
	// Denied is set when the person may list none of the kinds in all
	// namespaces, and no namespace to look in instead was given.
	Denied bool `json:"denied,omitempty"`
}

type entry struct {
	kind      int
	namespace string
	name      string
}

// index is the names of everything one person may list in one cluster.
type index struct {
	entries []entry
	skipped []string
	denied  bool
}

// Lister lists one kind's names in a namespace ("" for all of them).
type Lister func(ctx context.Context, k Kind, namespace, cont string) (names []entry, next string, err error)

// RESTLister lists names through client, a REST client for the cluster
// (any group; paths are absolute).
func RESTLister(client rest.Interface) Lister {
	return func(ctx context.Context, k Kind, namespace, cont string) ([]entry, string, error) {
		path := "/apis/" + k.Group + "/" + k.Version
		if k.Group == "" {
			path = "/api/" + k.Version
		}
		if k.Namespaced && namespace != "" {
			path += "/namespaces/" + namespace
		}
		req := client.Get().AbsPath(path+"/"+k.Resource).
			SetHeader("Accept", metadataAccept).
			Param("limit", fmt.Sprint(page))
		if cont != "" {
			req = req.Param("continue", cont)
		}
		raw, err := req.DoRaw(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("list %s: %w", k.Resource, err)
		}
		var list struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []struct {
				Metadata struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, "", fmt.Errorf("decode %s: %w", k.Resource, err)
		}
		if list.Kind != "PartialObjectMetadataList" {
			return nil, "", fmt.Errorf("list %s: the API server didn't return metadata only (kind %q)", k.Resource, list.Kind)
		}
		out := make([]entry, 0, len(list.Items))
		for _, it := range list.Items {
			out = append(out, entry{namespace: it.Metadata.Namespace, name: it.Metadata.Name})
		}
		return out, list.Metadata.Continue, nil
	}
}

// build lists every kind. Namespaced kinds the person may not list in all
// namespaces are listed in fallback instead (the ones they picked, or their
// service account's), and kinds they can't list at all are skipped.
func build(ctx context.Context, list Lister, fallback []string) (*index, error) {
	var (
		mu        sync.Mutex
		idx       = &index{}
		g         errgroup.Group
		forbidden int
	)
	g.SetLimit(4)
	for i, k := range Kinds {
		g.Go(func() error {
			names, complete, err := listKind(ctx, list, k, fallback)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case apierrors.IsForbidden(err):
				forbidden++
				idx.skipped = append(idx.skipped, k.Resource)
				return nil
			case apierrors.IsNotFound(err):
				// Not served by this cluster.
				idx.skipped = append(idx.skipped, k.Resource)
				return nil
			case err != nil:
				return err
			}
			if !complete {
				idx.skipped = append(idx.skipped, k.Resource)
			}
			for _, e := range names {
				e.kind = i
				idx.entries = append(idx.entries, e)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err //nolint:wrapcheck // the lister says what it listed
	}
	slices.Sort(idx.skipped)
	idx.denied = forbidden == len(Kinds)
	return idx, nil
}

// listKind lists one kind in all namespaces, or in fallback when that's
// forbidden. complete is false when it stopped at maxPerKind or some
// fallback namespace was forbidden.
func listKind(ctx context.Context, list Lister, k Kind, fallback []string) (names []entry, complete bool, err error) {
	names, complete, err = listAll(ctx, list, k, "")
	if !apierrors.IsForbidden(err) || !k.Namespaced || len(fallback) == 0 {
		return names, complete, err
	}
	names, complete = nil, true
	var denied error
	for _, ns := range fallback {
		got, done, err := listAll(ctx, list, k, ns)
		switch {
		case apierrors.IsForbidden(err):
			denied, complete = err, false
			continue
		case err != nil:
			return nil, false, err
		}
		names = append(names, got...)
		complete = complete && done
	}
	if names == nil && denied != nil {
		return nil, false, denied
	}
	return names, complete, nil
}

func listAll(ctx context.Context, list Lister, k Kind, namespace string) ([]entry, bool, error) {
	var out []entry
	cont := ""
	for {
		names, next, err := list(ctx, k, namespace, cont)
		if err != nil {
			return nil, false, err
		}
		out = append(out, names...)
		if next == "" {
			return out, true, nil
		}
		if len(out) >= maxPerKind {
			return out, false, nil
		}
		cont = next
	}
}

// match finds query's hits in idx. Every word must appear in the name or
// the namespace, or name the kind ("deploy", "svc"). Exact names come
// first, then names that start with the query, then the rest; within each,
// shorter (closer) names first, then by kind.
func (idx *index) match(query string) Result {
	words := strings.Fields(strings.ToLower(query))
	res := Result{Hits: []Hit{}, Skipped: idx.skipped, Denied: idx.denied}
	if len(words) == 0 {
		return res
	}
	type scored struct {
		e     entry
		score int
	}
	var found []scored
	for _, e := range idx.entries {
		name := strings.ToLower(e.name)
		ns := strings.ToLower(e.namespace)
		var inName []string
		ok := true
		for _, w := range words {
			switch {
			case strings.Contains(name, w):
				inName = append(inName, w)
			case strings.Contains(ns, w), slices.Contains(Kinds[e.kind].Aliases, w):
			default:
				ok = false
			}
			if !ok {
				break
			}
		}
		if !ok {
			continue
		}
		score := 3
		switch {
		case len(inName) == 0:
		case name == strings.Join(inName, " "):
			score = 0
		case strings.HasPrefix(name, inName[0]):
			score = 1
		default:
			score = 2
		}
		found = append(found, scored{e, score})
	}
	slices.SortFunc(found, func(a, b scored) int {
		return cmp.Or(
			cmp.Compare(a.score, b.score),
			cmp.Compare(len(a.e.name), len(b.e.name)),
			cmp.Compare(a.e.kind, b.e.kind),
			strings.Compare(a.e.name, b.e.name),
			strings.Compare(a.e.namespace, b.e.namespace),
		)
	})
	if len(found) > MaxHits {
		found, res.More = found[:MaxHits], true
	}
	for _, f := range found {
		k := Kinds[f.e.kind]
		res.Hits = append(res.Hits, Hit{
			Group: k.Group, Version: k.Version, Resource: k.Resource, Kind: k.Kind,
			Namespace: f.e.namespace, Name: f.e.name,
		})
	}
	return res
}

// Cache keeps each person's index per cluster for TTL. Keys must name the
// cluster and the identity: one person's index is never another's.
type Cache struct {
	TTL time.Duration
	now func() time.Time

	mu    sync.Mutex
	slots map[string]*slot
}

type slot struct {
	done  chan struct{}
	idx   *index
	err   error
	built time.Time
}

const (
	// maxSlots bounds the cache; the oldest index goes first.
	maxSlots = 256
	// buildTimeout bounds listing one cluster.
	buildTimeout = 30 * time.Second
)

// NewCache returns a cache that keeps an index for ttl.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{TTL: ttl, now: time.Now, slots: map[string]*slot{}}
}

// Search answers query from key's index, building it with list first when
// it's missing or older than TTL. Concurrent searches share one build.
// Errors aren't kept.
func (c *Cache) Search(ctx context.Context, key, query string, list Lister, fallback []string) (Result, error) {
	c.mu.Lock()
	s, ok := c.slots[key]
	if !ok || (s.idx != nil && c.now().Sub(s.built) >= c.TTL) {
		s = &slot{done: make(chan struct{})}
		c.evict()
		c.slots[key] = s
		// The build outlives the request that started it: typing cancels
		// each request, and the next one should find the index ready.
		go c.build(context.WithoutCancel(ctx), key, s, list, fallback)
	}
	c.mu.Unlock()
	select {
	case <-s.done:
	case <-ctx.Done():
		return Result{}, fmt.Errorf("search: %w", ctx.Err())
	}
	if s.err != nil {
		return Result{}, s.err
	}
	return s.idx.match(query), nil
}

func (c *Cache) build(ctx context.Context, key string, s *slot, list Lister, fallback []string) {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	idx, err := build(ctx, list, fallback)
	c.mu.Lock()
	s.idx, s.err, s.built = idx, err, c.now()
	if err != nil && c.slots[key] == s {
		delete(c.slots, key)
	}
	c.mu.Unlock()
	close(s.done)
}

// evict makes room for one more slot. Call with c.mu held.
func (c *Cache) evict() {
	now := c.now()
	for k, s := range c.slots {
		if s.idx != nil && now.Sub(s.built) >= c.TTL {
			delete(c.slots, k)
		}
	}
	for len(c.slots) >= maxSlots {
		var oldest string
		for k, s := range c.slots {
			if s.idx != nil && (oldest == "" || s.built.Before(c.slots[oldest].built)) {
				oldest = k
			}
		}
		if oldest == "" {
			return // every slot is still building
		}
		delete(c.slots, oldest)
	}
}
