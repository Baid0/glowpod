package overview

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/cluster"
	"glowpod/internal/metrics"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) metav1.Time { return metav1.NewTime(now.Add(-d)) }

func ptr[T any](v T) *T { return &v }

func controller(kind, name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{Kind: kind, Name: name, Controller: ptr(true), APIVersion: "apps/v1"}}
}

func nodeObj(name string, ready corev1.ConditionStatus, extra ...corev1.NodeCondition) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Conditions: append([]corev1.NodeCondition{{Type: corev1.NodeReady, Status: ready}}, extra...),
		},
	}
}

func podObj(ns, name, node string, phase corev1.PodPhase, owner []metav1.OwnerReference, statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, OwnerReferences: owner, CreationTimestamp: ago(time.Hour)},
		Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
			},
		}}},
		Status: corev1.PodStatus{Phase: phase, ContainerStatuses: statuses},
	}
}

func crashLoop(restarts int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name: "app", RestartCount: restarts,
		State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, FinishedAt: ago(23 * time.Minute)}},
	}
}

func oomKilled() corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name: "app", Ready: true,
		State:                corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: ago(41 * time.Minute)}},
	}
}

func readyStatus() corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
}

func deployment(ns, name string, want, ready int32, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, CreationTimestamp: ago(48 * time.Hour)},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr(want)},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: ready, AvailableReplicas: ready},
	}
}

func replicaSet(ns, name, deploy, rev, image string, created time.Duration) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns, Name: name, OwnerReferences: controller("Deployment", deploy),
			Annotations:       map[string]string{"deployment.kubernetes.io/revision": rev},
			CreationTimestamp: ago(created),
		},
		Spec: appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: image}}}}},
	}
}

// labCluster looks like the design board: payments crash-looping, an OOM in
// checkout, a pod waiting for memory in search, a failed backup job.
func labCluster() []runtime.Object {
	pending := podObj("search", "search-indexer-0", "", corev1.PodPending, controller("StatefulSet", "search-indexer"))
	pending.CreationTimestamp = ago(6 * time.Minute)
	pending.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: "0/3 nodes are available: 3 Insufficient memory."}}

	return []runtime.Object{
		nodeObj("w1", corev1.ConditionTrue),
		nodeObj("w2", corev1.ConditionTrue, corev1.NodeCondition{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue}),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "payments"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "checkout"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "search"}},

		podObj("payments", "payments-api-7d9f6b-x2kq9", "w1", corev1.PodRunning, controller("ReplicaSet", "payments-api-7d9f6b"), crashLoop(14)),
		podObj("payments", "payments-api-7d9f6b-ok", "w1", corev1.PodRunning, controller("ReplicaSet", "payments-api-7d9f6b"), readyStatus()),
		podObj("checkout", "checkout-web-6f4b9-m8zr2", "w2", corev1.PodRunning, controller("ReplicaSet", "checkout-web-6f4b9"), oomKilled()),
		podObj("checkout", "checkout-web-6f4b9-ok", "w2", corev1.PodRunning, controller("ReplicaSet", "checkout-web-6f4b9"), readyStatus()),
		pending,
		podObj("search", "done-job-abc", "w1", corev1.PodSucceeded, controller("Job", "done-job")),

		deployment("payments", "payments-api", 2, 1, map[string]string{"app.kubernetes.io/name": "payments"}),
		deployment("checkout", "checkout-web", 2, 2, nil),
		replicaSet("payments", "payments-api-7d9f6b", "payments-api", "3", "ghcr.io/acme/payments:v3", 20*time.Minute),
		replicaSet("checkout", "checkout-web-6f4b9", "checkout-web", "7", "ghcr.io/acme/checkout:v7", 3*time.Minute),
		replicaSet("checkout", "checkout-web-old", "checkout-web", "6", "ghcr.io/acme/checkout:v6", 26*time.Hour),
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: "search", Name: "search-indexer"},
			Spec:       appsv1.StatefulSetSpec{Replicas: ptr(int32(1))},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "kube-proxy"},
			Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 2},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ops", Name: "backup-nightly-29341", CreationTimestamp: ago(time.Hour)},
			Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", LastTransitionTime: ago(30 * time.Minute)},
			}},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ops", Name: "backup-nightly-29340", CreationTimestamp: ago(3 * time.Hour)},
			Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ops", Name: "ancient", CreationTimestamp: ago(72 * time.Hour)},
			Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Namespace: "search", Name: "data-search-indexer-0"},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase:    corev1.ClaimBound,
				Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			},
		},
	}
}

