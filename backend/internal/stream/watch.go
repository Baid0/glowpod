// Package stream pushes live pod and event changes to the browser over a
// WebSocket. Each connection runs its own watches with the caller's
// clients, so nothing is shared between users.
package stream

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/kerr"
	"glowpod/internal/summary"
)

// Message is one WebSocket frame, sent as JSON.
type Message struct {
	// Type is ready, pod, event, changed, resync or error.
	Type string `json:"type"`
	// Op is added, modified or deleted.
	Op string `json:"op,omitempty"`
	// Resource names the watch a resync or error is about: pods or events.
	Resource string         `json:"resource,omitempty"`
	Pod      *summary.Pod   `json:"pod,omitempty"`
	Event    *summary.Event `json:"event,omitempty"`
	// Ref names the object a "changed" message is about.
	Ref     *summary.ObjectRef `json:"ref,omitempty"`
	Code    string             `json:"code,omitempty"`
	Message string             `json:"message,omitempty"`
}

// Source is one resource to watch.
type Source struct {
	Name string
	// List returns the current resourceVersion to watch from.
	List  func(ctx context.Context) (string, error)
	Watch func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	// Convert turns a watch event into a message; false skips it.
	Convert func(op watch.EventType, obj runtime.Object) (Message, bool)
}

// watchTimeout makes the API server end each watch so it's renewed regularly.
const watchTimeout int64 = 5 * 60

// Backoff waits between retries after errors.
type Backoff struct {
	Initial, Max time.Duration
	next         time.Duration
}

func (b *Backoff) wait(ctx context.Context) bool {
	if b.next == 0 {
		b.next = b.Initial
	}
	t := time.NewTimer(b.next)
	defer t.Stop()
	b.next = min(b.next*2, b.Max)
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (b *Backoff) reset() { b.next = 0 }

// DefaultBackoff starts at half a second and caps at 30 seconds.
func DefaultBackoff() Backoff { return Backoff{Initial: 500 * time.Millisecond, Max: 30 * time.Second} }

// Run watches src, calling send for each change, until ctx ends or send
// returns false. When the API server says the resourceVersion is too old
// (410 Gone) it lists again, sends a resync message and resumes. A list that
// is forbidden sends an error message and stops.
func Run(ctx context.Context, src Source, send func(Message) bool, backoff Backoff) {
	relist := func(resync bool) (string, bool) {
		for {
			rv, err := src.List(ctx)
			if err == nil {
				backoff.reset()
				if resync && !send(Message{Type: "resync", Resource: src.Name}) {
					return "", false
				}
				return rv, true
			}
			if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
				c := kerr.Classify(err)
				send(Message{Type: "error", Resource: src.Name, Code: c.Code, Message: c.Message})
				return "", false
			}
			if !backoff.wait(ctx) {
				return "", false
			}
		}
	}

	rv, ok := relist(false)
	for ok && ctx.Err() == nil {
		w, err := src.Watch(ctx, metav1.ListOptions{
			ResourceVersion:     rv,
			AllowWatchBookmarks: true,
			TimeoutSeconds:      ptr(watchTimeout),
		})
		if err != nil {
			if isGone(err) {
				rv, ok = relist(true)
			} else {
				ok = backoff.wait(ctx)
			}
			continue
		}
		rv, ok = drain(ctx, w, src, rv, send)
		if ok && rv == "" {
			rv, ok = relist(true)
		}
	}
}

// drain forwards events from one watch. It returns the last resourceVersion,
// or "" when the watch reported 410 Gone and a relist is needed.
func drain(ctx context.Context, w watch.Interface, src Source, rv string, send func(Message) bool) (string, bool) {
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return rv, false
		case ev, open := <-w.ResultChan():
			if !open {
				return rv, true // timed out or closed: re-watch from rv
			}
			switch ev.Type {
			case watch.Error:
				if isGone(apierrors.FromObject(ev.Object)) {
					return "", true
				}
				return rv, true
			case watch.Bookmark:
				rv = resourceVersion(ev.Object, rv)
			default:
				rv = resourceVersion(ev.Object, rv)
				if msg, ok := src.Convert(ev.Type, ev.Object); ok && !send(msg) {
					return rv, false
				}
			}
		}
	}
}

func isGone(err error) bool {
	return apierrors.IsResourceExpired(err) || apierrors.IsGone(err)
}

func resourceVersion(obj runtime.Object, fallback string) string {
	if m, err := meta.Accessor(obj); err == nil && m.GetResourceVersion() != "" {
		return m.GetResourceVersion()
	}
	return fallback
}

func ptr[T any](v T) *T { return &v }

func op(t watch.EventType) string {
	switch t {
	case watch.Added:
		return "added"
	case watch.Deleted:
		return "deleted"
	default:
		return "modified"
	}
}

// listVersion lists one item to learn the current resourceVersion cheaply.
func listVersion(list func(context.Context, metav1.ListOptions) (string, error)) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return list(ctx, metav1.ListOptions{Limit: 1})
	}
}

// PodSource watches pods in namespace ("" = all).
func PodSource(kube kubernetes.Interface, namespace string, now func() time.Time) Source {
	pods := kube.CoreV1().Pods(namespace)
	return Source{
		Name: "pods",
		List: listVersion(func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := pods.List(ctx, o)
			if err != nil {
				return "", err //nolint:wrapcheck // classified by Run
			}
			return l.ResourceVersion, nil
		}),
		Watch: pods.Watch,
		Convert: func(t watch.EventType, obj runtime.Object) (Message, bool) {
			p, ok := obj.(*corev1.Pod)
			if !ok {
				return Message{}, false
			}
			s := summary.PodOf(p, now())
			return Message{Type: "pod", Op: op(t), Pod: &s}, true
		},
	}
}

// EventSource watches core/v1 events in namespace ("" = all). Deletions are
// skipped: events simply expire.
func EventSource(kube kubernetes.Interface, namespace string) Source {
	events := kube.CoreV1().Events(namespace)
	return Source{
		Name: "events",
		List: listVersion(func(ctx context.Context, o metav1.ListOptions) (string, error) {
			l, err := events.List(ctx, o)
			if err != nil {
				return "", err //nolint:wrapcheck // classified by Run
			}
			return l.ResourceVersion, nil
		}),
		Watch: events.Watch,
		Convert: func(t watch.EventType, obj runtime.Object) (Message, bool) {
			e, ok := obj.(*corev1.Event)
			if !ok || t == watch.Deleted {
				return Message{}, false
			}
			s := summary.EventOf(e)
			return Message{Type: "event", Op: op(t), Event: &s}, true
		},
	}
}
