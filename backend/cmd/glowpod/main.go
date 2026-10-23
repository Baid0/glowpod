// Command glowpod serves the Glowpod Kubernetes dashboard.
//
// In local mode it reads your kubeconfig, listens on a loopback address and
// opens the browser with a one-run sign-in link.
package main

import (
	"context"
	"crypto/tls"
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
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/rest"

	"glowpod/internal/api"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/hub"
	"glowpod/internal/quickstart"
	"glowpod/internal/web"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

const (
	shutdownTimeout = 5 * time.Second
	// vitePort is where `make dev` serves the UI; its proxy keeps that Host header.
	vitePort = "5173"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "glowpod:", err)
		os.Exit(1)
	}
}

// run is glowpod's main. Answers (the version) go to stdout; logs, help and
// errors go to stderr.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "term" {
		return runTerm(ctx, args[1:], stderr)
	}
	if len(args) > 0 && args[0] == "agent" {
		return runAgent(ctx, args[1:], stderr)
	}
	if len(args) > 0 && args[0] == "hub" {
		return runHub(ctx, args[1:], stdout, stderr)
	}
	cfg, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	if cfg.version {
		fmt.Fprintln(stdout, "glowpod", version)
		return nil
	}
	logger := newLogger(cfg, stderr)
	logger.Info("starting glowpod", "version", version, "mode", cfg.mode)
	return serve(ctx, cfg, logger, stderr)
}

// serve runs Glowpod with a parsed config until ctx ends.
func serve(ctx context.Context, cfg config, logger *slog.Logger, stderr io.Writer) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.addr, err)
	}
	if cfg.tls != nil {
		ln = tls.NewListener(ln, cfg.tls)
	}

	opts := api.Options{
		Version:       version,
		Frontend:      web.Handler(web.Dist()),
		PrometheusURL: cfg.prometheusURL,
		Logger:        logger,
	}
	var afterStart func()
	var shells *quickstart.Certs
	if cfg.mode == "server" && cfg.server.hub && cfg.server.shellAddr != "" {
		ca, err := quickstart.NewMemoryCA("Glowpod Hub shells")
		if err != nil {
			return err
		}
		u, _ := url.Parse(cfg.server.shellHubURL) // checked in parseFlags
		shells = &quickstart.Certs{CA: ca, Names: func() []string { return []string{u.Hostname()} }}
		cfg.server.shellCA = string(ca.PEM)
	}
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
		// net/http's own messages, mostly clients that drop a connection or
		// reject a certificate: debug, in the same log format.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelDebug),
	}
	afterStart()

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	if shells != nil {
		// Shell pods reach kubectl's proxy here, over TLS from a CA made at
		// start: shells end with this process, so it never needs saving.
		inner := &http.Server{
			Addr:              cfg.server.shellAddr,
			Handler:           onlyPrefix(hub.KubeAPIPrefix, srv.Handler),
			ReadHeaderTimeout: 10 * time.Second,
			BaseContext:       srv.BaseContext,
			ErrorLog:          srv.ErrorLog,
			TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: shells.GetCertificate},
		}
		go func() {
			if err := inner.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Warn("the shells' address stopped", "err", err)
			}
		}()
		defer inner.Close() //nolint:errcheck // shutting down
	}

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
	clusters, err := cluster.LoadContexts(cfg.kubeconfig, cfg.context)
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
	opts.Clusters, opts.Auth, opts.Hosts = clusters, authn, hosts
	current := clusters.Current().Info()
	home := "/c/" + current.ID + "/"

	return func() {
		logger.Info("using kubeconfig", "contexts", len(clusters.List()), "opening", current.Name, "server", current.Server)
		if cfg.dev {
			logger.Info("glowpod API is running; open the Vite dev server", "api", "http://"+ln.Addr().String(), "ui", "http://localhost:"+vitePort)
			return
		}
		// The link carries this run's sign-in token, so it goes to the terminal
		// only, never to the structured log.
		launch := authn.LaunchURL("http://"+ln.Addr().String(), home)
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
	sc := cfg.server
	if sc.hub {
		info.ID, info.Name = hub.LocalID, hub.LocalID
	}
	provider := cluster.NewServer(base, info)
	proxies, _ := parsePrefixes(sc.trustedProxies) // checked in parseFlags
	authCfg := auth.ServerConfig{
		Store:          auth.NewStore(sc.sessionIdle, sc.sessionTTL),
		TrustedProxies: proxies,
		SecureCookies:  !sc.insecureCookies,
		WhoAmI:         provider.WhoAmI,
		Forget:         provider.Forget,
		Token:          sc.token,
		OIDCName:       sc.oidcName,
		Logger:         logger,
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
	clusters := cluster.Single(provider)
	if sc.hub {
		h, err := newHub(ctx, sc, base, clusters, authCfg.Store, logger)
		if err != nil {
			return err
		}
		authCfg.Passwords = h
		authCfg.Store.Saver = h.Sessions()
		go pruneSessions(ctx, authCfg.Store, logger)
		opts.Hub = h
	}
	authn, err := auth.NewServer(authCfg)
	if err != nil {
		return err
	}
	if h, ok := opts.Hub.(*hub.Hub); ok {
		h.UseAuth(ctx, authn.SetProviders, hub.FixedAuth{OIDC: authCfg.OIDC, OIDCName: authCfg.OIDCName, LDAP: authCfg.LDAP})
	}
	opts.Clusters, opts.Auth = clusters, authn
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

func newLogger(cfg config, w io.Writer) *slog.Logger {
	level, _ := parseLevel(cfg.logLevel) // checked in parseFlags
	o := &slog.HandlerOptions{Level: level}
	if cfg.logFormat == "json" {
		return slog.New(slog.NewJSONHandler(w, o))
	}
	return slog.New(slog.NewTextHandler(w, o))
}

// onlyPrefix serves paths under prefix and nothing else.
func onlyPrefix(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
