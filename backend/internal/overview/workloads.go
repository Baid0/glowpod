package overview

import (
	"cmp"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/summary"
)

// ownerKey names a top-level workload: kind, namespace, name.
type ownerKey struct{ kind, namespace, name string }

// failingOwners returns the workloads that own a pod with a critical problem.
// Pods owned by a ReplicaSet are traced to its Deployment.
func failingOwners(pods []corev1.Pod, replicaSets []appsv1.ReplicaSet, now time.Time) map[ownerKey]bool {
	rsToDeployment := map[ownerKey]string{}
	for i := range replicaSets {
		rs := &replicaSets[i]
		if ref := metav1.GetControllerOf(rs); ref != nil && ref.Kind == "Deployment" {
			rsToDeployment[ownerKey{"ReplicaSet", rs.Namespace, rs.Name}] = ref.Name
		}
	}
	out := map[ownerKey]bool{}
	for i := range pods {
		p, ok := summary.PodProblem(&pods[i], now)
		if !ok || p.Severity != summary.Critical {
			continue
		}
		ref := metav1.GetControllerOf(&pods[i])
		if ref == nil {
			continue
		}
		key := ownerKey{ref.Kind, pods[i].Namespace, ref.Name}
		if d, ok := rsToDeployment[key]; ok {
			key = ownerKey{"Deployment", pods[i].Namespace, d}
		}
		out[key] = true
	}
	return out
}

func workloads(snap *snapshot, failing map[ownerKey]bool, now time.Time) Workloads {
	var w Workloads
	for i := range snap.deployments {
		w.Deployments.add(deploymentState(&snap.deployments[i], failing))
	}
	for i := range snap.statefulSets {
		w.StatefulSets.add(statefulSetState(&snap.statefulSets[i], failing))
	}
	for i := range snap.daemonSets {
		w.DaemonSets.add(daemonSetState(&snap.daemonSets[i], failing))
	}
	for i := range snap.jobs {
		j := &snap.jobs[i]
		if !withinJobWindow(j, now) {
			continue
		}
		switch {
		case jobCondition(j, batchv1.JobComplete):
			w.Jobs.Succeeded++
		case jobCondition(j, batchv1.JobFailed):
			w.Jobs.Failed++
		default:
			w.Jobs.Running++
		}
	}
	return w
}

func (c *WorkloadCount) add(s AppStatus) {
	c.Total++
	switch s {
	case AppOK:
		c.Ready++
	case AppHigh:
		c.Pending++
	case AppCritical:
		c.Failing++
	}
}

