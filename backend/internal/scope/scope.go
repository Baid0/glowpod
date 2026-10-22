// Package scope is which namespaces a request covers: all of them, one, or
// a few picked together.
package scope

import (
	"errors"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// Max is how many namespaces one request may name.
const Max = 20

// Namespaces lists the namespaces a request covers; empty means all.
type Namespaces []string

// ErrInvalid means ?namespace= isn't a valid name or list of names.
var ErrInvalid = errors.New("invalid namespace list")

// Parse reads ?namespace=: "" for all, "a", or "a,b,c". Names are checked,
// sorted and deduplicated.
func Parse(raw string) (Namespaces, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > Max {
		return nil, ErrInvalid
	}
	out := make(Namespaces, 0, len(parts))
	for _, p := range parts {
		if len(validation.IsDNS1123Label(p)) > 0 {
			return nil, ErrInvalid
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// One covers a single namespace ("" = all).
func One(ns string) Namespaces {
	if ns == "" {
		return nil
	}
	return Namespaces{ns}
}

// All reports whether every namespace is covered.
func (n Namespaces) All() bool { return len(n) == 0 }

// Contains reports whether ns is covered.
func (n Namespaces) Contains(ns string) bool { return n.All() || slices.Contains(n, ns) }

// Targets are the namespaces to make list calls in: "" (all) or each name.
func (n Namespaces) Targets() []string {
	if n.All() {
		return []string{""}
	}
	return n
}

// Single is the one namespace covered, if exactly one is.
func (n Namespaces) Single() (string, bool) {
	if len(n) == 1 {
		return n[0], true
	}
	return "", false
}

// String is the ?namespace= form.
func (n Namespaces) String() string { return strings.Join(n, ",") }
