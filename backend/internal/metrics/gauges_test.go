package metrics

import (
	"errors"
	"math"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const gib = 1 << 30

func node(name, mem, pods string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse(mem),
			corev1.ResourcePods:   resource.MustParse(pods),
		}},
	}
}

func requests(cpu, mem string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(mem),
	}}
}

func runningPod(nodeName string, containers ...corev1.Container) corev1.Pod {
	return corev1.Pod{
		Spec:   corev1.PodSpec{NodeName: nodeName, Containers: containers},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestCPUAndMemory(t *testing.T) {
	nodes := []corev1.Node{node("a", "4Gi", "110"), node("b", "4Gi", "110")}
	always := corev1.ContainerRestartPolicyAlways

	sidecarPod := runningPod("a", corev1.Container{Resources: requests("100m", "64Mi")})
	sidecarPod.Spec.InitContainers = []corev1.Container{
		{Name: "sidecar", RestartPolicy: &always, Resources: requests("50m", "32Mi")},
		{Name: "migrate", Resources: requests("1", "1Gi")}, // bigger than app + sidecar
	}
	sidecarPod.Spec.Overhead = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}

	pods := []corev1.Pod{
		runningPod("a", corev1.Container{Resources: requests("500m", "1Gi")}, corev1.Container{Resources: requests("250m", "512Mi")}),
		sidecarPod,
		{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: requests("4", "8Gi")}}}}, // unscheduled: not counted
		{Spec: corev1.PodSpec{NodeName: "b", Containers: []corev1.Container{{Resources: requests("4", "8Gi")}}}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
	}
	usage := map[string]Usage{"a": {CPU: 0.5, Memory: 1 * gib}, "b": {CPU: 0.25, Memory: 2 * gib}, "gone": {CPU: 9}}

	tests := []struct {
		name      string
		usageErr  error
		wantState State
		wantCPU   float64
		wantMem   float64
	}{
		{name: "live", wantState: Live, wantCPU: 0.75, wantMem: 3 * gib},
		{name: "no metrics-server", usageErr: ErrNoMetrics, wantState: NoMetrics},
		{name: "metrics error", usageErr: errors.New("boom"), wantState: Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cpu, mem := CPUAndMemory(nodes, pods, usage, tt.usageErr)
			if cpu.State != tt.wantState || mem.State != tt.wantState {
				t.Fatalf("states = %s/%s, want %s", cpu.State, mem.State, tt.wantState)
			}
			if !near(cpu.Value, tt.wantCPU) || !near(mem.Value, tt.wantMem) {
				t.Fatalf("values = %v cores / %v bytes", cpu.Value, mem.Value)
			}
			if cpu.Max != 4 || mem.Max != 8*gib {
				t.Fatalf("max = %v cores / %v bytes", cpu.Max, mem.Max)
			}
			// 0.75 + max(app 0.1 + sidecar 0.05, init 1 + the sidecar already running 0.05) + 0.01 overhead
			if !near(*cpu.Requested, 1.81) {
				t.Fatalf("cpu requested = %v", *cpu.Requested)
			}
			if !near(*mem.Requested, 1.5*gib+1*gib+32*1024*1024) {
				t.Fatalf("mem requested = %v GiB", *mem.Requested/gib)
			}
			if tt.wantState != Live && cpu.Message == "" {
				t.Fatal("non-live gauges explain themselves")
			}
		})
	}
}

func TestPods(t *testing.T) {
	nodes := []corev1.Node{node("a", "4Gi", "110"), node("b", "16Gi", "250")}
	pods := []corev1.Pod{
		runningPod("a"), runningPod("b"),
		{Status: corev1.PodStatus{Phase: corev1.PodPending}},
		{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
	}
	g := Pods(nodes, pods)
	if g.Value != 2 || g.Max != 360 || g.Nodes != 2 || g.PerNodeMax != 250 || g.State != Live {
		t.Fatalf("got %+v", g)
	}
}

func pvc(ns, name, size string, phase corev1.PersistentVolumeClaimPhase) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase:    phase,
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
		},
	}
}

func TestStorage(t *testing.T) {
	pvcs := []corev1.PersistentVolumeClaim{
		pvc("db", "data-0", "10Gi", corev1.ClaimBound),
		pvc("db", "data-1", "10Gi", corev1.ClaimBound),
		pvc("db", "pending", "100Gi", corev1.ClaimPending),
	}
	tests := []struct {
		name      string
		pvcs      []corev1.PersistentVolumeClaim
		usage     map[PVCKey]float64
		usageErr  error
		wantState State
		wantValue float64
		wantMax   float64
	}{
		{name: "usage reported", pvcs: pvcs, usage: map[PVCKey]float64{{"db", "data-0"}: 2 * gib, {"db", "data-1"}: 1 * gib}, wantState: Live, wantValue: 3 * gib, wantMax: 20 * gib},
		{name: "capacity only", pvcs: pvcs, usage: map[PVCKey]float64{}, wantState: NoMetrics, wantMax: 20 * gib},
		{name: "kubelets unreachable", pvcs: pvcs, usageErr: errors.New("forbidden"), wantState: Failed, wantMax: 20 * gib},
		{name: "no volumes", wantState: NoMetrics},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Storage(tt.pvcs, tt.usage, tt.usageErr)
			if g.State != tt.wantState || !near(g.Value, tt.wantValue) || !near(g.Max, tt.wantMax) {
				t.Fatalf("got %+v", g)
			}
			if len(tt.pvcs) > 0 && g.Volumes != 2 {
				t.Fatalf("volumes = %d, want 2 bound", g.Volumes)
			}
		})
	}
}
