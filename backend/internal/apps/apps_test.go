package apps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func meta(ns, name string, labels, annotations map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, Annotations: annotations, CreationTimestamp: metav1.NewTime(now.Add(-48 * time.Hour))}
}

func TestKey(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		ann    map[string]string
		want   string
	}{
		{"helm release wins", map[string]string{partOf: "argocd", appName: "argocd-server"}, map[string]string{helmRelease: "prometheus"}, "prometheus"},
		{"part-of groups components", map[string]string{partOf: "argocd", appName: "argocd-server"}, nil, "argocd"},
		{"name", map[string]string{appName: "payments", instance: "payments-prod"}, nil, "payments"},
		{"instance", map[string]string{instance: "web-1"}, nil, "web-1"},
		{"old app label", map[string]string{"app": "legacy"}, nil, "legacy"},
		{"k8s-app", map[string]string{"k8s-app": "kube-dns"}, nil, "kube-dns"},
		{"workload name", nil, nil, "coredns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := meta("kube-system", "coredns", tt.labels, tt.ann)
			if got := Key(&m); got != tt.want {
				t.Fatalf("Key = %q, want %q", got, tt.want)
			}
		})
	}
}

func deployment(ns, name string, want, ready int32, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: meta(ns, name, labels, nil),
		Spec: appsv1.DeploymentSpec{Replicas: ptr(want), Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "acme/" + name + ":v1"}},
		}}},
		Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: ready, AvailableReplicas: ready},
	}
}

func controller(kind, name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{Kind: kind, Name: name, Controller: ptr(true), APIVersion: "apps/v1"}}
}

func replicaSet(ns, name, deploy, rev string, ago time.Duration) *appsv1.ReplicaSet {
	m := meta(ns, name, nil, map[string]string{"deployment.kubernetes.io/revision": rev})
	m.OwnerReferences = controller("Deployment", deploy)
	m.CreationTimestamp = metav1.NewTime(now.Add(-ago))
	return &appsv1.ReplicaSet{ObjectMeta: m, Spec: appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Image: "acme/" + deploy + ":r" + rev}},
	}}}}
}

func pod(name string, owner []metav1.OwnerReference, crash bool) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: meta("argocd", name, nil, nil),
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", Ready: !crash,
		}}},
	}
	p.OwnerReferences = owner
	if crash {
		p.Status.ContainerStatuses[0].RestartCount = 5
		p.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}
	}
	return p
}

// argocd: three components grouped by part-of, one of them crashing.
func cluster() []runtime.Object {
	argo := func(name string) map[string]string {
		return map[string]string{partOf: "argocd", appName: name}
	}
	return []runtime.Object{
		deployment("argocd", "argocd-server", 1, 1, argo("argocd-server")),
		deployment("argocd", "argocd-repo-server", 2, 1, argo("argocd-repo-server")),
		&appsv1.StatefulSet{
			ObjectMeta: meta("argocd", "argocd-application-controller", argo("argocd-application-controller"), nil),
			Spec:       appsv1.StatefulSetSpec{Replicas: ptr(int32(1))},
			Status:     appsv1.StatefulSetStatus{ReadyReplicas: 1},
		},
		deployment("argocd", "unrelated", 1, 1, nil),
		replicaSet("argocd", "argocd-repo-server-abc", "argocd-repo-server", "2", time.Hour),
		replicaSet("argocd", "argocd-repo-server-old", "argocd-repo-server", "1", 30*time.Hour),
		replicaSet("argocd", "unrelated-1", "unrelated", "1", time.Minute),
		pod("argocd-repo-server-abc-1", controller("ReplicaSet", "argocd-repo-server-abc"), false),
		pod("argocd-repo-server-abc-2", controller("ReplicaSet", "argocd-repo-server-abc"), true),
		pod("argocd-application-controller-0", controller("StatefulSet", "argocd-application-controller"), false),
		pod("unrelated-1-x", controller("ReplicaSet", "unrelated-1"), false),
		pod("loose", nil, false),
	}
}

func TestList(t *testing.T) {
	got, err := List(context.Background(), fake.NewClientset(cluster()...), "argocd", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("apps = %+v", got)
	}
	a := got[0]
	if a.Name != "argocd" || a.Workloads != 3 || a.Ready != 3 || a.Total != 4 || a.Status != Critical {
		t.Fatalf("argocd = %+v (critical first: a crashing pod)", a)
	}
	if a.LastDeploy == nil || !a.LastDeploy.Equal(now.Add(-time.Hour)) {
		t.Fatalf("last deploy = %v", a.LastDeploy)
	}
	if got[1].Name != "unrelated" || got[1].Status != OK {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestGet(t *testing.T) {
	kube := fake.NewClientset(cluster()...)
	d, err := Get(context.Background(), kube, "argocd", "argocd", now)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, w := range d.Workloads {
		kinds = append(kinds, w.Kind+"/"+w.Name+"/"+string(w.Status))
	}
	// Troubled first, then by kind and name.
	if strings.Join(kinds, ",") != "Deployment/argocd-repo-server/critical,Deployment/argocd-server/ok,StatefulSet/argocd-application-controller/ok" {
		t.Fatalf("workloads = %v", kinds)
	}
	if d.Workloads[0].Images[0] != "acme/argocd-repo-server:v1" || d.Workloads[0].Resource != "deployments" {
		t.Fatalf("workload = %+v", d.Workloads[0])
	}
	var pods []string
	for _, p := range d.Pods {
		pods = append(pods, p.Name)
	}
	if strings.Join(pods, ",") != "argocd-application-controller-0,argocd-repo-server-abc-1,argocd-repo-server-abc-2" {
		t.Fatalf("pods = %v (only this app's pods)", pods)
	}
	if len(d.Problems) != 1 || d.Problems[0].Reason != "CrashLoopBackOff" {
		t.Fatalf("problems = %+v", d.Problems)
	}
	if len(d.Deploys) != 2 || d.Deploys[0].Revision != "2" || d.Deploys[0].Image != "acme/argocd-repo-server:r2" {
		t.Fatalf("deploys = %+v (newest first, this app only)", d.Deploys)
	}

	if _, err := Get(context.Background(), kube, "argocd", "nope", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
