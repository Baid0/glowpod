package metrics

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
)

// State of a gauge, matching the HalfGauge component's states.
type State string

// Gauge states the backend can know about. (Loading and stale are client-side.)
const (
	Live      State = "live"
	NoMetrics State = "no-metrics"
	Failed    State = "error"
)

// Gauge is one Overview gauge.
type Gauge struct {
	State State   `json:"state"`
	Value float64 `json:"value"`
	Max   float64 `json:"max"`
	// Unit of Value and Max: cores, bytes, pods or bytes/s.
	Unit string `json:"unit"`
	// Requested is what pods have reserved, in Unit (CPU and memory only).
	Requested *float64 `json:"requested,omitempty"`
	// Trend is the change in Value/Max over TrendWindow seconds (up to an hour).
	Trend       *float64 `json:"trend,omitempty"`
	TrendWindow int      `json:"trendWindow,omitempty"`
	// Message says, in plain words, why the gauge isn't live.
	Message string `json:"message,omitempty"`

	// Extras for the gauge footers.
	Nodes      int    `json:"nodes,omitempty"`
	PerNodeMax int64  `json:"perNodeMax,omitempty"`
	Volumes    int    `json:"volumes,omitempty"`
	Source     string `json:"source,omitempty"`
}

// Fraction is Value/Max, or 0 when Max is 0.
func (g Gauge) Fraction() float64 {
	if g.Max <= 0 {
		return 0
	}
	return g.Value / g.Max
}

const (
	noMetricsMessage = "Install metrics-server to see usage."
	errorMessage     = "Couldn't load usage."
)

// CPUAndMemory builds the CPU and memory gauges. usageErr is the error from
// reading node usage; ErrNoMetrics gives the no-metrics state.
func CPUAndMemory(nodes []corev1.Node, pods []corev1.Pod, usage map[string]Usage, usageErr error) (cpu, mem Gauge) {
	var allocCPU, allocMem float64
	for i := range nodes {
		allocCPU += nodes[i].Status.Allocatable.Cpu().AsApproximateFloat64()
		allocMem += nodes[i].Status.Allocatable.Memory().AsApproximateFloat64()
	}
	var reqCPU, reqMem float64
	for i := range pods {
		if !occupiesNode(&pods[i]) {
			continue
		}
		c, m := podRequests(&pods[i])
		reqCPU += c
		reqMem += m
	}

	cpu = Gauge{State: Live, Max: allocCPU, Unit: "cores", Requested: &reqCPU}
	mem = Gauge{State: Live, Max: allocMem, Unit: "bytes", Requested: &reqMem}

	switch {
	case errors.Is(usageErr, ErrNoMetrics):
		cpu.State, cpu.Message = NoMetrics, noMetricsMessage
		mem.State, mem.Message = NoMetrics, noMetricsMessage
	case usageErr != nil:
		cpu.State, cpu.Message = Failed, errorMessage
		mem.State, mem.Message = Failed, errorMessage
	default:
		for i := range nodes {
			u := usage[nodes[i].Name]
			cpu.Value += u.CPU
			mem.Value += u.Memory
		}
	}
	return cpu, mem
}

// Pods builds the pod-capacity gauge: running pods against the sum of every
// node's pod capacity.
func Pods(nodes []corev1.Node, pods []corev1.Pod) Gauge {
	g := Gauge{State: Live, Unit: "pods", Nodes: len(nodes)}
	for i := range nodes {
		n := nodes[i].Status.Allocatable.Pods().Value()
		g.Max += float64(n)
		g.PerNodeMax = max(g.PerNodeMax, n)
	}
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			g.Value++
		}
	}
	return g
}

// Storage builds the storage gauge: used bytes on bound PVCs against their
// total capacity. usage comes from kubelet stats; when no PVC reports usage
// the gauge shows capacity only (no-metrics).
func Storage(pvcs []corev1.PersistentVolumeClaim, usage map[PVCKey]float64, usageErr error) Gauge {
	g := Gauge{State: Live, Unit: "bytes"}
	reported := 0
	for i := range pvcs {
		pvc := &pvcs[i]
		if pvc.Status.Phase != corev1.ClaimBound {
			continue
		}
		g.Volumes++
		if c, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			g.Max += c.AsApproximateFloat64()
		}
		if used, ok := usage[PVCKey{Namespace: pvc.Namespace, Name: pvc.Name}]; ok {
			g.Value += used
			reported++
		}
	}
	switch {
	case g.Volumes == 0:
		g.State, g.Message = NoMetrics, "No persistent volumes yet."
	case usageErr != nil && reported == 0:
		g.State, g.Message = Failed, "Couldn't read volume usage from the kubelets."
	case reported == 0:
		g.State, g.Message = NoMetrics, "These volumes don't report usage; showing capacity only."
	}
	return g
}

// occupiesNode reports whether a pod holds resources on a node.
func occupiesNode(p *corev1.Pod) bool {
	return p.Spec.NodeName != "" && p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed
}

// podRequests is the scheduler's effective request: app containers plus
// sidecars, or the largest init container if that's bigger, plus overhead.
func podRequests(p *corev1.Pod) (cpu, mem float64) {
	var sidecarCPU, sidecarMem, initCPU, initMem float64
	for _, c := range p.Spec.InitContainers {
		rc, rm := requestOf(c.Resources.Requests)
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sidecarCPU += rc
			sidecarMem += rm
			continue
		}
		initCPU = max(initCPU, rc+sidecarCPU)
		initMem = max(initMem, rm+sidecarMem)
	}
	for _, c := range p.Spec.Containers {
		rc, rm := requestOf(c.Resources.Requests)
		cpu += rc
		mem += rm
	}
	cpu = max(cpu+sidecarCPU, initCPU)
	mem = max(mem+sidecarMem, initMem)
	oc, om := requestOf(p.Spec.Overhead)
	return cpu + oc, mem + om
}

func requestOf(list corev1.ResourceList) (cpu, mem float64) {
	return valueOf(list, corev1.ResourceCPU), valueOf(list, corev1.ResourceMemory)
}

func valueOf(list corev1.ResourceList, name corev1.ResourceName) float64 {
	q, ok := list[name]
	if !ok {
		return 0
	}
	return (&q).AsApproximateFloat64()
}
