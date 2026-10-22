package pods

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	corev1 "k8s.io/api/core/v1"

	"glowpod/internal/summary"
)

// Tone of a story step, matching the status colors.
type Tone string

// Story tones.
const (
	ToneOK       Tone = "ok"
	ToneInfo     Tone = "info"
	ToneHigh     Tone = "high"
	ToneCritical Tone = "critical"
)

// StoryItem is one step in "what happened", oldest first.
type StoryItem struct {
	Time   time.Time `json:"time"`
	Tone   Tone      `json:"tone"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	// Count is how many times this step repeated in a row.
	Count int `json:"count"`
}

// Event reasons that the pod's own status already tells better.
var skipReasons = map[string]bool{
	"Scheduled": true, "Pulling": true, "Created": true,
}

var criticalReasons = map[string]bool{
	"BackOff": true, "Failed": true, "FailedMount": true, "FailedAttachVolume": true,
	"Evicted": true, "OOMKilling": true, "FailedCreatePodSandBox": true, "FailedKillPod": true,
}

var quoted = regexp.MustCompile(`"([^"]+)"`)

// Story tells what happened to a pod in order: created, scheduled, images
// pulled, containers started, crashes and restarts, readiness, and where it
// stands now. Repeats in a row collapse into one step with a count.
func Story(pod *corev1.Pod, events []summary.Event, now time.Time) []StoryItem {
	var items []StoryItem
	add := func(t time.Time, tone Tone, title, detail string) {
		if t.IsZero() {
			return
		}
		items = append(items, StoryItem{Time: t.UTC(), Tone: tone, Title: title, Detail: detail, Count: 1})
	}

	created := "Created"
	if ref := pod.OwnerReferences; len(ref) > 0 {
		created = fmt.Sprintf("Created by %s %s", ref[0].Kind, ref[0].Name)
	}
	add(pod.CreationTimestamp.Time, ToneInfo, created, "")

	for _, c := range pod.Status.Conditions {
		at := c.LastTransitionTime.Time
		switch {
		case c.Type == corev1.PodScheduled && c.Status == corev1.ConditionTrue:
			add(at, ToneInfo, "Scheduled on "+pod.Spec.NodeName, "")
		case c.Type == corev1.PodScheduled:
			add(at, ToneHigh, "Waiting for a node", c.Message)
		case c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue:
			add(at, ToneOK, "Ready to serve traffic", "")
		case c.Type == corev1.PodReady && pod.Status.Phase == corev1.PodRunning:
			add(at, ToneHigh, "Not ready", c.Message)
		}
	}

	for _, s := range append(slices.Clone(pod.Status.InitContainerStatuses), pod.Status.ContainerStatuses...) {
		if t := s.LastTerminationState.Terminated; t != nil {
			add(t.FinishedAt.Time, exitTone(t), exitTitle(s.Name, t), t.Message)
		}
		switch {
		case s.State.Running != nil:
			add(s.State.Running.StartedAt.Time, ToneOK, "Container "+s.Name+" is running", "")
		case s.State.Terminated != nil:
			t := s.State.Terminated
			add(t.FinishedAt.Time, exitTone(t), exitTitle(s.Name, t), t.Message)
		case s.State.Waiting != nil && s.State.Waiting.Reason == "CrashLoopBackOff":
			add(now, ToneCritical, fmt.Sprintf("Container %s keeps restarting (CrashLoopBackOff)", s.Name),
				fmt.Sprintf("Restarted %d times so far. Kubernetes waits longer between each try.", s.RestartCount))
		case s.State.Waiting != nil && s.State.Waiting.Reason != "ContainerCreating" && s.State.Waiting.Reason != "PodInitializing":
			add(now, ToneCritical, fmt.Sprintf("Container %s is stuck (%s)", s.Name, s.State.Waiting.Reason), s.State.Waiting.Message)
		}
	}

	for _, e := range events {
		if skipReasons[e.Reason] {
			continue
		}
		tone, title := eventStep(e)
		item := StoryItem{Time: e.Time.UTC(), Tone: tone, Title: title, Detail: e.Message, Count: int(e.Count)}
		if item.Time.IsZero() {
			continue
		}
		items = append(items, item)
	}

	slices.SortStableFunc(items, func(a, b StoryItem) int { return a.Time.Compare(b.Time) })
	return collapse(items)
}

func exitTone(t *corev1.ContainerStateTerminated) Tone {
	if t.ExitCode == 0 && t.Reason != "OOMKilled" {
		return ToneInfo
	}
	return ToneCritical
}

func exitTitle(name string, t *corev1.ContainerStateTerminated) string {
	switch {
	case t.Reason == "OOMKilled":
		return fmt.Sprintf("Container %s ran out of memory (OOMKilled)", name)
	case t.ExitCode == 0:
		return fmt.Sprintf("Container %s finished (exit code 0)", name)
	default:
		return fmt.Sprintf("Container %s exited with code %d (%s)", name, t.ExitCode, orDefault(t.Reason, "Error"))
	}
}

func eventStep(e summary.Event) (Tone, string) {
	switch e.Reason {
	case "Pulled":
		if m := quoted.FindStringSubmatch(e.Message); m != nil {
			return ToneInfo, "Image " + m[1] + " ready"
		}
		return ToneInfo, "Image ready"
	case "Started":
		return ToneInfo, strings.TrimSuffix(e.Message, ".")
	case "Killing":
		return ToneInfo, "Stopping a container"
	case "Unhealthy":
		return ToneHigh, firstSentence(e.Message)
	case "BackOff":
		return ToneCritical, "Back-off restarting a failed container (BackOff)"
	case "FailedScheduling":
		return ToneHigh, "Couldn't be placed on a node (FailedScheduling)"
	}
	tone := ToneInfo
	if e.Type == corev1.EventTypeWarning {
		tone = ToneHigh
	}
	if criticalReasons[e.Reason] {
		tone = ToneCritical
	}
	return tone, words(e.Reason)
}

// collapse merges a step with the one before it when they say the same thing.
func collapse(items []StoryItem) []StoryItem {
	out := make([]StoryItem, 0, len(items))
	for _, it := range items {
		if n := len(out); n > 0 && out[n-1].Title == it.Title && out[n-1].Tone == it.Tone {
			out[n-1].Count += it.Count
			out[n-1].Time = it.Time
			if it.Detail != "" {
				out[n-1].Detail = it.Detail
			}
			continue
		}
		out = append(out, it)
	}
	return out
}

// words turns "FailedMount" into "Failed mount (FailedMount)".
func words(reason string) string {
	if reason == "" {
		return "Something happened"
	}
	var b strings.Builder
	for i, r := range reason {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteRune(' ')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	if b.String() == reason {
		return reason
	}
	return b.String() + " (" + reason + ")"
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ":\n"); i > 0 {
		s = s[:i]
	}
	return s
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
