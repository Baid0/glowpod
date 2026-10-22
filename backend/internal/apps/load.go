package apps

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/paging"
	"glowpod/internal/summary"
)

// ErrNotFound means no workload in the namespace belongs to that app.
var ErrNotFound = errors.New("app not found")

// Detail is GET /api/v1/clusters/{cluster}/apps/{namespace}/{name}.
type Detail struct {
	App       App               `json:"app"`
	Workloads []Workload        `json:"workloads"`
	Pods      []summary.Pod     `json:"pods"`
	Problems  []summary.Problem `json:"problems"`
	Deploys   []Deploy          `json:"deploys"`
}

// Snapshot is a namespace's workloads, ReplicaSets and pods, read together.
type Snapshot struct {
	Workloads   Workloads
	ReplicaSets []appsv1.ReplicaSet
	Pods        []corev1.Pod
}

// Load reads a Snapshot of namespace ("" = all namespaces).
func Load(ctx context.Context, kube kubernetes.Interface, namespace string) (*Snapshot, error) {
	return load(ctx, kube, namespace)
}

func load(ctx context.Context, kube kubernetes.Interface, namespace string) (*Snapshot, error) {
	var snap Snapshot
	g, ctx := errgroup.WithContext(ctx)
	apps := kube.AppsV1()
	g.Go(func() (err error) {
		snap.Workloads.Deployments, err = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.Deployment, string, error) {
			l, err := apps.Deployments(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	g.Go(func() (err error) {
		snap.Workloads.StatefulSets, err = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
			l, err := apps.StatefulSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	g.Go(func() (err error) {
		snap.Workloads.DaemonSets, err = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
			l, err := apps.DaemonSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	g.Go(func() (err error) {
		snap.ReplicaSets, err = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]appsv1.ReplicaSet, string, error) {
			l, err := apps.ReplicaSets(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	g.Go(func() (err error) {
		snap.Pods, err = paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Pod, string, error) {
			l, err := kube.CoreV1().Pods(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("list workloads: %w", err)
	}
	return &snap, nil
}

// List returns the apps in namespace ("" = all namespaces).
func List(ctx context.Context, kube kubernetes.Interface, namespace string, now time.Time) ([]App, error) {
	snap, err := load(ctx, kube, namespace)
	if err != nil {
		return nil, err
	}
	ix := NewIndex(snap.ReplicaSets)
	return Group(snap.Workloads, ix, Failing(snap.Pods, ix, now)), nil
}

// Get returns one app with its workloads, pods, problems and recent deploys.
func Get(ctx context.Context, kube kubernetes.Interface, namespace, name string, now time.Time) (*Detail, error) {
	snap, err := load(ctx, kube, namespace)
	if err != nil {
		return nil, err
	}
	ix := NewIndex(snap.ReplicaSets)
	failing := Failing(snap.Pods, ix, now)

	mine := Workloads{}
	for _, d := range snap.Workloads.Deployments {
		if Key(&d.ObjectMeta) == name {
			mine.Deployments = append(mine.Deployments, d)
		}
	}
	for _, s := range snap.Workloads.StatefulSets {
		if Key(&s.ObjectMeta) == name {
			mine.StatefulSets = append(mine.StatefulSets, s)
		}
	}
	for _, d := range snap.Workloads.DaemonSets {
		if Key(&d.ObjectMeta) == name {
			mine.DaemonSets = append(mine.DaemonSets, d)
		}
	}
	grouped := Group(mine, ix, failing)
	if len(grouped) == 0 {
		return nil, ErrNotFound
	}

	d := &Detail{
		App:       grouped[0],
		Workloads: rows(mine, ix, failing),
		Pods:      []summary.Pod{},
		Problems:  []summary.Problem{},
	}
	slices.SortFunc(d.Workloads, func(a, b Workload) int {
		if c := cmp.Compare(Severity(b.Status), Severity(a.Status)); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	owners := map[Owner]bool{}
	deployments := map[string]bool{}
	for _, w := range d.Workloads {
		owners[Owner{w.Kind, namespace, w.Name}] = true
		if w.Kind == "Deployment" {
			deployments[w.Name] = true
		}
	}
	for i := range snap.Pods {
		o, ok := ix.OwnerOf(&snap.Pods[i])
		if !ok || !owners[o] {
			continue
		}
		pod := summary.PodOf(&snap.Pods[i], now)
		d.Pods = append(d.Pods, pod)
		if pod.Problem != nil {
			d.Problems = append(d.Problems, *pod.Problem)
		}
	}
	d.Deploys = RecentDeploys(snap.ReplicaSets, 10, func(dep string) bool { return deployments[dep] })
	return d, nil
}
