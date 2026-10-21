package summary

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) metav1.Time { return metav1.NewTime(now.Add(-d)) }

type podOpt func(*corev1.Pod)

func pod(phase corev1.PodPhase, opts ...podOpt) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "payments-api-7d9f6b-x2kq9", CreationTimestamp: ago(time.Hour)},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "api",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("6Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
			},
		}}},
		Status: corev1.PodStatus{Phase: phase},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func container(cs corev1.ContainerStatus) podOpt {
	return func(p *corev1.Pod) {
		cs.Name = "api"
		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, cs)
	}
}

func waiting(reason, msg string) corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: msg}}
}

func terminated(reason string, code int32, finished time.Duration) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code, FinishedAt: ago(finished)}}
}

func cond(t corev1.PodConditionType, s corev1.ConditionStatus, since time.Duration, msg string) podOpt {
	return func(p *corev1.Pod) {
		p.Status.Conditions = append(p.Status.Conditions, corev1.PodCondition{Type: t, Status: s, LastTransitionTime: ago(since), Message: msg})
	}
}

func createdAgo(d time.Duration) podOpt {
	return func(p *corev1.Pod) { p.CreationTimestamp = ago(d) }
}

func TestPodProblem(t *testing.T) {
	tests := []struct {
		name       string
		pod        *corev1.Pod
		wantOK     bool
		severity   Severity
		reason     string
		detail     string
		wantHealth PodHealth
	}{
		{
			name:       "healthy running pod",
			pod:        pod(corev1.PodRunning, container(corev1.ContainerStatus{Ready: true}), cond(corev1.PodReady, corev1.ConditionTrue, time.Hour, "")),
			wantHealth: Running,
		},
		{
			name: "crash loop",
			pod: pod(corev1.PodRunning, container(corev1.ContainerStatus{
				RestartCount: 14, State: waiting("CrashLoopBackOff", "back-off"), LastTerminationState: terminated("Error", 1, 23*time.Minute),
			})),
			wantOK: true, severity: Critical, reason: "CrashLoopBackOff", detail: "restarted 14 times · exit code 1", wantHealth: Failing,
		},
		{
			name: "recent OOM wins over crash loop and names the limit",
			pod: pod(corev1.PodRunning, container(corev1.ContainerStatus{
				RestartCount: 3, State: waiting("CrashLoopBackOff", ""), LastTerminationState: terminated("OOMKilled", 137, 41*time.Minute),
			})),
			wantOK: true, severity: Critical, reason: "OOMKilled", detail: "hit its 512Mi memory limit", wantHealth: Failing,
		},
		{
			name: "old OOM is forgotten",
			pod: pod(corev1.PodRunning, container(corev1.ContainerStatus{
				Ready: true, LastTerminationState: terminated("OOMKilled", 137, 3*time.Hour),
			})),
			wantHealth: Running,
		},
		{
			name: "image pull",
			pod: pod(corev1.PodPending, container(corev1.ContainerStatus{
				Image: "ghcr.io/acme/api:v9", State: waiting("ImagePullBackOff", ""),
			})),
			wantOK: true, severity: Critical, reason: "ImagePullBackOff", detail: "can't pull ghcr.io/acme/api:v9", wantHealth: Failing,
		},
		{
			name: "missing secret",
			pod: pod(corev1.PodPending, container(corev1.ContainerStatus{
				State: waiting("CreateContainerConfigError", `secret "db" not found`),
			})),
			wantOK: true, severity: Critical, reason: "CreateContainerConfigError", detail: `secret "db" not found`, wantHealth: Failing,
		},
		{
			name: "unschedulable for memory",
			pod: pod(corev1.PodPending, createdAgo(6*time.Minute),
				cond(corev1.PodScheduled, corev1.ConditionFalse, 6*time.Minute, "0/6 nodes are available: 6 Insufficient memory.")),
			wantOK: true, severity: High, reason: "Pending", detail: "no node has 6Gi memory free", wantHealth: Pending,
		},
		{
			name:       "pending inside the grace period",
			pod:        pod(corev1.PodPending, createdAgo(30*time.Second), cond(corev1.PodScheduled, corev1.ConditionFalse, 30*time.Second, "Insufficient memory")),
			wantHealth: Pending,
		},
		{
			name:   "stuck starting",
			pod:    pod(corev1.PodPending, createdAgo(10*time.Minute)),
			wantOK: true, severity: High, reason: "Pending", detail: "pending for 10 minutes", wantHealth: Pending,
		},
		{
			name: "failed (evicted)",
			pod: pod(corev1.PodFailed, func(p *corev1.Pod) {
				p.Status.Reason = "Evicted"
				p.Status.Message = "The node was low on resource: memory."
			}),
			wantOK: true, severity: Critical, reason: "Evicted", detail: "The node was low on resource: memory.", wantHealth: Failing,
		},
		{
			name:   "not ready for a while",
			pod:    pod(corev1.PodRunning, container(corev1.ContainerStatus{}), cond(corev1.PodReady, corev1.ConditionFalse, 7*time.Minute, "")),
			wantOK: true, severity: High, reason: "NotReady", detail: "readiness check failing for 7 minutes", wantHealth: Running,
		},
		{
			name:       "not ready only briefly",
			pod:        pod(corev1.PodRunning, cond(corev1.PodReady, corev1.ConditionFalse, time.Minute, "")),
			wantHealth: Running,
		},
		{
			name: "terminating pods are ignored",
			pod: pod(corev1.PodRunning, container(corev1.ContainerStatus{State: waiting("CrashLoopBackOff", "")}), func(p *corev1.Pod) {
				ts := ago(time.Second)
				p.DeletionTimestamp = &ts
			}),
			wantHealth: Running,
		},
		{name: "succeeded", pod: pod(corev1.PodSucceeded), wantHealth: Done},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PodProblem(tt.pod, now)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (problem %+v)", ok, tt.wantOK, got)
			}
			if ok && (got.Severity != tt.severity || got.Reason != tt.reason || got.Detail != tt.detail) {
				t.Fatalf("got %s/%s %q, want %s/%s %q", got.Severity, got.Reason, got.Detail, tt.severity, tt.reason, tt.detail)
			}
			if ok && (got.Title == "" || got.Hint == "" || got.Kind != "Pod" || got.Name == "") {
				t.Fatalf("problem is missing plain-language fields: %+v", got)
			}
			if h := HealthOf(tt.pod, now); h != tt.wantHealth {
				t.Fatalf("health = %s, want %s", h, tt.wantHealth)
			}
		})
	}
}

