package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// openBrowser opens url, which run builds from our own listener address.
func openBrowser(ctx context.Context, url string) error {
	if !strings.HasPrefix(url, "http://") {
		return fmt.Errorf("refusing to open non-http URL %q", url)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url) //nolint:gosec // fixed binary; url is our loopback address
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec // as above
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url) //nolint:gosec // as above
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
