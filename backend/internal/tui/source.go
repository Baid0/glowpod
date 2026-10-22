package tui

import (
	"context"
	"io"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/access"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/events"
	"glowpod/internal/logs"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/pods"
	"glowpod/internal/resources"
	"glowpod/internal/scope"
	"glowpod/internal/stream"
	"glowpod/internal/summary"
	"glowpod/internal/terminal"
)

// Source is everything the terminal app reads and changes. Its methods
// return the same shapes as Glowpod's HTTP API, so signing in to an
// in-cluster Glowpod later is another implementation of this interface.
// Every call runs as the user: their kubeconfig now, their session later.
type Source interface {
	Cluster() cluster.Info
	Overview(ctx context.Context, ns scope.Namespaces) (*overview.Response, error)
	// Events are the newest events, newest first; fieldSelector narrows them.
	Events(ctx context.Context, ns scope.Namespaces, fieldSelector string, limit int) ([]summary.Event, error)
	// Namespaces lists every namespace; restricted is true when the user may
	// not list them (they can still type one).
	Namespaces(ctx context.Context) (names []string, restricted bool, err error)
	Types(ctx context.Context) ([]resources.Type, error)
	List(ctx context.Context, t resources.Type, opts resources.ListOptions) (*resources.Table, error)
	Get(ctx context.Context, t resources.Type, namespace, name string) (*resources.Object, error)
	Pod(ctx context.Context, namespace, name string) (*pods.Detail, error)
	Logs(ctx context.Context, namespace, pod string, opts logs.Options) (io.ReadCloser, error)
	// Watch calls changed whenever something in the list changes, until ctx
	// ends. WatchEvents does the same for events.
	Watch(ctx context.Context, t resources.Type, namespace, labelSelector, fieldSelector string, changed func())
	WatchEvents(ctx context.Context, namespace string, changed func())
	Allowed(ctx context.Context, checks []access.Check) ([]bool, error)
	Scale(ctx context.Context, t resources.Type, namespace, name string, replicas int64) error
	Restart(ctx context.Context, t resources.Type, namespace, name string) error
	Delete(ctx context.Context, t resources.Type, namespace, name string) error
	Shell(ctx context.Context, req terminal.Request, s terminal.Streams) error
}

// Local is the Source for local mode: the kubeconfig's user, through the
// same packages the web server uses.
type Local struct {
	info          cluster.Info
	clients       *cluster.Clients
	overview      *overview.Service
	prometheusURL string
	proms         *metrics.PromCache
	shell         terminal.Starter
}

// NewLocal returns a Source acting as clients' identity.
func NewLocal(info cluster.Info, clients *cluster.Clients, prometheusURL string) *Local {
	return &Local{
		info:          info,
		clients:       clients,
		overview:      overview.NewService(),
		prometheusURL: prometheusURL,
		proms:         metrics.NewPromCache(10 * time.Minute),
		shell:         terminal.Remote(clients.Config, clients.Kube),
	}
}

// Cluster implements Source.
func (l *Local) Cluster() cluster.Info { return l.info }

// Overview implements Source.
func (l *Local) Overview(ctx context.Context, ns scope.Namespaces) (*overview.Response, error) {
	kube := l.clients.Kube
	deps := overview.Deps{
		Kube:    kube,
		Metrics: metrics.NewReader(kube),
		Prometheus: func(ctx context.Context) (metrics.Prometheus, error) {
			if l.prometheusURL != "" {
				return &metrics.URL{Base: l.prometheusURL}, nil
			}
			return l.proms.Get(ctx, auth.LocalIdentity.Key, func(ctx context.Context) (metrics.Prometheus, error) {
				t, err := metrics.Discover(ctx, kube)
				if err != nil || t == nil {
					return nil, err
				}
				return metrics.NewServiceProxy(kube, *t), nil
			})
		},
	}
	return l.overview.Build(ctx, auth.LocalIdentity, l.info, deps, ns)
}

