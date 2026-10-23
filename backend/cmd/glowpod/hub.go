package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"glowpod/internal/agent"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/hub"
)

// imageRepository is where releases publish the image agents run.
const imageRepository = "ghcr.io/glowpod-dev/glowpod"

// newHub starts a Hub on base, Glowpod's own service account in the local
// cluster, and adds every imported cluster to clusters.
func newHub(ctx context.Context, sc serverConfig, base *rest.Config, clusters *cluster.Registry, logger *slog.Logger) (*hub.Hub, error) {
	kube, err := kubernetes.NewForConfig(base)
	if err != nil {
		return nil, fmt.Errorf("hub client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(base)
	if err != nil {
		return nil, fmt.Errorf("hub client: %w", err)
	}
	var ca string
	if sc.hubCAFile != "" {
		pem, err := os.ReadFile(sc.hubCAFile)
		if err != nil {
			return nil, fmt.Errorf("read -hub-ca-file: %w", err)
		}
		ca = string(pem)
	}
	image := sc.agentImage
	if image == "" {
		image = imageRepository + ":" + strings.TrimPrefix(version, "v")
	}
	var admins []string
	for _, a := range strings.Split(sc.hubAdmins, ",") {
		if a = strings.TrimSpace(a); a != "" {
			admins = append(admins, a)
		}
	}
	h := hub.New(hub.Config{
		Namespace:     sc.hubNamespace,
		Kube:          kube,
		Dynamic:       dyn,
		Registry:      clusters,
		PublicURL:     sc.publicURL,
		AgentImage:    image,
		HubCA:         ca,
		Admins:        admins,
		BootstrapHint: sc.bootstrapHint,
		KubeconfigTTL: sc.kubeconfigTTL,
		Logger:        logger.With("component", "hub"),
	})
	if err := h.Start(ctx); err != nil {
		return nil, fmt.Errorf("start the Hub: %w", err)
	}
	logger.Info("glowpod hub", "namespace", sc.hubNamespace, "agent_image", image)
	return h, nil
}

// runAgent is glowpod agent: in an imported cluster, connect out to the Hub.
func runAgent(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("glowpod agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	namespace := fs.String("namespace", os.Getenv("POD_NAMESPACE"), "namespace of the agent's Secret (default $POD_NAMESPACE)")
	secret := fs.String("secret", "glowpod-agent", "the agent's Secret: the Hub's URL and CA, and its token or credential")
	kubeconfig := fs.String("kubeconfig", "", "kubeconfig, to run the agent outside the cluster for a test (default: in-cluster)")
	logFormat := fs.String("log-format", "text", "log format: text or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err //nolint:wrapcheck // flag errors are already user-facing
	}
	if *namespace == "" {
		*namespace = hub.AgentNamespace
	}
	var logger *slog.Logger
	if *logFormat == "json" {
		logger = slog.New(slog.NewJSONHandler(stderr, nil))
	} else {
		logger = slog.New(slog.NewTextHandler(stderr, nil))
	}

	var upstream *rest.Config
	var err error
	if *kubeconfig != "" {
		upstream, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		upstream, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("connect to the API server: %w", err)
	}
	upstream.UserAgent = "glowpod-agent/" + version
	kube, err := kubernetes.NewForConfig(upstream)
	if err != nil {
		return fmt.Errorf("API client: %w", err)
	}
	logger.Info("starting glowpod agent", "version", version, "secret", *namespace+"/"+*secret)
	return agent.Run(ctx, agent.Config{Namespace: *namespace, Secret: *secret, Upstream: upstream, Kube: kube, Logger: logger})
}

// pruneSessions deletes the Hub's expired sign-ins now and then: a session
// nobody comes back to is never read again otherwise.
func pruneSessions(ctx context.Context, store *auth.Store, logger *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if err := store.Prune(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("couldn't delete expired sessions", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
