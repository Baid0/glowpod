package topology

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuild(t *testing.T) {
	one := int32(1)
	meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Namespace: "shop", Name: name} }
	web := &appsv1.Deployment{
		ObjectMeta: meta("web"),
		Spec: appsv1.DeploymentSpec{Replicas: &one, Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web", "tier": "front"}},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "cfg", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}},
					{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}},
				},
				Containers: []corev1.Container{{
					Name:    "app",
					EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "db"}}}},
					Env: []corev1.EnvVar{{Name: "X", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "gone"}, Key: "x",
					}}}},
				}},
			},
		}},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
	kube := fake.NewClientset(
		web,
		&corev1.Service{ObjectMeta: meta("web"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{"app": "web"}}},
		&corev1.Service{ObjectMeta: meta("orphan"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{"app": "none"}}},
		&networkingv1.Ingress{ObjectMeta: meta("front"), Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			Host: "shop.example.com",
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
				{Path: "/", Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web"}}},
				{Path: "/old", Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "ghost"}}},
			}}},
		}}}},
		&corev1.ConfigMap{ObjectMeta: meta("settings")},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta("data"), Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}},
	)
	m, err := Build(context.Background(), kube, "shop", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]Node{}
	for _, n := range m.Nodes {
		nodes[n.ID] = n
	}
	edges := map[Edge]bool{}
	for _, e := range m.Edges {
		edges[e] = true
	}

	check := func(id string, col Column, status string) {
		t.Helper()
		n, ok := nodes[id]
		if !ok || n.Column != col || n.Status != status {
			t.Errorf("%s = %+v (present %v), want column %d status %s", id, n, ok, col, status)
		}
	}
	check("Ingress/front", Entry, "neutral")
	check("Service/web", Services, "neutral")
	check("Service/orphan", Services, "high")
	check("Service/ghost", Services, "critical")
	check("Deployment/web", Runs, "ok")
	check("ConfigMap/settings", Uses, "neutral")
	check("ConfigMap/gone", Uses, "critical")
	check("Secret/db", Uses, "neutral")
	check("PersistentVolumeClaim/data", Uses, "ok")
	if !nodes["Service/ghost"].Missing || !nodes["ConfigMap/gone"].Missing || nodes["Secret/db"].Missing {
		t.Error("missing references are flagged, secrets aren't checked (that would read them)")
	}
	if nodes["Deployment/web"].Detail != "1/1 ready" || nodes["Ingress/front"].Detail != "shop.example.com" {
		t.Errorf("details: %+v %+v", nodes["Deployment/web"], nodes["Ingress/front"])
	}
	for _, e := range []Edge{
		{"Ingress/front", "Service/web"}, {"Ingress/front", "Service/ghost"}, {"Service/web", "Deployment/web"},
		{"Deployment/web", "ConfigMap/settings"}, {"Deployment/web", "Secret/db"}, {"Deployment/web", "PersistentVolumeClaim/data"},
	} {
		if !edges[e] {
			t.Errorf("missing edge %v", e)
		}
	}
	if edges[Edge{"Service/orphan", "Deployment/web"}] {
		t.Error("a service only links to workloads its selector matches")
	}
	// Columns are in order for drawing.
	for i := 1; i < len(m.Nodes); i++ {
		if m.Nodes[i].Column < m.Nodes[i-1].Column {
			t.Fatalf("nodes out of column order at %d", i)
		}
	}
}
