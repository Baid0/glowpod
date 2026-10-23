package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"glowpod/internal/auth"
	"glowpod/internal/terminal"
)

// A browser shell is a short-lived pod in the Hub's namespace with kubectl
// and friends, whose kubeconfig acts as one person on one cluster. The Hub
// opens bash in it with its own identity; the person never gets rights in
// the Hub's namespace.
const (
	kindShell = "shell"
	// ShellComponent labels shell pods; the chart's NetworkPolicy lets them
	// reach only the Hub.
	ShellComponent = "glowpod-shell"
	shellContainer = "shell"
	// shellLifetime is the longest a shell lives, typing or not.
	shellLifetime = 12 * time.Hour
	// shellIdle closes a shell nobody typed in for this long.
	shellIdle = 30 * time.Minute
	// shellStartTimeout covers downloading the image on a new node.
	shellStartTimeout = 3 * time.Minute
	keyKubeconfig     = "kubeconfig"
)

// shellRun is one running shell's objects.
type shellRun struct {
	pod, secret, tokenID string
}

// localAccess is the Hub's own identity on its own cluster, for shell pods.
type localAccess struct {
	mu   sync.Mutex
	cfg  *rest.Config
	kube kubernetes.Interface
}

func (h *Hub) local() (*rest.Config, kubernetes.Interface, error) {
	h.localAccess.mu.Lock()
	defer h.localAccess.mu.Unlock()
	if h.localAccess.kube != nil {
		return h.localAccess.cfg, h.localAccess.kube, nil
	}
	p, ok := h.cfg.Registry.Get(LocalID)
	b, okBase := p.(baseConfiger)
	if !ok || !okBase {
		return nil, nil, errors.New("the Hub can't reach its own cluster")
	}
	cfg := b.Base()
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("API client for the Hub's cluster: %w", err)
	}
	h.localAccess.cfg, h.localAccess.kube = cfg, kube
	return cfg, kube, nil
}

// openShell is GET /api/v1/hub/shell/{cluster}: a WebSocket terminal in a
// new shell pod for that cluster.
func (h *Hub) openShell(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cluster")
	ctx := r.Context()
	info := auth.SessionInfoFrom(ctx)
	if !h.Visible(info, id) {
		writeError(w, http.StatusNotFound, "unknown_cluster", "The Hub has no such cluster that you can see.")
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if namespace != "" && len(validation.IsDNS1123Label(namespace)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
		return
	}
	cfg, kube, err := h.local()
	if err != nil || h.cfg.ShellImage == "" {
		writeError(w, http.StatusServiceUnavailable, "no_shell", "This Hub can't start shells.")
		return
	}
	h.cfg.Logger.Info("opened a cluster shell", "user", info.User, "cluster", id)
	start := func(ctx context.Context, _ terminal.Request, s terminal.Streams) error {
		say := func(format string, a ...any) { _, _ = fmt.Fprintf(s.Stdout, format+"\r\n", a...) }
		say("Starting a shell for %s…", id)
		run, err := h.startShell(ctx, kube, info, id, namespace, say)
		if run != nil {
			defer h.stopShell(context.WithoutCancel(ctx), kube, *run)
		}
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		in := &activeReader{r: s.Stdin}
		in.touch()
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if in.idle() > shellIdle {
						say("\r\nClosed after %d minutes without typing. Open the shell again to carry on.", int(shellIdle.Minutes()))
						cancel()
						return
					}
				}
			}
		}()
		err = terminal.Remote(cfg, kube)(ctx, terminal.Request{Namespace: h.cfg.Namespace, Pod: run.pod, Container: shellContainer},
			terminal.Streams{Stdin: in, Stdout: s.Stdout, Sizes: s.Sizes})
		if ctx.Err() != nil {
			return nil // closed for being idle, or the browser left
		}
		if err != nil {
			h.cfg.Logger.Warn("a cluster shell ended with an error", "cluster", id, "pod", run.pod, "err", err)
		}
		return err
	}
	terminal.Serve(w, r, start, terminal.Request{}, auth.CSRFTokenFrom(ctx))
}

