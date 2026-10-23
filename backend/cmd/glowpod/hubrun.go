package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"glowpod/internal/hub"
	"glowpod/internal/quickstart"
)

const hubUsage = `glowpod hub: Glowpod Hub in one container, with K3s inside (the glowpod-hub image).

  glowpod hub run                  start K3s and the Hub (the image's entrypoint)
  glowpod hub bootstrap-password   print the first-start password

Set the Hub's other options with GLOWPOD_<FLAG> variables, such as
GLOWPOD_PUBLIC_URL or GLOWPOD_OIDC_ISSUER (see glowpod -h).
`

// runHub is glowpod hub.
func runHub(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, hubUsage)
		return nil
	}
	switch args[0] {
	case "run":
		return runHubServer(ctx, args[1:], stderr)
	case "bootstrap-password":
		return printBootstrapPassword(ctx, args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stderr, hubUsage)
		return nil
	}
	return fmt.Errorf("unknown command %q; see glowpod hub -h", args[0])
}

func dataDirFlag(fs *flag.FlagSet) *string {
	def := "/var/lib/glowpod"
	if v := os.Getenv("GLOWPOD_DATA_DIR"); v != "" {
		def = v
	}
	return fs.String("data-dir", def, "where K3s, the CA and everything else are kept (a volume)")
}

// runHubServer starts K3s, waits for it, and serves the Hub on it over
// HTTPS, with plain HTTP redirecting there.
func runHubServer(ctx context.Context, args []string, stderr io.Writer) error {
	f := flag.NewFlagSet("glowpod hub run", flag.ContinueOnError)
	f.SetOutput(stderr)
	dataDir := dataDirFlag(f)
	tlsDir := f.String("tls-dir", "/etc/glowpod/tls", "your own certificate: tls.crt and tls.key, and ca.crt if a private CA signed it")
	httpsAddr := f.String("https-addr", ":443", "HTTPS address")
	httpAddr := f.String("http-addr", ":80", "plain-HTTP address, which redirects to HTTPS; empty to turn off")
	k3s := f.String("k3s", "k3s", "the k3s binary")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err //nolint:wrapcheck // flag errors are already user-facing
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", f.Arg(0))
	}
	paths := quickstart.Paths{Data: *dataDir}

	tlsCfg, caFile, certs, err := hubTLS(paths, *tlsDir)
	if err != nil {
		return err
	}
	serverArgs := []string{
		"-mode=server", "-hub",
		"-kubeconfig", paths.Kubeconfig(),
		"-hub-namespace", quickstart.Namespace,
		"-addr", *httpsAddr,
	}
	if caFile != "" {
		serverArgs = append(serverArgs, "-hub-ca-file", caFile)
	}
	cfg, err := parseFlags(serverArgs, stderr)
	if err != nil {
		return err
	}
	cfg.tls = tlsCfg
	cfg.server.bootstrapHint = "docker exec <container> glowpod hub bootstrap-password"
	logger := newLogger(cfg, stderr)
	logger.Info("starting glowpod hub", "version", version, "data_dir", paths.Data)

	if err := quickstart.PrepareCgroups(); err != nil {
		return err
	}
	if err := quickstart.WriteManifests(paths); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	k3sDone, err := quickstart.StartK3s(ctx, *k3s, paths)
	if err != nil {
		return err
	}
	logger.Info("starting Kubernetes inside the container (K3s); a first start takes about a minute", "k3s_log", paths.K3sLog())
	kube, err := waitForKubeconfig(ctx, paths, k3sDone)
	if err != nil {
		return err
	}
	if err := quickstart.WaitReady(ctx, kube, k3sDone, logger); err != nil {
		return err
	}
	if certs != nil {
		certs.Names = serverURLHost(ctx, kube, logger)
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	if *httpAddr != "" {
		redirect := &http.Server{
			Addr:              *httpAddr,
			Handler:           quickstart.Redirect(cfg.server.publicURL),
			ReadHeaderTimeout: 10 * time.Second,
		}
		wg.Go(func() {
			if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Warn("plain-HTTP redirect stopped", "err", err)
			}
		})
		context.AfterFunc(ctx, func() { _ = redirect.Close() })
	}

	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, cfg, logger, stderr) }()
	select {
	case err := <-errc:
		cancel()
		// K3s gets SIGTERM; its exit status says only that.
		k3sErr := <-k3sDone
		logger.Debug("k3s stopped", "err", k3sErr)
		return err
	case err := <-k3sDone:
		cancel()
		serveErr := <-errc
		logger.Debug("the Hub stopped", "err", serveErr)
		return fmt.Errorf("k3s stopped, so the Hub stops too (see %s): %w", paths.K3sLog(), errOrNil(err))
	}
}