type fakeMetrics struct {
	nodes    map[string]metrics.Usage
	nodesErr error
	pods     []metrics.PodUsage
	podsErr  error
	volumes  map[metrics.PVCKey]float64
}

func (f *fakeMetrics) NodeUsage(context.Context) (map[string]metrics.Usage, error) {
	return f.nodes, f.nodesErr
}

func (f *fakeMetrics) PodUsage(_ context.Context, ns string) ([]metrics.PodUsage, error) {
	if f.podsErr != nil {
		return nil, f.podsErr
	}
	var out []metrics.PodUsage
	for _, p := range f.pods {
		if ns == "" || p.Namespace == ns {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeMetrics) VolumeUsage(context.Context, []string) (map[metrics.PVCKey]float64, error) {
	return f.volumes, nil
}

type fakeProm struct{ value float64 }

func (p fakeProm) Query(context.Context, string) (float64, error) { return p.value, nil }

func healthyMetrics() *fakeMetrics {
	return &fakeMetrics{
		nodes: map[string]metrics.Usage{"w1": {CPU: 1, Memory: 2 << 30}, "w2": {CPU: 3.6, Memory: 4 << 30}},
		pods: []metrics.PodUsage{
			{Namespace: "payments", Name: "payments-api-7d9f6b-ok", Usage: metrics.Usage{CPU: 0.2, Memory: 100}},
			{Namespace: "checkout", Name: "checkout-web-6f4b9-ok", Usage: metrics.Usage{CPU: 0.9, Memory: 50}},
		},
		volumes: map[metrics.PVCKey]float64{{Namespace: "search", Name: "data-search-indexer-0"}: 2 << 30},
	}
}

func newTestService() *Service {
	s := NewService()
	s.now = func() time.Time { return now }
	return s
}

func build(t *testing.T, s *Service, kube *fake.Clientset, m *fakeMetrics, prom metrics.Prometheus, ns string) *Response {
	t.Helper()
	deps := Deps{Kube: kube, Metrics: m, Prometheus: func(context.Context) (metrics.Prometheus, error) { return prom, nil }}
	resp, err := s.Build(context.Background(), cluster.Identity{Key: "test"}, cluster.Info{ID: "default", Name: "lab"}, deps, ns)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestBuildAllNamespaces(t *testing.T) {
	resp := build(t, newTestService(), fake.NewClientset(labCluster()...), healthyMetrics(), fakeProm{value: 1e6}, "")

	if resp.Cluster.Nodes != 2 || resp.Cluster.Namespaces != 3 || resp.Cluster.Name != "lab" {
		t.Fatalf("cluster = %+v", resp.Cluster)
	}
	g := resp.Gauges
	if g.CPU.State != metrics.Live || g.CPU.Value != 4.6 || g.CPU.Max != 8 {
		t.Fatalf("cpu = %+v", g.CPU)
	}
	if g.Pods.Value != 4 || g.Pods.Max != 220 {
		t.Fatalf("pods = %+v", g.Pods)
	}
	if g.Storage.State != metrics.Live || g.Storage.Volumes != 1 {
		t.Fatalf("storage = %+v", g.Storage)
	}
	if g.Network == nil || g.Network.State != metrics.Live {
		t.Fatalf("network = %+v", g.Network)
	}

	w := resp.Workloads
	if w.Deployments != (WorkloadCount{Total: 2, Ready: 1, Failing: 1}) {
		t.Fatalf("deployments = %+v", w.Deployments)
	}
	if w.StatefulSets != (WorkloadCount{Total: 1, Pending: 1}) {
		t.Fatalf("statefulsets = %+v", w.StatefulSets)
	}
	if w.DaemonSets != (WorkloadCount{Total: 1, Ready: 1}) {
		t.Fatalf("daemonsets = %+v", w.DaemonSets)
	}
	if w.Jobs != (JobCount{Succeeded: 1, Failed: 1}) {
		t.Fatalf("jobs (24h) = %+v", w.Jobs)
	}

	if resp.Health != (Health{Running: 2, Pending: 1, Failing: 2}) {
		t.Fatalf("health = %+v", resp.Health)
	}

	var reasons []string
	for _, p := range resp.Problems {
		reasons = append(reasons, p.Reason)
	}
	// Critical first, newest first within a severity.
	want := []string{"CrashLoopBackOff", "BackoffLimitExceeded", "OOMKilled", "Pending", "MemoryPressure"}
	if len(reasons) != len(want) || resp.ProblemCount != len(want) {
		t.Fatalf("problems = %v", reasons)
	}
	for i := range want {
		if reasons[i] != want[i] {
			t.Fatalf("problems = %v, want %v", reasons, want)
		}
	}
	if resp.Problems[3].Detail != "no node has 1Gi memory free" {
		t.Fatalf("pending detail = %q", resp.Problems[3].Detail)
	}

	if len(resp.Nodes) != 2 || resp.Nodes[0].Hot || !resp.Nodes[1].Hot || *resp.Nodes[1].CPU != 0.9 {
		t.Fatalf("nodes = %+v / %+v", resp.Nodes[0], resp.Nodes[1])
	}
	if len(resp.TopConsumers) != 2 || resp.TopConsumers[0].Name != "checkout-web-6f4b9-ok" {
		t.Fatalf("top consumers = %+v", resp.TopConsumers)
	}

	if len(resp.Apps) != 4 || resp.Apps[0].Name != "payments" || resp.Apps[0].Status != AppCritical {
		t.Fatalf("apps = %+v", resp.Apps)
	}
	if len(resp.RecentDeploys) != 3 || resp.RecentDeploys[0].Name != "checkout-web" || resp.RecentDeploys[0].Revision != "7" {
		t.Fatalf("deploys = %+v", resp.RecentDeploys)
	}
	if len(resp.Unavailable) != 0 {
		t.Fatalf("unavailable = %+v", resp.Unavailable)
	}
}

func TestBuildVariants(t *testing.T) {
	tests := []struct {
		name  string
		ns    string
		mets  func() *fakeMetrics
		prom  metrics.Prometheus
		check func(t *testing.T, r *Response)
	}{
		{
			name: "namespace filter keeps gauges cluster-wide",
			ns:   "payments",
			mets: healthyMetrics,
			check: func(t *testing.T, r *Response) {
				if r.Gauges.Pods.Value != 4 {
					t.Fatalf("gauges must stay cluster-wide, pods = %v", r.Gauges.Pods.Value)
				}
				if r.ProblemCount != 1 || r.Problems[0].Namespace != "payments" {
					t.Fatalf("problems = %+v", r.Problems)
				}
				if r.Workloads.Deployments.Total != 1 || r.Health != (Health{Running: 1, Failing: 1}) {
					t.Fatalf("workloads = %+v health = %+v", r.Workloads, r.Health)
				}
				if len(r.TopConsumers) != 1 {
					t.Fatalf("top consumers = %+v", r.TopConsumers)
				}
			},
		},
		{
			name: "no prometheus hides the network gauge",
			mets: healthyMetrics,
			check: func(t *testing.T, r *Response) {
				if r.Gauges.Network != nil {
					t.Fatalf("network = %+v", r.Gauges.Network)
				}
			},
		},
		{
			name: "no metrics-server",
			mets: func() *fakeMetrics {
				return &fakeMetrics{nodesErr: metrics.ErrNoMetrics, podsErr: metrics.ErrNoMetrics}
			},
			check: func(t *testing.T, r *Response) {
				if r.Gauges.CPU.State != metrics.NoMetrics || r.Gauges.Memory.State != metrics.NoMetrics {
					t.Fatalf("cpu/memory = %s/%s", r.Gauges.CPU.State, r.Gauges.Memory.State)
				}
				if r.Gauges.Pods.State != metrics.Live {
					t.Fatal("pods gauge doesn't need metrics-server")
				}
				if r.Nodes[0].CPU != nil {
					t.Fatal("node load has no usage without metrics")
				}
				if len(r.Unavailable) != 1 || r.Unavailable[0].Section != "topConsumers" {
					t.Fatalf("unavailable = %+v", r.Unavailable)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, build(t, newTestService(), fake.NewClientset(labCluster()...), tt.mets(), tt.prom, tt.ns))
		})
	}
}

func TestBuildTrendsAppearAfterFiveMinutes(t *testing.T) {
	s := newTestService()
	kube := fake.NewClientset(labCluster()...)
	if r := build(t, s, kube, healthyMetrics(), nil, ""); r.Gauges.CPU.Trend != nil {
		t.Fatal("no trend on the first load")
	}
	m := healthyMetrics()
	m.nodes["w1"] = metrics.Usage{CPU: 1.8, Memory: 2 << 30}
	s.now = func() time.Time { return now.Add(6 * time.Minute) }
	r := build(t, s, kube, m, nil, "")
	if r.Gauges.CPU.Trend == nil || *r.Gauges.CPU.Trend <= 0 || r.Gauges.CPU.TrendWindow != 360 {
		t.Fatalf("cpu trend = %v window %d", r.Gauges.CPU.Trend, r.Gauges.CPU.TrendWindow)
	}

	// Another identity has its own history.
	deps := Deps{Kube: kube, Metrics: m}
	other, err := s.Build(context.Background(), cluster.Identity{Key: "someone-else"}, cluster.Info{}, deps, "")
	if err != nil {
		t.Fatal(err)
	}
	if other.Gauges.CPU.Trend != nil {
		t.Fatal("trend history leaked across identities")
	}
}

// Someone with access to one namespace can't list nodes or every pod; the
// Overview still works for their namespace, without the load gauges.
func TestBuildForNamespaceUser(t *testing.T) {
	kube := fake.NewClientset(labCluster()...)
	kube.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "", errors.New("rbac"))
	})
	kube.PrependReactor("list", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetNamespace() == "" {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("rbac"))
		}
		return false, nil, nil
	})
	deps := Deps{Kube: kube, Metrics: healthyMetrics()}
	resp, err := newTestService().Build(context.Background(), cluster.Identity{Key: "t"}, cluster.Info{}, deps, "payments")
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]metrics.Gauge{"cpu": resp.Gauges.CPU, "memory": resp.Gauges.Memory, "pods": resp.Gauges.Pods} {
		if g.State != metrics.NoAccess || g.Message == "" {
			t.Errorf("%s gauge = %+v, want no-access", name, g)
		}
	}
	if resp.Health.Running+resp.Health.Pending+resp.Health.Failing == 0 {
		t.Fatal("the namespace's pods still count")
	}
	if _, err := newTestService().Build(context.Background(), cluster.Identity{Key: "t"}, cluster.Info{}, deps, ""); !apierrors.IsForbidden(err) {
		t.Fatalf("all namespaces without access = %v", err)
	}
}