func TestNodeProblems(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "ip-10-0-2-14"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, Message: "Kubelet stopped posting node status."},
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
			{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
		}},
	}
	got := NodeProblems(node)
	if len(got) != 2 {
		t.Fatalf("got %d problems, want 2: %+v", len(got), got)
	}
	if got[0].Severity != Critical || got[0].Reason != "NotReady" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Severity != High || got[1].Reason != "MemoryPressure" || got[1].Title != "Node is low on memory" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestJobProblem(t *testing.T) {
	failed := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ops", Name: "backup-nightly-29341"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"},
		}},
	}
	p, ok := JobProblem(failed)
	if !ok || p.Reason != "BackoffLimitExceeded" || p.Kind != "Job" {
		t.Fatalf("got %+v, %v", p, ok)
	}
	if _, ok := JobProblem(&batchv1.Job{}); ok {
		t.Fatal("a job with no conditions isn't a problem")
	}
}

func TestPodOf(t *testing.T) {
	p := pod(corev1.PodRunning,
		container(corev1.ContainerStatus{Ready: true, RestartCount: 2}),
		func(p *corev1.Pod) { p.Spec.NodeName = "w1" })
	got := PodOf(p, now)
	if got.Ready != "1/1" || got.Restarts != 2 || got.Health != Running || got.Node != "w1" || got.Problem != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestEventOf(t *testing.T) {
	tests := []struct {
		name  string
		event corev1.Event
		want  time.Time
		count int32
	}{
		{name: "last timestamp", event: corev1.Event{LastTimestamp: ago(time.Minute), Count: 4}, want: now.Add(-time.Minute), count: 4},
		{name: "event time only", event: corev1.Event{EventTime: metav1.NewMicroTime(now.Add(-3 * time.Minute))}, want: now.Add(-3 * time.Minute), count: 1},
		{name: "series", event: corev1.Event{
			EventTime: metav1.NewMicroTime(now.Add(-time.Hour)),
			Series:    &corev1.EventSeries{Count: 9, LastObservedTime: metav1.NewMicroTime(now.Add(-2 * time.Minute))},
		}, want: now.Add(-2 * time.Minute), count: 9},
		{name: "creation fallback", event: corev1.Event{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: ago(5 * time.Minute)}}, want: now.Add(-5 * time.Minute), count: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EventOf(&tt.event)
			if !got.Time.Equal(tt.want) || got.Count != tt.count || got.Type != "Normal" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
