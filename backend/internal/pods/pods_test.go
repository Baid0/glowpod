package pods

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	"glowpod/internal/metrics"
	"glowpod/internal/summary"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func at(minAgo int) metav1.Time { return metav1.NewTime(now.Add(-time.Duration(minAgo) * time.Minute)) }

func ptr[T any](v T) *T { return &v }

func crashingPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "payments", Name: "api-7d9f6b-x2kq9", UID: "pod-uid", CreationTimestamp: at(60),
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7d9f6b", Controller: ptr(true)}},
		},
		Spec: corev1.PodSpec{
			NodeName: "w1",
			Containers: []corev1.Container{{
				Name: "app", Image: "ghcr.io/acme/api:v3",
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
				},
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromString("http")},
				}},
			}},
			InitContainers: []corev1.Container{
				{Name: "migrate", Image: "ghcr.io/acme/migrate:v3"},
				{Name: "proxy", Image: "envoy:v1", RestartPolicy: ptr(corev1.ContainerRestartPolicyAlways)},
			},
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"}}},
				{Name: "cfg", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-config"}}}},
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning, PodIP: "10.244.1.7", QOSClass: corev1.PodQOSBurstable,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: at(59)},
				{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: at(40), Message: "containers with unready status: [app]"},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{Name: "migrate", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed", FinishedAt: at(58)}}},
				{Name: "proxy", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: at(58)}}},
			},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app", RestartCount: 14,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1, Reason: "Error", FinishedAt: at(2), Message: "missing env DB_URL",
				}},
			}},
		},
	}
}

func events() []summary.Event {
	return []summary.Event{
		{Reason: "Scheduled", Type: "Normal", Time: at(59).Time, Count: 1},
		{Reason: "Pulled", Type: "Normal", Message: `Successfully pulled image "ghcr.io/acme/api:v3" in 2s`, Time: at(57).Time, Count: 1},
		{Reason: "Started", Type: "Normal", Message: "Started container app", Time: at(57).Time, Count: 1},
		{Reason: "Unhealthy", Type: "Warning", Message: "Readiness probe failed: connection refused", Time: at(41).Time, Count: 3},
		{Reason: "BackOff", Type: "Warning", Message: "Back-off restarting failed container app", Time: at(20).Time, Count: 9},
		{Reason: "BackOff", Type: "Warning", Message: "Back-off restarting failed container app", Time: at(5).Time, Count: 5},
		{Reason: "FailedMount", Type: "Warning", Message: `MountVolume.SetUp failed for volume "cfg"`, Time: at(50).Time, Count: 1},
	}
}

func TestStory(t *testing.T) {
	got := Story(crashingPod(), events(), now)

	var titles []string
	for _, s := range got {
		titles = append(titles, string(s.Tone)+": "+s.Title)
	}
	want := []string{
		"info: Created by ReplicaSet api-7d9f6b",
		"info: Scheduled on w1",
		"info: Container migrate finished (exit code 0)",
		"ok: Container proxy is running",
		"info: Image ghcr.io/acme/api:v3 ready",
		"info: Started container app",
		"critical: Failed mount (FailedMount)",
		"high: Readiness probe failed",
		"high: Not ready",
		"critical: Back-off restarting a failed container (BackOff)",
		"critical: Container app exited with code 1 (Error)",
		"critical: Container app keeps restarting (CrashLoopBackOff)",
	}
	if strings.Join(titles, "\n") != strings.Join(want, "\n") {
		t.Fatalf("story:\n%s\n\nwant:\n%s", strings.Join(titles, "\n"), strings.Join(want, "\n"))
	}

	// Two back-to-back BackOff events collapse into one step with their counts.
	for _, s := range got {
		if strings.Contains(s.Title, "Back-off") && s.Count != 14 {
			t.Fatalf("BackOff count = %d, want 14", s.Count)
		}
		if strings.Contains(s.Title, "exited with code 1") && s.Detail != "missing env DB_URL" {
			t.Fatalf("exit detail = %q", s.Detail)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Time.Before(got[i-1].Time) {
			t.Fatalf("story is out of order at %d", i)
		}
	}
}

func TestStoryOOMAndPending(t *testing.T) {
	oom := crashingPod()
	oom.Status.ContainerStatuses[0].LastTerminationState.Terminated.Reason = "OOMKilled"
	oom.Status.ContainerStatuses[0].LastTerminationState.Terminated.ExitCode = 137
	if !hasTitle(Story(oom, nil, now), "Container app ran out of memory (OOMKilled)") {
		t.Fatal("OOMKilled not told")
	}

	pending := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "search-indexer-0", CreationTimestamp: at(6)},
		Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, LastTransitionTime: at(6), Message: "0/3 nodes are available: 3 Insufficient memory."},
		}},
	}
	story := Story(pending, nil, now)
	if len(story) != 2 || story[1].Title != "Waiting for a node" || story[1].Tone != ToneHigh || !strings.Contains(story[1].Detail, "Insufficient memory") {
		t.Fatalf("pending story = %+v", story)
	}
}

