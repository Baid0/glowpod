package nodes

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"glowpod/internal/metrics"
)

func controlled(kind string) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{Kind: kind, Name: "owner", Controller: &yes, APIVersion: "apps/v1"}}
}

func pod(ns, name string, mod func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, OwnerReferences: controlled("ReplicaSet")},
		Spec: corev1.PodSpec{NodeName: "w1", Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")},
		}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if mod != nil {
		mod(p)
	}
	return p
}

func TestPlanFor(t *testing.T) {
	now := metav1.Now()
	pods := []corev1.Pod{
		*pod("shop", "web-1", nil),
		*pod("shop", "cache-1", func(p *corev1.Pod) {
			p.Spec.Volumes = []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
		}),
		*pod("kube-system", "proxy-x", func(p *corev1.Pod) { p.OwnerReferences = controlled("DaemonSet") }),
		*pod("kube-system", "etcd-w1", func(p *corev1.Pod) {
			p.OwnerReferences = nil
			p.Annotations = map[string]string{mirrorAnnotation: "abc"}
		}),
		*pod("dev", "scratch", func(p *corev1.Pod) { p.OwnerReferences = nil }),
		*pod("dev", "done", func(p *corev1.Pod) { p.OwnerReferences = nil; p.Status.Phase = corev1.PodSucceeded }),
		*pod("shop", "leaving", func(p *corev1.Pod) { p.DeletionTimestamp = &now }),
	}
	plan := PlanFor(pods)
	names := func(refs []PodRef) map[string]PodRef {
		m := map[string]PodRef{}
		for _, r := range refs {
			m[r.Name] = r
		}
		return m
	}
	evict, skip := names(plan.Evict), names(plan.Skip)
	for _, n := range []string{"web-1", "cache-1", "done"} {
		if _, ok := evict[n]; !ok {
			t.Errorf("%s should be evicted", n)
		}
	}
	for _, n := range []string{"proxy-x", "etcd-w1", "scratch", "leaving"} {
		if r, ok := skip[n]; !ok || r.Reason == "" {
			t.Errorf("%s should be skipped with a reason, got %+v", n, r)
		}
	}
	if !evict["cache-1"].LocalData || evict["web-1"].LocalData {
		t.Error("emptyDir pods are flagged as losing local data")
	}
}

func TestDrain(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w1"}}
	kube := fake.NewClientset(node,
		pod("shop", "web-1", nil), pod("shop", "web-2", nil), pod("shop", "db-0", nil),
		pod("kube-system", "proxy-x", func(p *corev1.Pod) { p.OwnerReferences = controlled("DaemonSet") }),
		pod("other", "elsewhere", func(p *corev1.Pod) { p.Spec.NodeName = "w2" }),
	)
	// The fake clientset ignores field selectors; keep the list to w1.
	kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		all, err := kube.Tracker().List(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, "")
		if err != nil {
			return true, nil, err
		}
		list := all.(*corev1.PodList)
		out := &corev1.PodList{}
		for _, p := range list.Items {
			if p.Spec.NodeName == "w1" {
				out.Items = append(out.Items, p)
			}
		}
		return true, out, nil
	})
	var evicted []string
	kube.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		e := a.(k8stesting.CreateAction).GetObject().(*policyv1.Eviction)
		switch e.Name {
		case "db-0":
			return true, nil, apierrors.NewTooManyRequests("Cannot evict pod as it would violate the pod's disruption budget.", 10)
		case "web-2":
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods/eviction"}, "web-2", errors.New("rbac"))
		}
		evicted = append(evicted, e.Name)
		return true, nil, nil
	})

	res, err := Drain(context.Background(), kube, "w1")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := kube.CoreV1().Nodes().Get(context.Background(), "w1", metav1.GetOptions{})
	if !got.Spec.Unschedulable {
		t.Fatal("drain cordons first")
	}
	if len(res.Evicted) != 1 || res.Evicted[0].Name != "web-1" || len(evicted) != 1 {
		t.Fatalf("evicted = %+v", res.Evicted)
	}
	if len(res.Blocked) != 1 || res.Blocked[0].Name != "db-0" || res.Blocked[0].Reason == "" {
		t.Fatalf("blocked = %+v", res.Blocked)
	}
	if len(res.Failed) != 1 || res.Failed[0].Name != "web-2" {
		t.Fatalf("failed = %+v", res.Failed)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Name != "proxy-x" {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
}

func TestCordon(t *testing.T) {
	kube := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w1"}})
	for _, want := range []bool{true, false} {
		if err := Cordon(context.Background(), kube, "w1", want); err != nil {
			t.Fatal(err)
		}
		n, _ := kube.CoreV1().Nodes().Get(context.Background(), "w1", metav1.GetOptions{})
		if n.Spec.Unschedulable != want {
			t.Fatalf("unschedulable = %v, want %v", n.Spec.Unschedulable, want)
		}
	}
	if err := Cordon(context.Background(), kube, "missing", true); !apierrors.IsNotFound(errors.Unwrap(err)) {
		t.Fatalf("err = %v", err)
	}
}

type usage map[string]metrics.Usage

func (u usage) NodeUsage(context.Context) (map[string]metrics.Usage, error) { return u, nil }

func TestBuild(t *testing.T) {
	since := metav1.NewTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "w1", Labels: map[string]string{rolePrefix + "worker": "", "zone": "a"}},
		Spec:       corev1.NodeSpec{Unschedulable: true, Taints: []corev1.Taint{{Key: "gpu", Value: "yes", Effect: corev1.TaintEffectNoSchedule}}},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue, LastTransitionTime: since},
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastTransitionTime: since},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse, LastTransitionTime: since},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi"), corev1.ResourcePods: resource.MustParse("110"),
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.36.5", Architecture: "arm64"},
		},
	}
	kube := fake.NewClientset(node, pod("shop", "web-1", nil), pod("shop", "web-2", nil))
	d, err := Build(context.Background(), kube, usage{"w1": {CPU: 1, Memory: 2 << 30}}, "w1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Ready || !d.Unschedulable || len(d.Roles) != 1 || d.Roles[0] != "worker" {
		t.Fatalf("detail = %+v", d)
	}
	if d.Conditions[0].Type != "Ready" || d.Conditions[0].Problem {
		t.Fatalf("Ready comes first and isn't a problem: %+v", d.Conditions)
	}
	if mp := d.Conditions[1]; mp.Type != "MemoryPressure" || !mp.Problem {
		t.Fatalf("memory pressure is a problem: %+v", mp)
	}
	if d.Gauges.CPU.Value != 1 || d.Gauges.CPU.Max != 4 || *d.Gauges.CPU.Requested != 1 {
		t.Fatalf("cpu = %+v", d.Gauges.CPU)
	}
	if d.Gauges.Pods.Value != 2 || d.Gauges.Pods.Max != 110 || len(d.Pods) != 2 {
		t.Fatalf("pods gauge = %+v, pods = %d", d.Gauges.Pods, len(d.Pods))
	}
	if len(d.Taints) != 1 || d.Info.Architecture != "arm64" {
		t.Fatalf("taints = %+v info = %+v", d.Taints, d.Info)
	}
	if _, err := Build(context.Background(), kube, nil, "missing", time.Now()); err == nil {
		t.Fatal("want not found")
	}
}
