package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/tui"
)

// termConfig is `glowpod term`: Glowpod in the terminal.
type termConfig struct {
	kubeconfig    string
	context       string
	namespace     string
	prometheusURL string
	resource      string
}

func parseTermFlags(args []string, stderr io.Writer) (termConfig, error) {
	fs := flag.NewFlagSet("glowpod term", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: glowpod term [flags] [resource]\n\n"+
			"Glowpod in the terminal. Starts on Glance; give a resource (pods, deploy, any CRD) to start on its table.\n\n")
		fs.PrintDefaults()
	}
	var cfg termConfig
	fs.StringVar(&cfg.kubeconfig, "kubeconfig", "", "kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	fs.StringVar(&cfg.context, "context", "", "kubeconfig context (default: the current context)")
	fs.StringVar(&cfg.namespace, "namespace", "", "namespace to start in (default: all)")
	fs.StringVar(&cfg.namespace, "n", "", "shorthand for -namespace")
	fs.StringVar(&cfg.prometheusURL, "prometheus-url", "", "Prometheus base URL; skips discovery in the cluster")
	if err := fs.Parse(args); err != nil {
		return termConfig{}, err //nolint:wrapcheck // flag errors are already user-facing
	}
	switch fs.NArg() {
	case 0:
	case 1:
		cfg.resource = fs.Arg(0)
	default:
		return termConfig{}, fmt.Errorf("unexpected argument %q", fs.Arg(1))
	}
	return cfg, nil
}

// runTerm runs the terminal app as the kubeconfig's user.
func runTerm(ctx context.Context, args []string, stderr io.Writer) error {
	cfg, err := parseTermFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	// Anything written to the terminal would tear the screen: no client-go
	// warnings or klog lines while the app runs.
	rest.SetDefaultWarningHandler(rest.NoWarnings{})
	klog.SetOutput(io.Discard)
	klog.LogToStderr(false)
	klog.SetLogger(logr.Discard())
	slog.SetDefault(slog.New(slog.DiscardHandler))

	restCfg, info, err := cluster.LoadKubeconfig(cfg.kubeconfig, cfg.context)
	if err != nil {
		return err
	}
	provider, err := cluster.NewLocal(restCfg, info)
	if err != nil {
		return err
	}
	clients, err := provider.ClientsFor(ctx, auth.LocalIdentity)
	if err != nil {
		return err
	}
	src := tui.NewLocal(info, clients, cfg.prometheusURL)
	return tui.Run(ctx, src, tui.Options{Namespace: cfg.namespace, Resource: cfg.resource})
}