// startShell makes the token, its kubeconfig and the pod, and waits until
// the pod runs. The returned run, when not nil, must be stopped.
func (h *Hub) startShell(ctx context.Context, kube kubernetes.Interface, info auth.SessionInfo, id, namespace string, say func(string, ...any)) (*shellRun, error) {
	failed := func(msg string, err error) (*shellRun, error) {
		h.cfg.Logger.Warn("couldn't start a cluster shell", "cluster", id, "err", err)
		return nil, &terminal.Failure{Code: "shell_failed", Message: msg}
	}
	hubURL, ca, err := h.shellHubURL(ctx)
	if err != nil {
		return failed("The Hub's server URL isn't set yet.", err)
	}
	secret, tok, err := h.newKubeToken(ctx, info, tokenScope{clusters: []string{id}, shell: true, ttl: shellLifetime})
	if err != nil {
		return failed("Couldn't make a token for the shell.", err)
	}
	run := &shellRun{tokenID: tok.ID}
	raw, err := kubeconfigFor(hubURL, ca, []string{id}, id, namespace, secret)
	if err != nil {
		return run, &terminal.Failure{Code: "shell_failed", Message: "Couldn't make the shell's kubeconfig."}
	}
	labels := map[string]string{
		"app.kubernetes.io/managed-by": managedBy,
		"app.kubernetes.io/component":  ShellComponent,
		"hub.glowpod.dev/kind":         kindShell,
	}
	annotations := map[string]string{"hub.glowpod.dev/user": info.User, "hub.glowpod.dev/cluster": id}
	sec, err := kube.CoreV1().Secrets(h.cfg.Namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "glowpod-shell-", Labels: labels, Annotations: annotations},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{keyKubeconfig: raw},
	}, metav1.CreateOptions{})
	if err != nil {
		_, ferr := failed("Couldn't save the shell's kubeconfig.", err)
		return run, ferr
	}
	run.secret = sec.Name
	pod, err := kube.CoreV1().Pods(h.cfg.Namespace).Create(ctx, h.shellPod(sec.Name, id, labels, annotations), metav1.CreateOptions{})
	if err != nil {
		_, ferr := failed("Couldn't start the shell's pod.", err)
		return run, ferr
	}
	run.pod = pod.Name
	// The Secret goes when the pod does, even if the Hub doesn't get to it.
	sec.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}}
	if _, err := kube.CoreV1().Secrets(h.cfg.Namespace).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
		h.cfg.Logger.Debug("couldn't tie the shell's Secret to its pod", "err", err)
	}
	if err := h.waitForShell(ctx, kube, pod.Name, say); err != nil {
		return run, err
	}
	return run, nil
}

// shellPod runs the shell image locked down: not root, a read-only root
// filesystem, no service-account token and no rights of its own.
func (h *Hub) shellPod(secret, id string, labels, annotations map[string]string) *corev1.Pod {
	no, yes := false, true
	uid := int64(1000)
	deadline := int64(shellLifetime.Seconds())
	grace := int64(1)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "glowpod-shell-", Labels: labels, Annotations: annotations},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  &no,
			EnableServiceLinks:            &no,
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         &deadline,
			TerminationGracePeriodSeconds: &grace,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:            shellContainer,
				Image:           h.cfg.ShellImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             []corev1.EnvVar{{Name: "GLOWPOD_CLUSTER", Value: id}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &no,
					ReadOnlyRootFilesystem:   &yes,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "kubeconfig", MountPath: "/etc/glowpod", ReadOnly: true},
					{Name: "home", MountPath: "/home/shell"},
					{Name: "tmp", MountPath: "/tmp"},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "kubeconfig", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secret}}},
				{Name: "home", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr(resource.MustParse("256Mi"))}}},
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr(resource.MustParse("64Mi"))}}},
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }

