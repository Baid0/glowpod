package summary

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// PodHealth buckets a pod for the "running · pending · failing" line.
type PodHealth string

// Pod health buckets.
const (
	Running PodHealth = "running"
	Pending PodHealth = "pending"
	Failing PodHealth = "failing"
	// Done covers Succeeded and Unknown pods, which aren't counted.
	Done PodHealth = "done"
)

// HealthOf buckets pod. A critical problem makes it failing whatever its phase.
func HealthOf(pod *corev1.Pod, now time.Time) PodHealth {
	if p, ok := PodProblem(pod, now); ok && p.Severity == Critical {
		return Failing
	}
	switch pod.Status.Phase {
	case corev1.PodRunning:
		return Running
	case corev1.PodPending:
		return Pending
	case corev1.PodFailed:
		return Failing
	default:
		return Done
	}
}

// Pod is a pod as the live stream sends it.
type Pod struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Phase     string    `json:"phase"`
	Health    PodHealth `json:"health"`
	Ready     string    `json:"ready"`
	Restarts  int32     `json:"restarts"`
	Node      string    `json:"node,omitempty"`
	Problem   *Problem  `json:"problem,omitempty"`
}

// PodOf summarises pod.
func PodOf(pod *corev1.Pod, now time.Time) Pod {
	var ready int
	var restarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += cs.RestartCount
	}
	out := Pod{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Phase:     string(pod.Status.Phase),
		Health:    HealthOf(pod, now),
		Ready:     fmt.Sprintf("%d/%d", ready, len(pod.Spec.Containers)),
		Restarts:  restarts,
		Node:      pod.Spec.NodeName,
	}
	if p, ok := PodProblem(pod, now); ok {
		out.Problem = &p
	}
	return out
}

// ObjectRef points at the object an event is about.
type ObjectRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Event is one row in "Recent events".
type Event struct {
	ID string `json:"id"`
	// Type is Normal or Warning.
	Type    string    `json:"type"`
	Reason  string    `json:"reason"`
	Message string    `json:"message"`
	Object  ObjectRef `json:"object"`
	Count   int32     `json:"count"`
	Time    time.Time `json:"time"`
}

// EventOf summarises a core/v1 event.
func EventOf(e *corev1.Event) Event {
	count := e.Count
	if e.Series != nil && e.Series.Count > count {
		count = e.Series.Count
	}
	if count == 0 {
		count = 1
	}
	return Event{
		ID:      string(e.UID),
		Type:    orDefault(e.Type, corev1.EventTypeNormal),
		Reason:  e.Reason,
		Message: shorten(e.Message),
		Object: ObjectRef{
			Kind:      e.InvolvedObject.Kind,
			Namespace: e.InvolvedObject.Namespace,
			Name:      e.InvolvedObject.Name,
		},
		Count: count,
		Time:  EventTime(e),
	}
}

// EventTime is when an event last happened, whichever field the emitter filled.
func EventTime(e *corev1.Event) time.Time {
	switch {
	case e.Series != nil && !e.Series.LastObservedTime.IsZero():
		return e.Series.LastObservedTime.Time
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp.Time
	default:
		return e.CreationTimestamp.Time
	}
}