func TestBuildFailures(t *testing.T) {
	forbidden := func(resource string) k8stesting.ReactionFunc {
		return func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: resource}, "", errors.New("rbac"))
		}
	}
	tests := []struct {
		name        string
		resource    string
		wantErr     bool
		unavailable string
	}{
		{name: "pods in all namespaces are required", resource: "pods", wantErr: true},
		{name: "workloads are optional", resource: "deployments", unavailable: "workloads"},
		{name: "jobs are optional", resource: "jobs", unavailable: "jobs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kube := fake.NewClientset(labCluster()...)
			kube.PrependReactor("list", tt.resource, forbidden(tt.resource))
			deps := Deps{Kube: kube, Metrics: healthyMetrics()}
			resp, err := newTestService().Build(context.Background(), cluster.Identity{Key: "t"}, cluster.Info{}, deps, "")
			if tt.wantErr {
				var req *RequiredError
				if !errors.As(err, &req) || !apierrors.IsForbidden(err) {
					t.Fatalf("err = %v, want a forbidden RequiredError", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Unavailable) != 1 || resp.Unavailable[0].Section != tt.unavailable {
				t.Fatalf("unavailable = %+v", resp.Unavailable)
			}
			if resp.Unavailable[0].Message != "You don't have access to this. Ask an admin for access." {
				t.Fatalf("message = %q", resp.Unavailable[0].Message)
			}
		})
	}
}
