// Package resources lists and reads any Kubernetes resource, CRDs included,
// through discovery, the API server's Table output and the dynamic client.
package resources

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// CoreGroup stands in for the empty core group in URLs.
const CoreGroup = "core"

// Type is one listable resource type at its preferred version.
type Type struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	ShortNames []string `json:"shortNames,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// GVR returns the dynamic-client address of t.
func (t Type) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: t.Group, Version: t.Version, Resource: t.Resource}
}

// Can reports whether the API serves verb for t.
func (t Type) Can(verb string) bool { return slices.Contains(t.Verbs, verb) }

// URLGroup is the group as it appears in Glowpod URLs ("core" for "").
func (t Type) URLGroup() string {
	if t.Group == "" {
		return CoreGroup
	}
	return t.Group
}

// Discover returns every listable resource type at its group's preferred
// version, sorted by group then resource. Groups whose discovery failed
// (often an aggregated API that's down) are skipped, not fatal.
func Discover(d discovery.DiscoveryInterface) ([]Type, error) {
	groups, lists, err := d.ServerGroupsAndResources()
	var partial *discovery.ErrGroupDiscoveryFailed
	if err != nil && !errors.As(err, &partial) {
		return nil, fmt.Errorf("discover API resources: %w", err)
	}

	preferred := map[string]string{}
	for _, g := range groups {
		if g.PreferredVersion.Version != "" {
			preferred[g.Name] = g.PreferredVersion.Version
		}
	}

	seen := map[schema.GroupResource]bool{}
	var out []Type
	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		if v, ok := preferred[gv.Group]; ok && v != gv.Version {
			continue
		}
		for _, r := range list.APIResources {
			gr := schema.GroupResource{Group: gv.Group, Resource: r.Name}
			if strings.Contains(r.Name, "/") || seen[gr] || !slices.Contains(r.Verbs, "list") {
				continue
			}
			seen[gr] = true
			out = append(out, typeOf(gv, r))
		}
	}
	slices.SortFunc(out, func(a, b Type) int {
		if c := cmp.Compare(a.Group, b.Group); c != 0 {
			return c
		}
		return cmp.Compare(a.Resource, b.Resource)
	})
	return out, nil
}

func typeOf(gv schema.GroupVersion, r metav1.APIResource) Type {
	return Type{
		Group:      gv.Group,
		Version:    gv.Version,
		Resource:   r.Name,
		Kind:       r.Kind,
		Namespaced: r.Namespaced,
		Verbs:      r.Verbs,
		ShortNames: r.ShortNames,
		Categories: r.Categories,
	}
}

// Find looks up group (URL form, "core" for the core group), version and resource.
func Find(types []Type, group, version, resource string) (Type, bool) {
	if group == CoreGroup {
		group = ""
	}
	for _, t := range types {
		if t.Group == group && t.Version == version && t.Resource == resource {
			return t, true
		}
	}
	return Type{}, false
}

// Resolve finds a type in the identity's cached discovery, refreshing the
// cache once on a miss so a newly installed CRD shows up.
func Resolve(d discovery.CachedDiscoveryInterface, group, version, resource string) (Type, error) {
	for attempt := 0; attempt < 2; attempt++ {
		types, err := Discover(d)
		if err != nil {
			return Type{}, err
		}
		if t, ok := Find(types, group, version, resource); ok {
			return t, nil
		}
		d.Invalidate()
	}
	return Type{}, ErrUnknownType
}

// ErrUnknownType means the cluster doesn't serve that group/version/resource.
var ErrUnknownType = errors.New("unknown resource type")
