// Package summary turns Kubernetes objects into the small, plain-language
// shapes the UI shows: problems that need attention, pod and event rows.
package summary

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Severity of a problem. Critical uses the rose color; high uses ink.
type Severity string

// Severities, most severe first.
const (
	Critical Severity = "critical"
	High     Severity = "high"
)

// Problem is one row in "Needs attention".
type Problem struct {
	Severity Severity `json:"severity"`
	// Reason is the Kubernetes term, e.g. CrashLoopBackOff.
	Reason string `json:"reason"`
	// Title says the same in plain words, e.g. "Keeps restarting".
	Title string `json:"title"`
	// Detail is the short secondary line, e.g. "restarted 14 times · exit code 1".
	Detail string `json:"detail"`
	// Hint is a suggested next step (shown in Glance mode).
	Hint      string    `json:"hint"`
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name"`
	Since     time.Time `json:"since"`
}

const (
	// pendingGrace avoids flagging pods that are simply starting up.
	pendingGrace = 2 * time.Minute
	// notReadyGrace is how long a running pod may fail readiness before it's a problem.
	notReadyGrace = 5 * time.Minute
	// recentOOM is how long an OOMKilled restart stays worth showing.
	recentOOM = time.Hour
)

// PodProblem returns the most important problem with pod, if any.
func PodProblem(pod *corev1.Pod, now time.Time) (Problem, bool) {
	if pod.DeletionTimestamp != nil {
		return Problem{}, false
	}
	base := Problem{Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, Since: pod.CreationTimestamp.Time}

	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for i := range statuses {
		if p, ok := containerProblem(pod, &statuses[i], base, now); ok {
			return p, true
		}
	}

	switch pod.Status.Phase {
	case corev1.PodFailed:
		p := base
		p.Severity = Critical
		p.Reason = orDefault(pod.Status.Reason, "Failed")
		p.Title = "Stopped with an error"
		if p.Reason == "Evicted" {
			p.Title = "Was evicted from its node"
		}
		p.Detail = shorten(pod.Status.Message)
		p.Hint = "Check its events and logs."
		if pod.Status.StartTime != nil {
			p.Since = pod.Status.StartTime.Time
		}
		return p, true

	case corev1.PodPending:
		if now.Sub(pod.CreationTimestamp.Time) < pendingGrace {
			return Problem{}, false
		}
		p := base
		p.Severity = High
		p.Reason = "Pending"
		if c := condition(pod, corev1.PodScheduled); c != nil && c.Status == corev1.ConditionFalse {
			p.Title = "Waiting for a node"
			p.Detail = unschedulableDetail(pod, c.Message)
			p.Hint = "Free up room, add a node, or lower its requests."
			return p, true
		}
		p.Title = "Stuck starting"
		p.Detail = "pending for " + humanDuration(now.Sub(pod.CreationTimestamp.Time))
		p.Hint = "Check its events for volume or image problems."
		return p, true

	case corev1.PodRunning:
		c := condition(pod, corev1.PodReady)
		if c != nil && c.Status == corev1.ConditionFalse && now.Sub(c.LastTransitionTime.Time) >= notReadyGrace {
			p := base
			p.Severity = High
			p.Reason = "NotReady"
			p.Title = "Running but not ready"
			p.Detail = "readiness check failing for " + humanDuration(now.Sub(c.LastTransitionTime.Time))
			p.Hint = "Check its readiness probe and logs."
			p.Since = c.LastTransitionTime.Time
			return p, true
		}
	}
	return Problem{}, false
}