func replicas(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

func deploymentState(d *appsv1.Deployment, failing map[ownerKey]bool) AppStatus {
	want := replicas(d.Spec.Replicas)
	st := d.Status
	if st.ReadyReplicas >= want && st.UpdatedReplicas >= want && st.AvailableReplicas >= want {
		return AppOK
	}
	if failing[ownerKey{"Deployment", d.Namespace, d.Name}] {
		return AppCritical
	}
	for _, c := range st.Conditions {
		if (c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse) ||
			(c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue) {
			return AppCritical
		}
	}
	return AppHigh
}

func statefulSetState(s *appsv1.StatefulSet, failing map[ownerKey]bool) AppStatus {
	if s.Status.ReadyReplicas >= replicas(s.Spec.Replicas) {
		return AppOK
	}
	if failing[ownerKey{"StatefulSet", s.Namespace, s.Name}] {
		return AppCritical
	}
	return AppHigh
}

func daemonSetState(d *appsv1.DaemonSet, failing map[ownerKey]bool) AppStatus {
	if d.Status.NumberReady >= d.Status.DesiredNumberScheduled {
		return AppOK
	}
	if failing[ownerKey{"DaemonSet", d.Namespace, d.Name}] {
		return AppCritical
	}
	return AppHigh
}

func withinJobWindow(j *batchv1.Job, now time.Time) bool {
	start := j.CreationTimestamp.Time
	if j.Status.StartTime != nil {
		start = j.Status.StartTime.Time
	}
	return now.Sub(start) <= jobWindow
}

func jobCondition(j *batchv1.Job, t batchv1.JobConditionType) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == t && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// appName groups workloads into apps: app.kubernetes.io/name, then the Helm
// release, then the older "app" label, then the workload's own name.
func appName(meta *metav1.ObjectMeta) string {
	for _, v := range []string{
		meta.Labels["app.kubernetes.io/name"],
		meta.Annotations["meta.helm.sh/release-name"],
		meta.Labels["app.kubernetes.io/instance"],
		meta.Labels["app"],
	} {
		if v != "" {
			return v
		}
	}
	return meta.Name
}

func apps(snap *snapshot, failing map[ownerKey]bool) []App {
	newestRS := map[ownerKey]time.Time{}
	for i := range snap.replicaSets {
		rs := &snap.replicaSets[i]
		if ref := metav1.GetControllerOf(rs); ref != nil && ref.Kind == "Deployment" {
			k := ownerKey{"Deployment", rs.Namespace, ref.Name}
			if t := rs.CreationTimestamp.Time; t.After(newestRS[k]) {
				newestRS[k] = t
			}
		}
	}

	byKey := map[[2]string]*App{}
	add := func(meta *metav1.ObjectMeta, ready, total int32, status AppStatus, deployed time.Time) {
		key := [2]string{meta.Namespace, appName(meta)}
		a, ok := byKey[key]
		if !ok {
			a = &App{Name: key[1], Namespace: key[0], Status: AppOK}
			byKey[key] = a
		}
		a.Workloads++
		a.Ready += ready
		a.Total += total
		if severity(status) > severity(a.Status) {
			a.Status = status
		}
		if !deployed.IsZero() && (a.LastDeploy == nil || deployed.After(*a.LastDeploy)) {
			a.LastDeploy = &deployed
		}
	}

	for i := range snap.deployments {
		d := &snap.deployments[i]
		deployed := newestRS[ownerKey{"Deployment", d.Namespace, d.Name}]
		if deployed.IsZero() {
			deployed = d.CreationTimestamp.Time
		}
		add(&d.ObjectMeta, d.Status.ReadyReplicas, replicas(d.Spec.Replicas), deploymentState(d, failing), deployed)
	}
	for i := range snap.statefulSets {
		s := &snap.statefulSets[i]
		add(&s.ObjectMeta, s.Status.ReadyReplicas, replicas(s.Spec.Replicas), statefulSetState(s, failing), s.CreationTimestamp.Time)
	}
	for i := range snap.daemonSets {
		d := &snap.daemonSets[i]
		add(&d.ObjectMeta, d.Status.NumberReady, d.Status.DesiredNumberScheduled, daemonSetState(d, failing), d.CreationTimestamp.Time)
	}

	out := make([]App, 0, len(byKey))
	for _, a := range byKey {
		out = append(out, *a)
	}
	slices.SortFunc(out, func(a, b App) int {
		if c := cmp.Compare(severity(b.Status), severity(a.Status)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	if len(out) > maxApps {
		out = out[:maxApps]
	}
	return out
}

func severity(s AppStatus) int {
	switch s {
	case AppCritical:
		return 2
	case AppHigh:
		return 1
	default:
		return 0
	}
}

func recentDeploys(replicaSets []appsv1.ReplicaSet) []Deploy {
	var out []Deploy
	for i := range replicaSets {
		rs := &replicaSets[i]
		ref := metav1.GetControllerOf(rs)
		rev := rs.Annotations["deployment.kubernetes.io/revision"]
		if ref == nil || ref.Kind != "Deployment" || rev == "" {
			continue
		}
		var image string
		if cs := rs.Spec.Template.Spec.Containers; len(cs) > 0 {
			image = cs[0].Image
		}
		out = append(out, Deploy{Namespace: rs.Namespace, Name: ref.Name, Revision: rev, Image: image, At: rs.CreationTimestamp.Time})
	}
	slices.SortFunc(out, func(a, b Deploy) int { return b.At.Compare(a.At) })
	if len(out) > topN {
		out = out[:topN]
	}
	if out == nil {
		out = []Deploy{}
	}
	return out
}
