// Package pods builds the pod detail page: the pod's facts, its containers
// with live usage, conditions, volumes, events, and a plain-language story
// of what happened to it.
package pods

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/metrics"
	"glowpod/internal/summary"
)

// UsageSource reads one pod's per-container usage. *metrics.Reader implements it.
type UsageSource interface {
	ContainerUsage(ctx context.Context, namespace, name string) (map[string]metrics.Usage, error)
}

// Detail is GET /api/v1/clusters/{cluster}/pods/{namespace}/{name}.
type Detail struct {
	Pod        summary.Pod     `json:"pod"`
	CreatedAt  time.Time       `json:"createdAt"`
	IP         string          `json:"ip,omitempty"`
	QOSClass   string          `json:"qosClass,omitempty"`
	Owner      *Owner          `json:"owner,omitempty"`
	Containers []Container     `json:"containers"`
	Conditions []Condition     `json:"conditions"`
	Volumes    []Volume        `json:"volumes"`
	Events     []summary.Event `json:"events"`
	Story      []StoryItem     `json:"story"`
	// MetricsAvailable is false when metrics-server can't be reached.
	MetricsAvailable bool `json:"metricsAvailable"`
}

// Owner is the workload that runs the pod, traced through its ReplicaSet.
type Owner struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

// Resources are CPU in cores and memory in bytes; absent when unset.
type Resources struct {
	CPU    *float64 `json:"cpu,omitempty"`
	Memory *float64 `json:"memory,omitempty"`
}

// Termination is how a container last stopped.
type Termination struct {
	Reason     string    `json:"reason,omitempty"`
	ExitCode   int32     `json:"exitCode"`
	FinishedAt time.Time `json:"finishedAt"`
}

// Container is one container (init, sidecar or app).
type Container struct {
	Name string `json:"name"`
	// Role is init, sidecar or app.
	Role     string       `json:"role"`
	Image    string       `json:"image"`
	State    string       `json:"state"` // running, waiting, terminated or unknown
	Reason   string       `json:"reason,omitempty"`
	Message  string       `json:"message,omitempty"`
	ExitCode *int32       `json:"exitCode,omitempty"`
	Since    *time.Time   `json:"since,omitempty"`
	Ready    bool         `json:"ready"`
	Restarts int32        `json:"restarts"`
	Last     *Termination `json:"lastTermination,omitempty"`
	Requests Resources    `json:"requests"`
	Limits   Resources    `json:"limits"`
	Usage    *Resources   `json:"usage,omitempty"`
	Ports    []Port       `json:"ports,omitempty"`
	Probes   []Probe      `json:"probes,omitempty"`
}

