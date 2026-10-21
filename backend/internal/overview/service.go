package overview

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/cluster"
	"glowpod/internal/kerr"
	"glowpod/internal/metrics"
	"glowpod/internal/summary"
)

const (
	pageSize        = 500
	maxProblems     = 50
	maxApps         = 50
	topN            = 5
	jobWindow       = 24 * time.Hour
	defaultHigh     = 0.70
	defaultCritical = 0.85
)

// MetricsSource reads usage. *metrics.Reader implements it.
type MetricsSource interface {
	NodeUsage(ctx context.Context) (map[string]metrics.Usage, error)
	PodUsage(ctx context.Context, namespace string) ([]metrics.PodUsage, error)
	VolumeUsage(ctx context.Context, nodes []string) (map[metrics.PVCKey]float64, error)
}

// Deps are the identity-bound clients one Build uses.
type Deps struct {
	Kube    kubernetes.Interface
	Metrics MetricsSource
	// Prometheus returns the Prometheus to query, or nil when there is none.
	Prometheus func(ctx context.Context) (metrics.Prometheus, error)
}

// Service builds Overview responses and keeps per-identity trend history.
type Service struct {
	now        func() time.Time
	thresholds [2]float64

	mu        sync.Mutex
	histories map[string]*metrics.History
}

// NewService returns a Service with the default 70% / 85% thresholds.
func NewService() *Service {
	return &Service{
		now:        time.Now,
		thresholds: [2]float64{defaultHigh, defaultCritical},
		histories:  map[string]*metrics.History{},
	}
}

func (s *Service) history(id cluster.Identity) *metrics.History {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.histories[id.Key]
	if !ok {
		h = metrics.NewHistory()
		s.histories[id.Key] = h
	}
	return h
}

// RequiredError wraps a failure to load data the page can't do without
// (nodes or pods); the API maps its cause to a status code.
type RequiredError struct {
	What string
	Err  error
}

func (e *RequiredError) Error() string { return fmt.Sprintf("list %s: %v", e.What, e.Err) }
func (e *RequiredError) Unwrap() error { return e.Err }

// snapshot is everything Build reads from the cluster.
type snapshot struct {
	nodes        []corev1.Node
	pods         []corev1.Pod
	version      string
	namespaces   int
	deployments  []appsv1.Deployment
	statefulSets []appsv1.StatefulSet
	daemonSets   []appsv1.DaemonSet
	replicaSets  []appsv1.ReplicaSet
	jobs         []batchv1.Job
	pvcs         []corev1.PersistentVolumeClaim
	nodeUsage    map[string]metrics.Usage
	nodeUsageErr error
	podUsage     []metrics.PodUsage
	podUsageErr  error
	volumeUsage  map[metrics.PVCKey]float64
	volumeErr    error
	prom         metrics.Prometheus
	promErr      error
	errs         map[string]error
}

// Build loads the cluster state and assembles the Overview for namespace
// ("" = all namespaces).
func (s *Service) Build(ctx context.Context, id cluster.Identity, info cluster.Info, deps Deps, namespace string) (*Response, error) {
	snap, err := load(ctx, deps, namespace)
	if err != nil {
		return nil, err
	}
	now := s.now()
	resp := &Response{
		Cluster: ClusterInfo{
			Info:       info,
			Version:    snap.version,
			Nodes:      len(snap.nodes),
			Namespaces: snap.namespaces,
		},
		Namespace:   namespace,
		GeneratedAt: now.UTC(),
		Thresholds:  s.thresholds,
	}

	resp.Gauges = s.gauges(ctx, id, snap, now)

	pods := inNamespace(snap.pods, namespace)
	for i := range pods {
		switch summary.HealthOf(&pods[i], now) {
		case summary.Running:
			resp.Health.Running++
		case summary.Pending:
			resp.Health.Pending++
		case summary.Failing:
			resp.Health.Failing++
		}
	}

	failing := failingOwners(pods, snap.replicaSets, now)
	resp.Workloads = workloads(snap, failing, now)
	resp.Problems, resp.ProblemCount = problems(snap, pods, namespace, now)
	resp.Nodes = nodeLoad(snap, s.thresholds[1])
	resp.TopConsumers = topConsumers(snap.podUsage)
	resp.Apps = apps(snap, failing)
	resp.RecentDeploys = recentDeploys(snap.replicaSets)

	for _, section := range []string{"namespaces", "workloads", "jobs", "replicaSets", "storage", "version"} {
		if err := snap.errs[section]; err != nil {
			resp.Unavailable = append(resp.Unavailable, Unavailable{Section: section, Message: describe(err)})
		}
	}
	if snap.podUsageErr != nil {
		resp.Unavailable = append(resp.Unavailable, Unavailable{Section: "topConsumers", Message: describe(snap.podUsageErr)})
	}
	return resp, nil
}

