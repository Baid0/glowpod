package overview

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"glowpod/internal/apps"
)

func workloads(snap *snapshot, failing map[apps.Owner]bool, now time.Time) Workloads {
	var w Workloads
	for i := range snap.deployments {
		w.Deployments.add(apps.DeploymentStatus(&snap.deployments[i], failing))
	}
	for i := range snap.statefulSets {
		w.StatefulSets.add(apps.StatefulSetStatus(&snap.statefulSets[i], failing))
	}
	for i := range snap.daemonSets {
		w.DaemonSets.add(apps.DaemonSetStatus(&snap.daemonSets[i], failing))
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
