// Command glowpod serves the Glowpod Kubernetes dashboard.
//
// In local mode it reads your kubeconfig, listens on a loopback address and
// opens the browser with a one-run sign-in link.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"glowpod/internal/api"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/web"
)

const (
	shutdownTimeout = 5 * time.Second
	// vitePort is where `make dev` serves the UI; its proxy keeps that Host header.
	vitePort = "5173"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "glowpod:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	cfg, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))

	restCfg, info, err := cluster.LoadKubeconfig(cfg.kubeconfig, cfg.context)
	if err != nil {
		return err
	}
	provider, err := cluster.NewLocal(restCfg, info)
	if err != nil {
		return err
	}

	authn := auth.NewLocalDisabled()
	if !cfg.dev {
		if authn, err = auth.NewLocal(); err != nil {
			return err
		}
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.addr, err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return fmt.Errorf("read listen address: %w", err)
	}
	hosts := auth.LoopbackHosts(port)
	if cfg.dev {
		hosts = append(hosts, auth.LoopbackHosts(vitePort)...)
	}

	srv := &http.Server{
		Handler: api.NewRouter(api.Options{
			Frontend:      web.Handler(web.Dist()),
			Provider:      provider,
			Auth:          authn,
			Hosts:         hosts,
			PrometheusURL: cfg.prometheusURL,
			Logger:        logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	logger.Info("using kubeconfig context", "context", info.Name, "server", info.Server)
	if cfg.dev {
		logger.Info("glowpod API is running; open the Vite dev server", "api", "http://"+ln.Addr().String(), "ui", "http://localhost:"+vitePort)
	} else {
		// The link carries this run's sign-in token, so it goes to the terminal
		// only, never to the structured log.
		launch := authn.LaunchURL("http://" + ln.Addr().String())
		fmt.Fprintf(stderr, "\n  Glowpod is running. Open:\n  %s\n\n", launch)
		if !cfg.noBrowser {
			go func() {
				if err := openBrowser(ctx, launch); err != nil {
					logger.Warn("couldn't open the browser; open the link above yourself", "err", err)
				}
			}()
		}
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Browsers open spare connections that never send a request; Shutdown
		// waits on those. Past the grace period, just close everything.
		if err := srv.Close(); err != nil {
			return fmt.Errorf("close server: %w", err)
		}
	}
	return nil
}