func (s *Service) gauges(ctx context.Context, id cluster.Identity, snap *snapshot, now time.Time) Gauges {
	var g Gauges
	g.CPU, g.Memory = metrics.CPUAndMemory(snap.nodes, snap.pods, snap.nodeUsage, snap.nodeUsageErr)
	g.Pods = metrics.Pods(snap.nodes, snap.pods)
	g.Storage = metrics.Storage(snap.pvcs, snap.volumeUsage, snap.volumeErr)
	if snap.errs["storage"] != nil {
		g.Storage = metrics.Gauge{State: metrics.Failed, Unit: "bytes", Message: "Couldn't list volumes."}
	}

	switch {
	case snap.promErr != nil:
		g.Network = &metrics.Gauge{State: metrics.Failed, Unit: "bytes/s", Source: "prometheus", Message: "Couldn't look for Prometheus."}
	case snap.prom != nil:
		n := metrics.Network(ctx, snap.prom)
		g.Network = &n
	}

	h := s.history(id)
	h.Apply("cpu", &g.CPU, now)
	h.Apply("memory", &g.Memory, now)
	h.Apply("pods", &g.Pods, now)
	h.Apply("storage", &g.Storage, now)
	if g.Network != nil {
		h.Apply("network", g.Network, now)
	}
	return g
}

