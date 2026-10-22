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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/apps"
	"glowpod/internal/cluster"
	"glowpod/internal/kerr"
	"glowpod/internal/metrics"
	"glowpod/internal/paging"
	"glowpod/internal/scope"
	"glowpod/internal/summary"
)

const (
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

// history is the trend history of one identity on one cluster.
func (s *Service) history(clusterID string, id cluster.Identity) *metrics.History {
	key := clusterID + "|" + id.Key
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.histories[key]
	if !ok {
		h = metrics.NewHistory()
		s.histories[key] = h
	}
	return h
}

func readyNodes(nodes []corev1.Node) int {
	n := 0
	for i := range nodes {
		for _, c := range nodes[i].Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				n++
			}
		}
	}
	return n
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
	// noNodes is set when the user may not list nodes: the load gauges
	// can't be computed, the rest of the page still works.
	noNodes bool
}

// Build loads the cluster state and assembles the Overview for the
// namespaces in ns (none = all namespaces).
func (s *Service) Build(ctx context.Context, id cluster.Identity, info cluster.Info, deps Deps, ns scope.Namespaces) (*Response, error) {
	snap, err := load(ctx, deps, ns)
	if err != nil {
		return nil, err
	}
	now := s.now()
	resp := &Response{
		Cluster: ClusterInfo{
			Info:       info,
			Version:    snap.version,
			Nodes:      len(snap.nodes),
			NodesReady: readyNodes(snap.nodes),
			Namespaces: snap.namespaces,
		},
		Namespace:   ns.String(),
		GeneratedAt: now.UTC(),
		Thresholds:  s.thresholds,
	}

	resp.Gauges = s.gauges(ctx, info.ID, id, snap, now)

	pods := inNamespaces(snap.pods, ns)
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

	ix := apps.NewIndex(snap.replicaSets)
	failing := apps.Failing(pods, ix, now)
	resp.Workloads = workloads(snap, failing, now)
	resp.Problems, resp.ProblemCount = problems(snap, pods, ns, now)
	resp.Nodes = nodeLoad(snap, s.thresholds[1])
	resp.TopConsumers = topConsumers(snap.podUsage)
	resp.Apps = apps.Group(apps.Workloads{
		Deployments:  snap.deployments,
		StatefulSets: snap.statefulSets,
		DaemonSets:   snap.daemonSets,
	}, ix, failing)
	if len(resp.Apps) > maxApps {
		resp.Apps = resp.Apps[:maxApps]
	}
	resp.RecentDeploys = apps.RecentDeploys(snap.replicaSets, topN, nil)

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

func (s *Service) gauges(ctx context.Context, clusterID string, id cluster.Identity, snap *snapshot, now time.Time) Gauges {
	var g Gauges
	g.CPU, g.Memory = metrics.CPUAndMemory(snap.nodes, snap.pods, snap.nodeUsage, snap.nodeUsageErr)
	g.Pods = metrics.Pods(snap.nodes, snap.pods)
	g.Storage = metrics.Storage(snap.pvcs, snap.volumeUsage, snap.volumeErr)
	switch err := snap.errs["storage"]; {
	case apierrors.IsForbidden(err):
		g.Storage = metrics.Gauge{State: metrics.NoAccess, Unit: "bytes", Message: "You can't see the cluster's volumes."}
	case err != nil:
		g.Storage = metrics.Gauge{State: metrics.Failed, Unit: "bytes", Message: "Couldn't list volumes."}
	}
	if snap.noNodes {
		const msg = "You can't see the cluster's nodes, so its load isn't shown."
		g.CPU = metrics.Gauge{State: metrics.NoAccess, Unit: "cores", Message: msg}
		g.Memory = metrics.Gauge{State: metrics.NoAccess, Unit: "bytes", Message: msg}
		g.Pods = metrics.Gauge{State: metrics.NoAccess, Unit: "pods", Message: msg}
	}

	switch {
	case apierrors.IsForbidden(snap.promErr):
		// Prometheus out of reach for this user: like not having it, hide the gauge.
	case snap.promErr != nil:
		g.Network = &metrics.Gauge{State: metrics.Failed, Unit: "bytes/s", Source: "prometheus", Message: "Couldn't look for Prometheus."}
	case snap.prom != nil:
		n := metrics.Network(ctx, snap.prom)
		g.Network = &n
	}

	h := s.history(clusterID, id)
	if snap.noNodes {
		h.Apply("storage", &g.Storage, now)
		return g
	}
	h.Apply("cpu", &g.CPU, now)
	h.Apply("memory", &g.Memory, now)
	h.Apply("pods", &g.Pods, now)
	h.Apply("storage", &g.Storage, now)
	if g.Network != nil {
		h.Apply("network", g.Network, now)
	}
	return g
}

func load(ctx context.Context, deps Deps, ns scope.Namespaces) (*snapshot, error) {
	snap := &snapshot{errs: map[string]error{}}
	kube := deps.Kube
	// Without nodes or pods there's no Overview; when either fails for a
	// reason other than RBAC (a cluster that's gone), stop everything else
	// instead of waiting for each call to time out in turn.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		nodeErr  error
		podErr   error
		required error
		failOnce sync.Once
	)
	fail := func(what string, err error) {
		if err != nil && !apierrors.IsForbidden(err) {
			failOnce.Do(func() {
				required = &RequiredError{What: what, Err: err}
				cancel()
			})
		}
	}
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
		snap.nodes, nodeErr = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Node, string, error) {
			l, err := kube.CoreV1().Nodes().List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		fail("nodes", nodeErr)
	})
	wg.Go(func() {
		// All namespaces: the gauges are cluster-wide.
		snap.pods, podErr = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Pod, string, error) {
			l, err := kube.CoreV1().Pods("").List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		// Forbidden for all namespaces is retried per namespace below.
		fail("pods", podErr)
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
		l, err := paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Namespace, string, error) {
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
		snap.deployments, err = across(ctx, ns, func(ctx context.Context, namespace string, o metav1.ListOptions) ([]appsv1.Deployment, string, error) {
			l, err := kube.AppsV1().Deployments(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		if err != nil {
			return err
		}
		snap.statefulSets, err = across(ctx, ns, func(ctx context.Context, namespace string, o metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
			l, err := kube.AppsV1().StatefulSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		if err != nil {
			return err
		}
		snap.daemonSets, err = across(ctx, ns, func(ctx context.Context, namespace string, o metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
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
		snap.replicaSets, err = across(ctx, ns, func(ctx context.Context, namespace string, o metav1.ListOptions) ([]appsv1.ReplicaSet, string, error) {
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
		snap.jobs, err = across(ctx, ns, func(ctx context.Context, namespace string, o metav1.ListOptions) ([]batchv1.Job, string, error) {
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
		snap.pvcs, err = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.PersistentVolumeClaim, string, error) {
			l, err := kube.CoreV1().PersistentVolumeClaims("").List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	wg.Go(func() { snap.nodeUsage, snap.nodeUsageErr = deps.Metrics.NodeUsage(ctx) })
	wg.Go(func() { snap.podUsage, snap.podUsageErr = podUsage(ctx, deps.Metrics, ns) })
	if deps.Prometheus != nil {
		wg.Go(func() { snap.prom, snap.promErr = deps.Prometheus(ctx) })
	}
	wg.Wait()
	if required != nil {
		return nil, required
	}

	// People with access to some namespaces only can't list nodes or every
	// pod; show them what they can see.
	if apierrors.IsForbidden(nodeErr) {
		snap.noNodes, nodeErr = true, nil
	}
	if apierrors.IsForbidden(podErr) && !ns.All() {
		snap.pods, podErr = across(ctx, ns, func(ctx context.Context, namespace string, o metav1.ListOptions) ([]corev1.Pod, string, error) {
			l, err := kube.CoreV1().Pods(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
	}
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

func hasBound(pvcs []corev1.PersistentVolumeClaim) bool {
	return slices.ContainsFunc(pvcs, func(p corev1.PersistentVolumeClaim) bool {
		return p.Status.Phase == corev1.ClaimBound
	})
}

func inNamespaces(pods []corev1.Pod, ns scope.Namespaces) []corev1.Pod {
	if ns.All() {
		return pods
	}
	out := make([]corev1.Pod, 0, len(pods))
	for i := range pods {
		if ns.Contains(pods[i].Namespace) {
			out = append(out, pods[i])
		}
	}
	return out
}

func problems(snap *snapshot, pods []corev1.Pod, ns scope.Namespaces, now time.Time) ([]summary.Problem, int) {
	var out []summary.Problem
	for i := range pods {
		if p, ok := summary.PodProblem(&pods[i], now); ok {
			out = append(out, p)
		}
	}
	if ns.All() {
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

// across reads a list in each namespace of ns (or all of them) and joins it.
func across[T any](ctx context.Context, ns scope.Namespaces, list func(ctx context.Context, namespace string, o metav1.ListOptions) ([]T, string, error)) ([]T, error) {
	items, _, err := paging.Across(ctx, ns.Targets(), list)
	return items, err //nolint:wrapcheck // the section names what failed
}

// podUsage reads per-pod usage in each namespace of ns and joins it.
func podUsage(ctx context.Context, m MetricsSource, ns scope.Namespaces) ([]metrics.PodUsage, error) {
	var out []metrics.PodUsage
	for _, namespace := range ns.Targets() {
		u, err := m.PodUsage(ctx, namespace)
		if err != nil {
			return nil, err //nolint:wrapcheck // metrics errors are already plain
		}
		out = append(out, u...)
	}
	return out, nil
}
