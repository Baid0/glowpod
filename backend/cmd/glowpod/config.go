package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
)

const defaultAddr = "127.0.0.1:7171"

type config struct {
	addr          string
	noBrowser     bool
	dev           bool
	kubeconfig    string
	context       string
	prometheusURL string
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("glowpod", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	fs.StringVar(&cfg.addr, "addr", defaultAddr, "loopback address to listen on")
	fs.BoolVar(&cfg.noBrowser, "no-browser", false, "don't open the browser on start")
	fs.BoolVar(&cfg.dev, "dev", false, "development mode: serve the API only, the Vite dev server serves the UI")
	fs.StringVar(&cfg.kubeconfig, "kubeconfig", "", "kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	fs.StringVar(&cfg.context, "context", "", "kubeconfig context (default: the current context)")
	fs.StringVar(&cfg.prometheusURL, "prometheus-url", "", "Prometheus base URL; skips discovery in the cluster")

	if err := fs.Parse(args); err != nil {
		return config{}, err //nolint:wrapcheck // flag errors are already user-facing
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if err := checkLoopback(cfg.addr); err != nil {
		return config{}, err
	}
	if cfg.prometheusURL != "" {
		u, err := url.Parse(cfg.prometheusURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return config{}, fmt.Errorf("-prometheus-url %q must be an http(s) URL", cfg.prometheusURL)
		}
	}
	return cfg, nil
}

// checkLoopback makes sure local mode, which acts with the user's kubeconfig,
// is never reachable from other machines.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid -addr %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-addr %q must be a loopback address such as 127.0.0.1 in local mode", addr)
	}
	return nil
}
