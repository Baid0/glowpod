// Command glowpod serves the Glowpod Kubernetes dashboard.
//
// In local mode it listens on a loopback address and opens the browser.
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
	"glowpod/internal/web"
)

const shutdownTimeout = 5 * time.Second

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

	srv := &http.Server{
		Handler: api.NewRouter(api.Options{
			Frontend: web.Handler(web.Dist()),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	url := "http://" + ln.Addr().String()
	if cfg.dev {
		logger.Info("glowpod API is running; open the Vite dev server", "api", url)
	} else {
		logger.Info("glowpod is running", "url", url)
		if !cfg.noBrowser {
			go func() {
				if err := openBrowser(ctx, url); err != nil {
					logger.Warn("couldn't open the browser; open the URL yourself", "url", url, "err", err)
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
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