func hasTitle(items []StoryItem, title string) bool {
	for _, s := range items {
		if s.Title == title {
			return true
		}
	}
	return false
}

func TestWords(t *testing.T) {
	for in, want := range map[string]string{
		"FailedMount":    "Failed mount (FailedMount)",
		"NodeNotReady":   "Node not ready (NodeNotReady)",
		"Evicted":        "Evicted",
		"":               "Something happened",
		"SandboxChanged": "Sandbox changed (SandboxChanged)",
	} {
		if got := words(in); got != want {
			t.Errorf("words(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeUsage struct {
	m   map[string]metrics.Usage
	err error
}

func (f fakeUsage) ContainerUsage(context.Context, string, string) (map[string]metrics.Usage, error) {
	return f.m, f.err
}

func TestBuild(t *testing.T) {
	pod := crashingPod()
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Namespace: "payments", Name: "api-7d9f6b",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Controller: ptr(true)}},
	}}
	ev := func(name string, uid types.UID, reason string) *corev1.Event {
		return &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "payments", Name: name},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: pod.Name, Namespace: "payments", UID: uid},
			Reason:         reason, Type: "Warning", LastTimestamp: at(5), Count: 2,
		}
	}
	kube := fake.NewClientset(pod, rs, ev("e1", "pod-uid", "BackOff"), ev("e0", "old-pod-uid", "Killing"))

	tests := []struct {
		name        string
		usage       UsageSource
		wantMetrics bool
	}{
		{name: "with metrics", usage: fakeUsage{m: map[string]metrics.Usage{"app": {CPU: 0.1, Memory: 100 << 20}}}, wantMetrics: true},
		{name: "without metrics", usage: fakeUsage{err: metrics.ErrNoMetrics}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Build(context.Background(), kube, tt.usage, "payments", pod.Name, now)
			if err != nil {
				t.Fatal(err)
			}
			if d.Owner == nil || d.Owner.Kind != "Deployment" || d.Owner.Name != "api" || d.Owner.Resource != "deployments" || d.Owner.Group != "apps" {
				t.Fatalf("owner = %+v (should trace through the ReplicaSet)", d.Owner)
			}
			if d.Pod.Problem == nil || d.Pod.Problem.Reason != "CrashLoopBackOff" || d.IP != "10.244.1.7" || d.QOSClass != "Burstable" {
				t.Fatalf("pod = %+v", d.Pod)
			}
			roles := []string{}
			for _, c := range d.Containers {
				roles = append(roles, c.Role+":"+c.Name+":"+c.State)
			}
			if strings.Join(roles, ",") != "init:migrate:terminated,sidecar:proxy:running,app:app:waiting" {
				t.Fatalf("containers = %v", roles)
			}
			app := d.Containers[2]
			if app.Reason != "CrashLoopBackOff" || app.Restarts != 14 || app.Last == nil || app.Last.ExitCode != 1 ||
				*app.Limits.Memory != 512<<20 || *app.Requests.CPU != 0.25 || app.Limits.CPU != nil ||
				len(app.Probes) != 1 || app.Probes[0].Action != "GET /ready on port http" || app.Ports[0].Port != 8080 {
				t.Fatalf("app container = %+v", app)
			}
			if (app.Usage != nil) != tt.wantMetrics || d.MetricsAvailable != tt.wantMetrics {
				t.Fatalf("usage = %+v, metrics = %v", app.Usage, d.MetricsAvailable)
			}
			if len(d.Events) != 1 || d.Events[0].Reason != "BackOff" {
				t.Fatalf("events = %+v (old pod's events must be dropped)", d.Events)
			}
			if len(d.Volumes) != 3 || d.Volumes[0] != (Volume{Name: "data", Kind: "persistentVolumeClaim", Source: "api-data"}) {
				t.Fatalf("volumes = %+v", d.Volumes)
			}
			if len(d.Story) == 0 || d.Story[len(d.Story)-1].Tone != ToneCritical {
				t.Fatalf("story = %+v", d.Story)
			}
		})
	}

	if _, err := Build(context.Background(), kube, nil, "payments", "gone", now); err == nil {
		t.Fatal("want an error for a missing pod")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}
