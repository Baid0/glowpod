// Package quickstart runs a Glowpod Hub in one container, like Rancher's
// Docker install: K3s inside, and the Hub on it. It's for trying the Hub;
// production Hubs use the Helm chart.
package quickstart

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
)

// Namespace is where the Hub keeps its state inside the container.
const Namespace = "glowpod"

//go:embed manifests.yaml
var manifests []byte

// Paths are where the container keeps everything, under one volume.
type Paths struct{ Data string }

// K3s is K3s's data directory.
func (p Paths) K3s() string { return filepath.Join(p.Data, "k3s") }

// Kubeconfig is K3s's admin kubeconfig, which the Hub uses as its own
// identity: it's alone in the container.
func (p Paths) Kubeconfig() string { return filepath.Join(p.Data, "kubeconfig.yaml") }

// K3sLog is where K3s's own log goes; it's long and rarely needed.
func (p Paths) K3sLog() string { return filepath.Join(p.Data, "k3s.log") }

// TLS holds the private CA and its key.
func (p Paths) TLS() string { return filepath.Join(p.Data, "tls") }

// PrepareCgroups lets K3s create cgroups inside the container. With cgroup
// v2 a container's processes sit in its root cgroup, where controllers
// can't be handed down; moving them to a child cgroup fixes that, as k3d
// and Rancher's image do.
func PrepareCgroups() error {
	const root = "/sys/fs/cgroup"
	controllers, err := os.ReadFile(root + "/cgroup.controllers")
	if errors.Is(err, fs.ErrNotExist) {
		return nil // cgroup v1: nothing to do
	}
	if err != nil {
		return fmt.Errorf("read cgroup controllers: %w", err)
	}
	if err := os.MkdirAll(root+"/init", 0o755); err != nil { //nolint:gosec // cgroup directories are 0755
		return fmt.Errorf("make the init cgroup (is the container --privileged?): %w", err)
	}
	procs, err := os.ReadFile(root + "/cgroup.procs")
	if err != nil {
		return fmt.Errorf("read cgroup processes: %w", err)
	}
	for _, pid := range strings.Fields(string(procs)) {
		// Kernel threads can't move; that's fine.
		_ = os.WriteFile(root+"/init/cgroup.procs", []byte(pid), 0) //nolint:gosec // a fixed cgroup path; the PID is the content

	}
	var enable []string
	for _, c := range strings.Fields(string(controllers)) {
		enable = append(enable, "+"+c)
	}
	if err := os.WriteFile(root+"/cgroup.subtree_control", []byte(strings.Join(enable, " ")), 0); err != nil { //nolint:gosec // a fixed cgroup path
		return fmt.Errorf("enable cgroup controllers: %w", err)
	}
	return nil
}

// WriteManifests puts the Hub's CRD, namespace and admin binding in K3s's
// auto-deploy folder; K3s applies them at every start.
func WriteManifests(p Paths) error {
	dir := filepath.Join(p.K3s(), "server", "manifests")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "glowpod-hub.yaml"), manifests, 0o600); err != nil {
		return fmt.Errorf("write the Hub's manifests: %w", err)
	}
	return nil
}

// ShellPort is where shell pods reach the Hub's /k8s proxy inside the
// container, over HTTPS from a CA made at start; it isn't published.
const ShellPort = 8081

// HostIP is the container's own address, which K3s uses as its node's:
// pods reach the Hub there.
func HostIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("read the container's addresses: %w", err)
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return n.IP.String(), nil
		}
	}
	return "", errors.New("the container has no IPv4 address")
}

// WriteShellPolicy lets shell pods reach only the Hub, at ip: a
// NetworkPolicy in K3s's auto-deploy folder, rewritten at every start since
// the container's address can change.
func WriteShellPolicy(p Paths, ip string) error {
	doc := fmt.Sprintf(`# Shell pods may reach the Hub's /k8s proxy and nothing else.
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: glowpod-shell
  namespace: %s
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: glowpod-shell
  policyTypes: [Ingress, Egress]
  egress:
    - to:
        - ipBlock:
            cidr: %s/32
      ports:
        - protocol: TCP
          port: %d
`, Namespace, ip, ShellPort)
	dir := filepath.Join(p.K3s(), "server", "manifests")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "glowpod-shell.yaml"), []byte(doc), 0o600); err != nil {
		return fmt.Errorf("write the shell policy: %w", err)
	}
	return nil
}

// K3sArgs are the arguments for k3s server.
func K3sArgs(p Paths) []string {
	return []string{
		"server",
		"--data-dir", p.K3s(),
		"--write-kubeconfig", p.Kubeconfig(),
		"--write-kubeconfig-mode", "0600",
		// The Hub serves 80 and 443 itself; the cluster inside needs no
		// ingress or load balancer.
		"--disable", "traefik",
		"--disable", "servicelb",
		// A fixed name, so a new container on the same volume is the same node.
		"--node-name", "glowpod-hub",
	}
}

// StartK3s starts k3s server. It stops K3s when ctx ends; the returned
// channel gets K3s's exit.
func StartK3s(ctx context.Context, binary string, p Paths) (<-chan error, error) {
	if err := os.MkdirAll(p.Data, 0o700); err != nil {
		return nil, fmt.Errorf("make %s: %w", p.Data, err)
	}
	logFile, err := os.OpenFile(p.K3sLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the K3s log: %w", err)
	}
	cmd := exec.CommandContext(ctx, binary, K3sArgs(p)...) //nolint:gosec // the binary is a flag of glowpod hub run
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 30 * time.Second
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start K3s: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		done <- err
	}()
	return done, nil
}

// WaitReady waits until the API server inside answers and serves the
// Hub's CRD, logging now and then: a first start takes about a minute.
func WaitReady(ctx context.Context, kube kubernetes.Interface, k3sDone <-chan error, logger *slog.Logger) error {
	start := time.Now()
	lastLog := start
	for {
		if _, err := kube.Discovery().ServerResourcesForGroupVersion("hub.glowpod.dev/v1alpha1"); err == nil {
			logger.Info("Kubernetes inside the container is ready", "took", time.Since(start).Round(time.Second).String())
			return nil
		}
		if time.Since(lastLog) > 15*time.Second {
			logger.Info("waiting for Kubernetes inside the container (K3s)", "waited", time.Since(start).Round(time.Second).String())
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for K3s: %w", ctx.Err())
		case err := <-k3sDone:
			return fmt.Errorf("k3s stopped: %w", errOrExit(err))
		case <-time.After(2 * time.Second):
		}
	}
}

func errOrExit(err error) error {
	if err == nil {
		return errors.New("it exited")
	}
	return err
}