// Events implements Source.
func (l *Local) Events(ctx context.Context, ns scope.Namespaces, fieldSelector string, limit int) ([]summary.Event, error) {
	return events.Newest(ctx, l.clients.Kube, ns.Targets(), fieldSelector, limit)
}

// Namespaces implements Source.
func (l *Local) Namespaces(ctx context.Context) ([]string, bool, error) {
	var names []string
	opts := metav1.ListOptions{Limit: 500}
	for {
		list, err := l.clients.Kube.CoreV1().Namespaces().List(ctx, opts)
		if apierrors.IsForbidden(err) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		if list.Continue == "" {
			break
		}
		opts.Continue = list.Continue
	}
	slices.Sort(names)
	return names, false, nil
}

// Types implements Source.
func (l *Local) Types(context.Context) ([]resources.Type, error) {
	return resources.Discover(l.clients.Discovery)
}

// List implements Source.
func (l *Local) List(ctx context.Context, t resources.Type, opts resources.ListOptions) (*resources.Table, error) {
	return resources.ListTable(ctx, l.clients.Kube.CoreV1().RESTClient(), t, opts)
}

// Get implements Source. Secret values stay hidden.
func (l *Local) Get(ctx context.Context, t resources.Type, namespace, name string) (*resources.Object, error) {
	return resources.Get(ctx, l.clients.Dynamic, t, namespace, name, false, true)
}

// Pod implements Source.
func (l *Local) Pod(ctx context.Context, namespace, name string) (*pods.Detail, error) {
	return pods.Build(ctx, l.clients.Kube, metrics.NewReader(l.clients.Kube), namespace, name, time.Now())
}

// Logs implements Source.
func (l *Local) Logs(ctx context.Context, namespace, pod string, opts logs.Options) (io.ReadCloser, error) {
	return logs.Open(ctx, l.clients.Kube, namespace, pod, opts)
}

// Watch implements Source.
func (l *Local) Watch(ctx context.Context, t resources.Type, namespace, labelSelector, fieldSelector string, changed func()) {
	src := stream.ListSource(l.clients.Dynamic, t.GVR(), t.Namespaced, namespace, labelSelector, fieldSelector)
	runWatch(ctx, src, changed)
}

// WatchEvents implements Source.
func (l *Local) WatchEvents(ctx context.Context, namespace string, changed func()) {
	runWatch(ctx, stream.EventSource(l.clients.Kube, namespace), changed)
}

// runWatch reports changes and resyncs; it relists after 410 Gone (stream.Run
// does that). A forbidden watch just stops: the list still shows.
func runWatch(ctx context.Context, src stream.Source, changed func()) {
	stream.Run(ctx, src, func(m stream.Message) bool {
		switch m.Type {
		case "ready":
		case "error":
			return false
		default:
			changed()
		}
		return true
	}, stream.DefaultBackoff())
}

// Allowed implements Source.
func (l *Local) Allowed(ctx context.Context, checks []access.Check) ([]bool, error) {
	return access.Review(ctx, l.clients.Kube, checks)
}

// Scale implements Source.
func (l *Local) Scale(ctx context.Context, t resources.Type, namespace, name string, replicas int64) error {
	return resources.Scale(ctx, l.clients.Dynamic, t, namespace, name, replicas)
}

// Restart implements Source.
func (l *Local) Restart(ctx context.Context, t resources.Type, namespace, name string) error {
	return resources.Restart(ctx, l.clients.Dynamic, t, namespace, name, time.Now())
}

// Delete implements Source. Dependents (a Deployment's pods) go too.
func (l *Local) Delete(ctx context.Context, t resources.Type, namespace, name string) error {
	return resources.Delete(ctx, l.clients.Dynamic, t, namespace, name, "")
}

// Shell implements Source.
func (l *Local) Shell(ctx context.Context, req terminal.Request, s terminal.Streams) error {
	return l.shell(ctx, req, s)
}
