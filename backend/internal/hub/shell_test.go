package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"

	"glowpod/internal/terminal"
)

// shellCluster is a fake cluster that names generateName objects, as the
// API server does, and sets every new pod's phase.
func shellCluster(t *testing.T, phase corev1.PodPhase, waiting string) *fake.Clientset {
	t.Helper()
	kube := fake.NewClientset()
	var n atomic.Int32
	kube.PrependReactor("create", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		obj := a.(k8stesting.CreateAction).GetObject()
		m, ok := obj.(metav1.Object)
		if ok && m.GetName() == "" && m.GetGenerateName() != "" {
			m.SetName(fmt.Sprintf("%s%d", m.GetGenerateName(), n.Add(1)))
			m.SetUID(types.UID("uid-" + m.GetName()))
		}
		if pod, ok := obj.(*corev1.Pod); ok {
			pod.Status.Phase = phase
			if waiting != "" {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: shellContainer, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: waiting}}}}
			}
		}
		return false, nil, nil
	})
	return kube
}

func shellHub(t *testing.T) *testHub {
	t.Helper()
	th, _, _ := proxyHub(t)
	th.cfg.ShellImage = "ghcr.io/glowpod-dev/glowpod-shell:9.9.9"
	th.cfg.ShellHubURL = "https://glowpod-shell.glowpod.svc:8443"
	th.cfg.ShellCA = "-----BEGIN CERTIFICATE-----\nshells\n-----END CERTIFICATE-----\n"
	return th
}

func TestShellStartsLockedDownAndCleansUp(t *testing.T) {
	th := shellHub(t)
	ctx := t.Context()
	kube := shellCluster(t, corev1.PodRunning, "")
	var said []string
	say := func(f string, a ...any) { said = append(said, fmt.Sprintf(f, a...)) }

	run, err := th.startShell(ctx, kube, ann, LocalID, "shop", say)
	if err != nil {
		t.Fatal(err)
	}
	pod, err := kube.CoreV1().Pods(ns).Get(ctx, run.pod, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, c := pod.Spec, pod.Spec.Containers[0]
	switch {
	case spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken:
		t.Fatal("the shell gets no service-account token")
	case spec.SecurityContext == nil || !*spec.SecurityContext.RunAsNonRoot:
		t.Fatal("the shell doesn't run as root")
	case c.SecurityContext == nil || !*c.SecurityContext.ReadOnlyRootFilesystem || *c.SecurityContext.AllowPrivilegeEscalation ||
		len(c.SecurityContext.Capabilities.Drop) != 1 || c.SecurityContext.Capabilities.Drop[0] != "ALL":
		t.Fatalf("the container is locked down: %+v", c.SecurityContext)
	case pod.Labels["app.kubernetes.io/component"] != ShellComponent:
		t.Fatal("the NetworkPolicy finds shells by their component label")
	case c.Image != th.cfg.ShellImage || spec.ActiveDeadlineSeconds == nil:
		t.Fatalf("image %q, deadline %v", c.Image, spec.ActiveDeadlineSeconds)
	}

	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, run.secret, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sec.OwnerReferences) != 1 || sec.OwnerReferences[0].Name != run.pod {
		t.Fatal("the kubeconfig's Secret goes with its pod")
	}
	cfg, err := clientcmd.Load(sec.Data[keyKubeconfig])
	if err != nil {
		t.Fatal(err)
	}
	cl, ctxCfg := cfg.Clusters[LocalID], cfg.Contexts[LocalID]
	if cl == nil || cl.Server != "https://glowpod-shell.glowpod.svc:8443/k8s/clusters/local" ||
		string(cl.CertificateAuthorityData) != th.cfg.ShellCA || ctxCfg.Namespace != "shop" {
		t.Fatalf("kubeconfig: %+v %+v", cl, ctxCfg)
	}
	tok, ok := th.checkKubeToken(ctx, cfg.AuthInfos["glowpod"].Token)
	if !ok || tok.User != ann.User || !tok.Shell || !tok.reaches(LocalID) || tok.reaches("prod") {
		t.Fatalf("the shell's token acts as ann on local only: %+v", tok)
	}
	if mine, _ := th.myKubeTokens(ctx, ann.User); len(mine) != 0 {
		t.Fatal("a shell's token isn't one of your kubeconfigs")
	}

	th.stopShell(ctx, kube, *run)
	if pods, _ := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{}); len(pods.Items) != 0 {
		t.Fatal("the pod is deleted")
	}
	if _, ok := th.checkKubeToken(ctx, cfg.AuthInfos["glowpod"].Token); ok {
		t.Fatal("the token ends with the shell")
	}
}

func TestShellExplainsWhyItCantStart(t *testing.T) {
	tests := []struct {
		name    string
		phase   corev1.PodPhase
		waiting string
		want    string
	}{
		{"the image can't be pulled", corev1.PodPending, "ImagePullBackOff", "couldn't download the shell image ghcr.io/glowpod-dev/glowpod-shell:9.9.9"},
		{"the pod stopped", corev1.PodFailed, "", "stopped before it started"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := shellHub(t)
			kube := shellCluster(t, tt.phase, tt.waiting)
			run, err := th.startShell(t.Context(), kube, ann, LocalID, "", func(string, ...any) {})
			var f *terminal.Failure
			if !errors.As(err, &f) || !strings.Contains(f.Message, tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if run == nil || run.pod == "" {
				t.Fatal("what was made is returned, to be cleaned up")
			}
		})
	}
}

func TestShellWaitGivesUp(t *testing.T) {
	th := shellHub(t)
	kube := shellCluster(t, corev1.PodPending, "ContainerCreating")
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	var said []string
	_, err := th.startShell(ctx, kube, ann, LocalID, "", func(f string, a ...any) { said = append(said, fmt.Sprintf(f, a...)) })
	if err == nil || len(said) == 0 || !strings.Contains(said[0], "Getting the shell image ready") {
		t.Fatalf("err %v, said %q", err, said)
	}
}

func TestOpenShellOnlyForVisibleClusters(t *testing.T) {
	th := shellHub(t)
	th.as = ann
	if rec := th.do(t, "GET", "/api/v1/hub/shell/prod", nil); rec.Code != 404 {
		t.Fatalf("a cluster ann can't see: %d", rec.Code)
	}
	if rec := th.do(t, "GET", "/api/v1/hub/shell/local?namespace=Not_Valid", nil); rec.Code != 400 {
		t.Fatalf("a bad namespace: %d", rec.Code)
	}
}

func TestCleanShellsAfterARestart(t *testing.T) {
	th := shellHub(t)
	ctx := t.Context()
	kube := shellCluster(t, corev1.PodRunning, "")
	if _, err := th.startShell(ctx, kube, ann, LocalID, "", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	th.localAccess.kube = kube
	th.cleanShells(ctx)
	if pods, _ := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=shell"}); len(pods.Items) != 0 {
		t.Fatalf("%d shell pods left", len(pods.Items))
	}
	tokens, _ := th.store.kubeTokens(ctx)
	for _, tok := range tokens {
		if tok.Shell {
			t.Fatal("shell tokens are deleted")
		}
	}
}