// waitForShell waits for the pod to run, and says why when it can't.
func (h *Hub) waitForShell(ctx context.Context, kube kubernetes.Interface, name string, say func(string, ...any)) error {
	ctx, cancel := context.WithTimeout(ctx, shellStartTimeout)
	defer cancel()
	saidPull := false
	for {
		pod, err := kube.CoreV1().Pods(h.cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			if pod.Status.Phase == corev1.PodRunning {
				return nil
			}
			if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
				return &terminal.Failure{Code: "shell_failed", Message: "The shell's pod stopped before it started."}
			}
			for _, c := range pod.Status.ContainerStatuses {
				if w := c.State.Waiting; w != nil {
					switch w.Reason {
					case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
						return &terminal.Failure{Code: "shell_image", Message: fmt.Sprintf(
							"The Hub's cluster couldn't download the shell image %s (%s).", h.cfg.ShellImage, w.Reason)}
					case "ContainerCreating":
						if !saidPull {
							say("Getting the shell image ready (the first time on a node takes a minute)…")
							saidPull = true
						}
					}
				}
			}
			for _, c := range pod.Status.Conditions {
				if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
					return &terminal.Failure{Code: "shell_failed", Message: "The Hub's cluster has no room for the shell's pod: " + c.Message}
				}
			}
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return &terminal.Failure{Code: "shell_timeout", Message: "The shell's pod didn't start within three minutes."}
			}
			return ctx.Err() //nolint:wrapcheck // the browser left
		case <-time.After(time.Second):
		}
	}
}

// stopShell deletes the pod (its Secret goes with it) and ends the token.
func (h *Hub) stopShell(ctx context.Context, kube kubernetes.Interface, run shellRun) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	now := int64(0)
	if run.pod != "" {
		if err := kube.CoreV1().Pods(h.cfg.Namespace).Delete(ctx, run.pod, metav1.DeleteOptions{GracePeriodSeconds: &now}); err != nil {
			h.cfg.Logger.Warn("couldn't delete a shell pod", "pod", run.pod, "err", err)
		}
	}
	if run.secret != "" {
		_ = h.store.deleteSecret(ctx, run.secret)
	}
	if run.tokenID != "" {
		if err := h.deleteKubeToken(ctx, run.tokenID); err != nil {
			h.cfg.Logger.Warn("couldn't delete a shell's token", "err", err)
		}
	}
}

// cleanShells removes shells left from before a restart: a shell lives only
// as long as its browser connection, and those didn't survive.
func (h *Hub) cleanShells(ctx context.Context) {
	_, kube, err := h.local()
	if err != nil {
		return
	}
	sel := metav1.ListOptions{LabelSelector: "hub.glowpod.dev/kind=" + kindShell}
	if pods, err := kube.CoreV1().Pods(h.cfg.Namespace).List(ctx, sel); err == nil {
		for i := range pods.Items {
			h.stopShell(ctx, kube, shellRun{pod: pods.Items[i].Name})
		}
	}
	if secrets, err := kube.CoreV1().Secrets(h.cfg.Namespace).List(ctx, sel); err == nil {
		for i := range secrets.Items {
			_ = h.store.deleteSecret(ctx, secrets.Items[i].Name)
		}
	}
	tokens, err := h.store.kubeTokens(ctx)
	if err != nil {
		return
	}
	for _, t := range tokens {
		if t.Shell {
			_ = h.deleteKubeToken(ctx, t.ID)
		}
	}
}

// shellHubURL is how a shell pod reaches the Hub's /k8s proxy, and the CA
// to trust for it: ShellHubURL (inside the cluster), else the server URL.
func (h *Hub) shellHubURL(ctx context.Context) (string, []byte, error) {
	if h.cfg.ShellHubURL != "" {
		return h.cfg.ShellHubURL, []byte(h.cfg.ShellCA), nil
	}
	u, err := h.serverURL(ctx)
	return u, []byte(h.cfg.HubCA), err
}

// activeReader notes when the person last typed.
type activeReader struct {
	r    io.Reader
	last atomic.Int64
}

func (a *activeReader) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if n > 0 {
		a.touch()
	}
	return n, err //nolint:wrapcheck // a pipe's own errors
}

func (a *activeReader) touch() { a.last.Store(time.Now().UnixNano()) }

func (a *activeReader) idle() time.Duration { return time.Since(time.Unix(0, a.last.Load())) }