func load(ctx context.Context, deps Deps, namespace string) (*snapshot, error) {
	snap := &snapshot{errs: map[string]error{}}
	kube := deps.Kube
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		nodeErr error
		podErr  error
	)
	optional := func(section string, f func() error) {
		wg.Go(func() {
			if err := f(); err != nil {
				mu.Lock()
				snap.errs[section] = err
				mu.Unlock()
			}
		})
	}

	wg.Go(func() {
		snap.nodes, nodeErr = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Node, string, error) {
			l, err := kube.CoreV1().Nodes().List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
	})
	wg.Go(func() {
		// All namespaces: the gauges are cluster-wide.
		snap.pods, podErr = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Pod, string, error) {
			l, err := kube.CoreV1().Pods("").List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
	})
	optional("version", func() error {
		v, err := kube.Discovery().ServerVersion()
		if err != nil {
			return err
		}
		snap.version = v.GitVersion
		return nil
	})
	optional("namespaces", func() error {
		l, err := listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Namespace, string, error) {
			l, err := kube.CoreV1().Namespaces().List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		snap.namespaces = len(l)
		return err
	})
	optional("workloads", func() error {
		var err error
		snap.deployments, err = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.Deployment, string, error) {
			l, err := kube.AppsV1().Deployments(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		if err != nil {
			return err
		}
		snap.statefulSets, err = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
			l, err := kube.AppsV1().StatefulSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		if err != nil {
			return err
		}
		snap.daemonSets, err = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
			l, err := kube.AppsV1().DaemonSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	optional("replicaSets", func() error {
		var err error
		snap.replicaSets, err = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.ReplicaSet, string, error) {
			l, err := kube.AppsV1().ReplicaSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	optional("jobs", func() error {
		var err error
		snap.jobs, err = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]batchv1.Job, string, error) {
			l, err := kube.BatchV1().Jobs(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	optional("storage", func() error {
		var err error
		snap.pvcs, err = listAll(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.PersistentVolumeClaim, string, error) {
			l, err := kube.CoreV1().PersistentVolumeClaims("").List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	wg.Go(func() { snap.nodeUsage, snap.nodeUsageErr = deps.Metrics.NodeUsage(ctx) })
	wg.Go(func() { snap.podUsage, snap.podUsageErr = deps.Metrics.PodUsage(ctx, namespace) })
	if deps.Prometheus != nil {
		wg.Go(func() { snap.prom, snap.promErr = deps.Prometheus(ctx) })
	}
	wg.Wait()

	if nodeErr != nil {
		return nil, &RequiredError{What: "nodes", Err: nodeErr}
	}
	if podErr != nil {
		return nil, &RequiredError{What: "pods", Err: podErr}
	}

	// Kubelet stats need the node names; skip them when there are no volumes.
	if hasBound(snap.pvcs) {
		names := make([]string, len(snap.nodes))
		for i := range snap.nodes {
			names[i] = snap.nodes[i].Name
		}
		snap.volumeUsage, snap.volumeErr = deps.Metrics.VolumeUsage(ctx, names)
	}
	return snap, nil
}

// listAll pages through a list with limit/continue.
func listAll[T any](ctx context.Context, list func(context.Context, metav1.ListOptions) ([]T, string, error)) ([]T, error) {
	var out []T
	opts := metav1.ListOptions{Limit: pageSize}
	for {
		items, next, err := list(ctx, opts)
		if err != nil {
			return nil, err //nolint:wrapcheck // callers wrap with what they listed
		}
		out = append(out, items...)
		if next == "" {
			return out, nil
		}
		opts.Continue = next
	}
}

func hasBound(pvcs []corev1.PersistentVolumeClaim) bool {
	return slices.ContainsFunc(pvcs, func(p corev1.PersistentVolumeClaim) bool {
		return p.Status.Phase == corev1.ClaimBound
	})
}

func inNamespace(pods []corev1.Pod, namespace string) []corev1.Pod {
	if namespace == "" {
		return pods
	}
	out := make([]corev1.Pod, 0, len(pods))
	for i := range pods {
		if pods[i].Namespace == namespace {
			out = append(out, pods[i])
		}
	}
	return out
}

func problems(snap *snapshot, pods []corev1.Pod, namespace string, now time.Time) ([]summary.Problem, int) {
	var out []summary.Problem
	for i := range pods {
		if p, ok := summary.PodProblem(&pods[i], now); ok {
			out = append(out, p)
		}
	}
	if namespace == "" {
		for i := range snap.nodes {
			out = append(out, summary.NodeProblems(&snap.nodes[i])...)
		}
	}
	for i := range snap.jobs {
		if !withinJobWindow(&snap.jobs[i], now) {
			continue
		}
		if p, ok := summary.JobProblem(&snap.jobs[i]); ok {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b summary.Problem) int {
		if a.Severity != b.Severity {
			if a.Severity == summary.Critical {
				return -1
			}
			return 1
		}
		return b.Since.Compare(a.Since)
	})
	total := len(out)
	if total > maxProblems {
		out = out[:maxProblems]
	}
	if out == nil {
		out = []summary.Problem{}
	}
	return out, total
}

func nodeLoad(snap *snapshot, critical float64) []NodeLoad {
	out := make([]NodeLoad, 0, len(snap.nodes))
	for i := range snap.nodes {
		n := &snap.nodes[i]
		load := NodeLoad{Name: n.Name}
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady {
				load.Ready = c.Status == corev1.ConditionTrue
			}
		}
		if u, ok := snap.nodeUsage[n.Name]; ok && snap.nodeUsageErr == nil {
			if alloc := n.Status.Allocatable.Cpu().AsApproximateFloat64(); alloc > 0 {
				f := u.CPU / alloc
				load.CPU = &f
			}
			if alloc := n.Status.Allocatable.Memory().AsApproximateFloat64(); alloc > 0 {
				f := u.Memory / alloc
				load.Memory = &f
			}
		}
		load.Hot = (load.CPU != nil && *load.CPU >= critical) || (load.Memory != nil && *load.Memory >= critical)
		out = append(out, load)
	}
	slices.SortFunc(out, func(a, b NodeLoad) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func topConsumers(usage []metrics.PodUsage) []Consumer {
	sorted := slices.Clone(usage)
	slices.SortFunc(sorted, func(a, b metrics.PodUsage) int {
		if c := cmp.Compare(b.CPU, a.CPU); c != 0 {
			return c
		}
		return cmp.Compare(b.Memory, a.Memory)
	})
	out := make([]Consumer, 0, topN)
	for _, u := range sorted[:min(topN, len(sorted))] {
		out = append(out, Consumer{Namespace: u.Namespace, Name: u.Name, CPU: u.CPU, Memory: u.Memory})
	}
	return out
}

func describe(err error) string {
	if errors.Is(err, metrics.ErrNoMetrics) {
		return "Install metrics-server to see usage."
	}
	return kerr.Classify(err).Message
}