func containerProblem(pod *corev1.Pod, cs *corev1.ContainerStatus, base Problem, now time.Time) (Problem, bool) {
	p := base
	p.Severity = Critical
	last := cs.LastTerminationState.Terminated

	if last != nil && last.Reason == "OOMKilled" && now.Sub(last.FinishedAt.Time) <= recentOOM {
		p.Reason = "OOMKilled"
		p.Title = "Ran out of memory"
		p.Detail = "ran out of memory"
		if limit, ok := memoryLimit(pod, cs.Name); ok {
			p.Detail = "hit its " + limit + " memory limit"
		}
		p.Hint = "Raise its memory limit, or find what uses the memory."
		p.Since = last.FinishedAt.Time
		return p, true
	}

	w := cs.State.Waiting
	if w == nil {
		return Problem{}, false
	}
	switch w.Reason {
	case "CrashLoopBackOff":
		p.Reason = w.Reason
		p.Title = "Keeps restarting"
		p.Detail = fmt.Sprintf("restarted %s", plural(int(cs.RestartCount), "time"))
		if last != nil {
			p.Detail += fmt.Sprintf(" · exit code %d", last.ExitCode)
			p.Since = last.FinishedAt.Time
		}
		p.Hint = "Check the logs from the last crash."
		return p, true
	case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
		p.Reason = w.Reason
		p.Title = "Can't pull its image"
		p.Detail = "can't pull " + cs.Image
		p.Hint = "Check the image name and tag, and the registry credentials."
		return p, true
	case "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
		p.Reason = w.Reason
		p.Title = "Can't start its container"
		p.Detail = shorten(w.Message)
		p.Hint = "Check that the ConfigMaps and Secrets it uses exist."
		return p, true
	}
	return Problem{}, false
}

// NodeProblems lists a node's problems: not ready (critical) and pressure (high).
func NodeProblems(node *corev1.Node) []Problem {
	var out []Problem
	for _, c := range node.Status.Conditions {
		base := Problem{Kind: "Node", Name: node.Name, Since: c.LastTransitionTime.Time}
		switch {
		case c.Type == corev1.NodeReady && c.Status != corev1.ConditionTrue:
			base.Severity = Critical
			base.Reason = "NotReady"
			base.Title = "Node isn't ready"
			base.Detail = orDefault(shorten(c.Message), "the kubelet stopped reporting")
			base.Hint = "Check the node's kubelet and network."
			out = append(out, base)
		case pressure[c.Type] != "" && c.Status == corev1.ConditionTrue:
			base.Severity = High
			base.Reason = string(c.Type)
			base.Title = "Node is low on " + pressure[c.Type]
			base.Detail = shorten(c.Message)
			base.Hint = "Free up " + pressure[c.Type] + " or move pods off the node."
			out = append(out, base)
		}
	}
	return out
}

var pressure = map[corev1.NodeConditionType]string{
	corev1.NodeMemoryPressure: "memory",
	corev1.NodeDiskPressure:   "disk",
	corev1.NodePIDPressure:    "process ids",
}

// JobProblem reports a failed job.
func JobProblem(job *batchv1.Job) (Problem, bool) {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return Problem{
				Severity:  Critical,
				Reason:    orDefault(c.Reason, "Failed"),
				Title:     "Job failed",
				Detail:    orDefault(shorten(c.Message), "the job failed"),
				Hint:      "Check the logs of its last pod.",
				Kind:      "Job",
				Namespace: job.Namespace,
				Name:      job.Name,
				Since:     c.LastTransitionTime.Time,
			}, true
		}
	}
	return Problem{}, false
}

var insufficient = regexp.MustCompile(`Insufficient (cpu|memory)`)

func unschedulableDetail(pod *corev1.Pod, message string) string {
	m := insufficient.FindStringSubmatch(message)
	if m == nil {
		return orDefault(shorten(message), "no node fits it")
	}
	name := corev1.ResourceName(m[1])
	total := resource.Quantity{}
	for _, c := range pod.Spec.Containers {
		if q, ok := c.Resources.Requests[name]; ok {
			total.Add(q)
		}
	}
	word := "memory"
	if name == corev1.ResourceCPU {
		word = "CPU"
	}
	if total.IsZero() {
		return "no node has enough " + word + " free"
	}
	return "no node has " + total.String() + " " + word + " free"
}

func memoryLimit(pod *corev1.Pod, container string) (string, bool) {
	for _, list := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, c := range list {
			if c.Name != container {
				continue
			}
			if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
				return q.String(), true
			}
		}
	}
	return "", false
}

func condition(pod *corev1.Pod, t corev1.PodConditionType) *corev1.PodCondition {
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == t {
			return &pod.Status.Conditions[i]
		}
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// shorten keeps the first line of a message, at most 120 characters.
func shorten(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:119]) + "…"
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return plural(int(d.Seconds()), "second")
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}