// Port is a declared container port.
type Port struct {
	Name     string `json:"name,omitempty"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// Probe summarises a liveness, readiness or startup probe.
type Probe struct {
	Kind   string `json:"kind"`
	Action string `json:"action"`
	Every  int32  `json:"periodSeconds"`
}

// Condition is a pod condition.
type Condition struct {
	Type    string     `json:"type"`
	Status  string     `json:"status"`
	Reason  string     `json:"reason,omitempty"`
	Message string     `json:"message,omitempty"`
	Since   *time.Time `json:"since,omitempty"`
}

// Volume is a mounted volume and where it comes from.
type Volume struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
}

// Build reads the pod, its events, owner and usage. Only the pod read is
// required; the rest degrade quietly.
func Build(ctx context.Context, kube kubernetes.Interface, usage UsageSource, namespace, name string, now time.Time) (*Detail, error) {
	pod, err := kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
	}

	var events []corev1.Event
	if list, err := kube.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fields.Set{
			"involvedObject.kind": "Pod",
			"involvedObject.name": name,
		}.AsSelector().String(),
		Limit: 200,
	}); err == nil {
		events = list.Items
	}

	d := &Detail{
		Pod:        summary.PodOf(pod, now),
		CreatedAt:  pod.CreationTimestamp.UTC(),
		IP:         pod.Status.PodIP,
		QOSClass:   string(pod.Status.QOSClass),
		Owner:      owner(ctx, kube, pod),
		Containers: containers(pod),
		Conditions: conditions(pod),
		Volumes:    volumes(pod),
		Events:     make([]summary.Event, 0, len(events)),
	}
	for i := range events {
		if events[i].InvolvedObject.UID != "" && events[i].InvolvedObject.UID != pod.UID {
			continue // an older pod with the same name
		}
		d.Events = append(d.Events, summary.EventOf(&events[i]))
	}
	slices.SortFunc(d.Events, func(a, b summary.Event) int { return b.Time.Compare(a.Time) })

	if usage != nil {
		if u, err := usage.ContainerUsage(ctx, namespace, name); err == nil {
			d.MetricsAvailable = true
			for i := range d.Containers {
				if cu, ok := u[d.Containers[i].Name]; ok {
					cpu, mem := cu.CPU, cu.Memory
					d.Containers[i].Usage = &Resources{CPU: &cpu, Memory: &mem}
				}
			}
		}
	}

	d.Story = Story(pod, d.Events, now)
	return d, nil
}

// owner finds the pod's controller, and the Deployment behind a ReplicaSet.
func owner(ctx context.Context, kube kubernetes.Interface, pod *corev1.Pod) *Owner {
	ref := metav1.GetControllerOf(pod)
	if ref == nil {
		return nil
	}
	o := ownerFromRef(ref.APIVersion, ref.Kind, ref.Name)
	if ref.Kind == "ReplicaSet" {
		if rs, err := kube.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ref.Name, metav1.GetOptions{}); err == nil {
			if dep := metav1.GetControllerOf(rs); dep != nil && dep.Kind == "Deployment" {
				o = ownerFromRef(dep.APIVersion, dep.Kind, dep.Name)
			}
		}
	}
	return o
}

func ownerFromRef(apiVersion, kind, name string) *Owner {
	group, version, found := strings.Cut(apiVersion, "/")
	if !found {
		group, version = "", apiVersion
	}
	return &Owner{Group: group, Version: version, Resource: strings.ToLower(kind) + "s", Kind: kind, Name: name}
}

func containers(pod *corev1.Pod) []Container {
	statuses := map[string]corev1.ContainerStatus{}
	for _, s := range append(slices.Clone(pod.Status.InitContainerStatuses), pod.Status.ContainerStatuses...) {
		statuses[s.Name] = s
	}
	var out []Container
	add := func(c corev1.Container, role string) {
		out = append(out, container(c, role, statuses[c.Name]))
	}
	for _, c := range pod.Spec.InitContainers {
		role := "init"
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			role = "sidecar"
		}
		add(c, role)
	}
	for _, c := range pod.Spec.Containers {
		add(c, "app")
	}
	return out
}

func container(c corev1.Container, role string, s corev1.ContainerStatus) Container {
	out := Container{
		Name:     c.Name,
		Role:     role,
		Image:    c.Image,
		State:    "unknown",
		Ready:    s.Ready,
		Restarts: s.RestartCount,
		Requests: resources(c.Resources.Requests),
		Limits:   resources(c.Resources.Limits),
	}
	switch {
	case s.State.Running != nil:
		out.State = "running"
		t := s.State.Running.StartedAt.UTC()
		out.Since = &t
	case s.State.Waiting != nil:
		out.State = "waiting"
		out.Reason = s.State.Waiting.Reason
		out.Message = s.State.Waiting.Message
	case s.State.Terminated != nil:
		t := s.State.Terminated
		out.State = "terminated"
		out.Reason = t.Reason
		out.Message = t.Message
		code := t.ExitCode
		out.ExitCode = &code
		ft := t.FinishedAt.UTC()
		out.Since = &ft
	}
	if t := s.LastTerminationState.Terminated; t != nil {
		out.Last = &Termination{Reason: t.Reason, ExitCode: t.ExitCode, FinishedAt: t.FinishedAt.UTC()}
	}
	for _, p := range c.Ports {
		out.Ports = append(out.Ports, Port{Name: p.Name, Port: p.ContainerPort, Protocol: string(p.Protocol)})
	}
	for _, p := range []struct {
		kind  string
		probe *corev1.Probe
	}{{"liveness", c.LivenessProbe}, {"readiness", c.ReadinessProbe}, {"startup", c.StartupProbe}} {
		if p.probe != nil {
			out.Probes = append(out.Probes, Probe{Kind: p.kind, Action: probeAction(p.probe), Every: max(p.probe.PeriodSeconds, 10)})
		}
	}
	return out
}

func probeAction(p *corev1.Probe) string {
	switch {
	case p.HTTPGet != nil:
		return fmt.Sprintf("GET %s on port %s", cmp.Or(p.HTTPGet.Path, "/"), p.HTTPGet.Port.String())
	case p.TCPSocket != nil:
		return "TCP port " + p.TCPSocket.Port.String()
	case p.GRPC != nil:
		return fmt.Sprintf("gRPC port %d", p.GRPC.Port)
	case p.Exec != nil:
		return "runs " + strings.Join(p.Exec.Command, " ")
	default:
		return "unknown"
	}
}

func resources(list corev1.ResourceList) Resources {
	var r Resources
	if q, ok := list[corev1.ResourceCPU]; ok {
		v := quantity(q)
		r.CPU = &v
	}
	if q, ok := list[corev1.ResourceMemory]; ok {
		v := quantity(q)
		r.Memory = &v
	}
	return r
}

func quantity(q resource.Quantity) float64 { return q.AsApproximateFloat64() }

func conditions(pod *corev1.Pod) []Condition {
	out := make([]Condition, 0, len(pod.Status.Conditions))
	for _, c := range pod.Status.Conditions {
		cond := Condition{Type: string(c.Type), Status: string(c.Status), Reason: c.Reason, Message: c.Message}
		if !c.LastTransitionTime.IsZero() {
			t := c.LastTransitionTime.UTC()
			cond.Since = &t
		}
		out = append(out, cond)
	}
	return out
}

func volumes(pod *corev1.Pod) []Volume {
	out := make([]Volume, 0, len(pod.Spec.Volumes))
	for _, v := range pod.Spec.Volumes {
		vol := Volume{Name: v.Name, Kind: "other"}
		switch {
		case v.PersistentVolumeClaim != nil:
			vol.Kind, vol.Source = "persistentVolumeClaim", v.PersistentVolumeClaim.ClaimName
		case v.ConfigMap != nil:
			vol.Kind, vol.Source = "configMap", v.ConfigMap.Name
		case v.Secret != nil:
			vol.Kind, vol.Source = "secret", v.Secret.SecretName
		case v.EmptyDir != nil:
			vol.Kind = "emptyDir"
		case v.HostPath != nil:
			vol.Kind, vol.Source = "hostPath", v.HostPath.Path
		case v.Projected != nil:
			vol.Kind = "projected"
		case v.DownwardAPI != nil:
			vol.Kind = "downwardAPI"
		}
		out = append(out, vol)
	}
	return out
}
