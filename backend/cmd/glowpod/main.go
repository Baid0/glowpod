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
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/rest"

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

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.addr, err)
	}

	opts := api.Options{
		Frontend:      web.Handler(web.Dist()),
		PrometheusURL: cfg.prometheusURL,
		Logger:        logger,
	}
	var afterStart func()
	if cfg.mode == "server" {
		err = setupServer(ctx, cfg, &opts, logger)
		afterStart = func() {
			logger.Info("glowpod is serving", "addr", ln.Addr().String(), "public_url", cfg.server.publicURL)
		}
	} else {
		afterStart, err = setupLocal(ctx, cfg, &opts, ln, logger, stderr)
	}
	if err != nil {
		_ = ln.Close()
		return err
	}

	srv := &http.Server{
		Handler:           api.NewRouter(opts),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	afterStart()

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

// setupLocal acts as the kubeconfig's user, for one person on this machine.
func setupLocal(ctx context.Context, cfg config, opts *api.Options, ln net.Listener, logger *slog.Logger, stderr io.Writer) (func(), error) {
	restCfg, info, err := cluster.LoadKubeconfig(cfg.kubeconfig, cfg.context)
	if err != nil {
		return nil, err
	}
	provider, err := cluster.NewLocal(restCfg, info)
	if err != nil {
		return nil, err
	}
	authn := auth.NewLocalDisabled()
	if !cfg.dev {
		if authn, err = auth.NewLocal(); err != nil {
			return nil, err
		}
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("read listen address: %w", err)
	}
	hosts := auth.LoopbackHosts(port)
	if cfg.dev {
		hosts = append(hosts, auth.LoopbackHosts(vitePort)...)
	}
	opts.Provider, opts.Auth, opts.Hosts = provider, authn, hosts

	return func() {
		logger.Info("using kubeconfig context", "context", info.Name, "server", info.Server)
		if cfg.dev {
			logger.Info("glowpod API is running; open the Vite dev server", "api", "http://"+ln.Addr().String(), "ui", "http://localhost:"+vitePort)
			return
		}
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
	}, nil
}

// setupServer serves many people in the cluster. Glowpod's own service
// account only impersonates; each person acts as themselves.
func setupServer(ctx context.Context, cfg config, opts *api.Options, logger *slog.Logger) error {
	base, info, err := serverBase(cfg)
	if err != nil {
		return err
	}
	provider := cluster.NewServer(base, info)
	sc := cfg.server
	authCfg := auth.ServerConfig{
		Store:         auth.NewStore(sc.sessionIdle, sc.sessionTTL),
		SecureCookies: !sc.insecureCookies,
		WhoAmI:        provider.WhoAmI,
		Forget:        provider.Forget,
		Token:         sc.token,
		OIDCName:      sc.oidcName,
		Logger:        logger,
	}
	if sc.oidcIssuer != "" {
		if authCfg.OIDC, err = newOIDC(ctx, sc); err != nil {
			return err
		}
	}
	if sc.ldapURL != "" {
		if authCfg.LDAP, err = newLDAP(sc); err != nil {
			return err
		}
	}
	authn, err := auth.NewServer(authCfg)
	if err != nil {
		return err
	}
	opts.Provider, opts.Auth = provider, authn
	if sc.publicURL != "" {
		u, _ := url.Parse(sc.publicURL) // checked in parseFlags
		opts.Hosts = []string{u.Host}
		opts.HSTS = u.Scheme == "https"
	}
	return nil
}

// serverBase is Glowpod's own identity: the pod's service account, or a
// kubeconfig when running server mode outside the cluster for a test.
func serverBase(cfg config) (*rest.Config, cluster.Info, error) {
	if cfg.kubeconfig != "" || cfg.context != "" {
		restCfg, info, err := cluster.LoadKubeconfig(cfg.kubeconfig, cfg.context)
		if err != nil {
			return nil, cluster.Info{}, err
		}
		if cfg.server.clusterName != "" {
			info.Name = cfg.server.clusterName
		}
		return restCfg, info, nil
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, cluster.Info{}, fmt.Errorf("server mode runs inside the cluster (pass -kubeconfig to run it elsewhere): %w", err)
	}
	return restCfg, cluster.InClusterInfo(restCfg, cfg.server.clusterName), nil
}