func errOrNil(err error) error {
	if err == nil {
		return errors.New("it exited")
	}
	return err
}

// hubTLS serves your own certificate from tlsDir when there is one, or
// certificates from the Hub's own CA. caFile is what agents should trust:
// "" when your certificate is publicly trusted.
func hubTLS(paths quickstart.Paths, tlsDir string) (*tls.Config, string, *quickstart.Certs, error) {
	certFile, keyFile := filepath.Join(tlsDir, "tls.crt"), filepath.Join(tlsDir, "tls.key")
	if _, err := os.Stat(certFile); err == nil {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, "", nil, fmt.Errorf("load your certificate from %s: %w", tlsDir, err)
		}
		caFile := filepath.Join(tlsDir, "ca.crt")
		if _, err := os.Stat(caFile); errors.Is(err, fs.ErrNotExist) {
			caFile = ""
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}, caFile, nil, nil
	}
	ca, err := quickstart.LoadOrCreateCA(paths.TLS())
	if err != nil {
		return nil, "", nil, err
	}
	certs := &quickstart.Certs{CA: ca}
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: certs.GetCertificate}, quickstart.CAPath(paths.TLS()), certs, nil
}

// waitForKubeconfig waits for K3s to write its kubeconfig, and connects.
func waitForKubeconfig(ctx context.Context, paths quickstart.Paths, k3sDone <-chan error) (kubernetes.Interface, error) {
	for {
		if _, err := os.Stat(paths.Kubeconfig()); err == nil {
			restCfg, err := clientcmd.BuildConfigFromFlags("", paths.Kubeconfig())
			if err != nil {
				return nil, fmt.Errorf("read K3s's kubeconfig: %w", err)
			}
			restCfg.Timeout = 10 * time.Second
			kube, err := kubernetes.NewForConfig(restCfg)
			if err != nil {
				return nil, fmt.Errorf("API client: %w", err)
			}
			return kube, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for K3s: %w", ctx.Err())
		case err := <-k3sDone:
			return nil, fmt.Errorf("k3s stopped (see %s): %w", paths.K3sLog(), errOrNil(err))
		case <-time.After(time.Second):
		}
	}
}

// serverURLHost keeps the server URL's host for the certificate, read from
// the Hub's settings every 15 seconds: it's set at first start.
func serverURLHost(ctx context.Context, kube kubernetes.Interface, logger *slog.Logger) func() []string {
	var mu sync.Mutex
	var host string
	read := func() {
		raw, err := hub.ServerURL(ctx, kube, quickstart.Namespace)
		if err != nil {
			logger.Debug("read the server URL", "err", err)
			return
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return
		}
		mu.Lock()
		host = u.Hostname()
		mu.Unlock()
	}
	read()
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				read()
			}
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		if host == "" {
			return nil
		}
		return []string{host}
	}
}

// printBootstrapPassword is glowpod hub bootstrap-password, for docker exec.
// It prints to the terminal of whoever asks, never to the log.
func printBootstrapPassword(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f := flag.NewFlagSet("glowpod hub bootstrap-password", flag.ContinueOnError)
	f.SetOutput(stderr)
	dataDir := dataDirFlag(f)
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err //nolint:wrapcheck // flag errors are already user-facing
	}
	paths := quickstart.Paths{Data: *dataDir}
	restCfg, err := clientcmd.BuildConfigFromFlags("", paths.Kubeconfig())
	if err != nil {
		return fmt.Errorf("the Hub isn't running yet, or not in this container: %w", err)
	}
	restCfg.Timeout = 10 * time.Second
	kube, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("API client: %w", err)
	}
	pw, err := hub.BootstrapPassword(ctx, kube, quickstart.Namespace)
	if err != nil {
		return fmt.Errorf("read the bootstrap password (is the Hub still starting?): %w", err)
	}
	if pw == "" {
		fmt.Fprintln(stderr, "First start is done, so there's no bootstrap password any more. Sign in as admin with the password you chose.")
		return nil
	}
	fmt.Fprintln(stdout, pw)
	return nil
}
