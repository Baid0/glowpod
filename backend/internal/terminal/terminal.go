// Package terminal runs an interactive shell in a container and connects it
// to the browser over a WebSocket.
//
// Protocol: the browser's first message is a JSON "start" carrying the
// session's CSRF token and the terminal size. After that, binary messages
// are keystrokes (stdin) and text messages are JSON control ("resize").
// The server sends the terminal's output as binary messages and JSON text
// messages for "ready", "exit" and "error".
package terminal

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy" //nolint:staticcheck // client-go 0.37's Upgrader still takes this package's Connection
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
	"k8s.io/streaming/pkg/httpstream"

	"glowpod/internal/kerr"
)

// ShellCommand starts bash when the image has it, else sh.
var ShellCommand = []string{
	"/bin/sh", "-c",
	"TERM=xterm-256color; export TERM; command -v bash >/dev/null 2>&1 && exec bash || exec sh",
}

const (
	startTimeout = 10 * time.Second
	writeTimeout = 10 * time.Second
	// readLimit fits a large paste in one message.
	readLimit = 1 << 20
	maxSize   = 1000
)

// Request says which container to open a shell in.
type Request struct {
	Namespace string
	Pod       string
	Container string
}

// Streams connect the shell to the browser.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Sizes  remotecommand.TerminalSizeQueue
}

// Starter runs the shell and returns when it exits.
type Starter func(ctx context.Context, req Request, s Streams) error

// Remote runs the shell through the API server's pods/exec, as the
// identity cfg and kube belong to. It speaks WebSocket and falls back to
// SPDY for older API servers.
func Remote(cfg *rest.Config, kube kubernetes.Interface) Starter {
	return func(ctx context.Context, req Request, s Streams) error {
		u := kube.CoreV1().RESTClient().Post().
			Resource("pods").Namespace(req.Namespace).Name(req.Pod).SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: req.Container,
				Command:   ShellCommand,
				Stdin:     true,
				Stdout:    true,
				TTY:       true,
			}, scheme.ParameterCodec).URL()
		opts := remotecommand.StreamOptions{Stdin: s.Stdin, Stdout: s.Stdout, Tty: true, TerminalSizeQueue: s.Sizes}
		if cfg.Dial != nil {
			// The config dials its own way (a Hub's tunnel to an agent). The
			// WebSocket executor can't use a custom dialer; SPDY can.
			ex, err := spdyOver(cfg, u)
			if err != nil {
				return err
			}
			return ex.StreamWithContext(ctx, opts)
		}
		ws, err := remotecommand.NewWebSocketExecutor(cfg, http.MethodGet, u.String())
		if err != nil {
			return err
		}
		spdy, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, u)
		if err != nil {
			return err
		}
		ex, err := remotecommand.NewFallbackExecutor(ws, spdy, func(err error) bool {
			return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
		})
		if err != nil {
			return err
		}
		return ex.StreamWithContext(ctx, opts)
	}
}

// spdyOver is an exec executor that dials with cfg.Dial and adds cfg's
// credentials and impersonation, as every other request does.
func spdyOver(cfg *rest.Config, u *url.URL) (remotecommand.Executor, error) {
	upgrader, err := spdy.NewRoundTripperWithConfig(spdy.RoundTripperConfig{
		UpgradeTransport: &http.Transport{DialContext: cfg.Dial},
		PingPeriod:       5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("exec transport: %w", err)
	}
	wrapped, err := rest.HTTPWrappersForConfig(cfg, upgrader)
	if err != nil {
		return nil, fmt.Errorf("exec transport: %w", err)
	}
	return remotecommand.NewSPDYExecutorForTransports(wrapped, upgrader, http.MethodPost, u) //nolint:wrapcheck // only fails on a bad URL
}

// Message is a JSON control message in either direction.
type Message struct {
	// Type is start, resize (from the browser) or ready, exit, error.
	Type    string `json:"type"`
	CSRF    string `json:"csrf,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Code    string `json:"code,omitempty"`
	Exit    *int   `json:"exit,omitempty"`
	Message string `json:"message,omitempty"`
}

// Serve upgrades to a WebSocket and runs the shell until it exits or the
// browser leaves. Accept rejects other origins; the start message must
// also carry csrf, the session's token.
func Serve(w http.ResponseWriter, r *http.Request, start Starter, req Request, csrf string) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow() //nolint:errcheck // best effort after a normal close
	conn.SetReadLimit(readLimit)
	ctx, stop := context.WithCancel(r.Context())
	defer stop()

	var mu sync.Mutex
	send := func(m Message) {
		mu.Lock()
		defer mu.Unlock()
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		_ = wsjson.Write(wctx, conn, m)
	}

	sctx, cancel := context.WithTimeout(ctx, startTimeout)
	var first Message
	err = wsjson.Read(sctx, conn, &first)
	cancel()
	if err != nil || first.Type != "start" || csrf == "" ||
		subtle.ConstantTimeCompare([]byte(first.CSRF), []byte(csrf)) != 1 {
		send(Message{Type: "error", Code: "csrf", Message: "This terminal wasn't opened by Glowpod. Reload the page and try again."})
		_ = conn.Close(websocket.StatusPolicyViolation, "")
		return
	}

	sizes := newSizeQueue()
	sizes.push(first.Cols, first.Rows)
	stdin, stdinW := io.Pipe()
	go func() {
		defer stop()
		defer stdinW.Close()
		defer sizes.close()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if _, err := stdinW.Write(data); err != nil {
					return
				}
				continue
			}
			var m Message
			if jsonErr := unmarshal(data, &m); jsonErr == nil && m.Type == "resize" {
				sizes.push(m.Cols, m.Rows)
			}
		}
	}()

	send(Message{Type: "ready"})
	out := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		if err := conn.Write(wctx, websocket.MessageBinary, p); err != nil {
			return 0, fmt.Errorf("send output: %w", err)
		}
		return len(p), nil
	})

	err = start(ctx, req, Streams{Stdin: stdin, Stdout: out, Sizes: sizes})
	if ctx.Err() != nil {
		return // the browser left
	}
	send(exitMessage(err))
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// exitMessage says how the shell ended, in plain words.
func exitMessage(err error) Message {
	var exit utilexec.CodeExitError
	switch {
	case err == nil:
		code := 0
		return Message{Type: "exit", Exit: &code, Message: "The shell exited."}
	case errors.As(err, &exit) && exit.Code != 126 && exit.Code != 127:
		code := exit.Code
		return Message{Type: "exit", Exit: &code, Message: "The shell exited with code " + itoa(code) + "."}
	case noShell(err):
		return Message{Type: "error", Code: "no_shell", Message: "This container has no shell (/bin/sh). Minimal images, such as distroless ones, often don't."}
	case apierrors.IsBadRequest(err) && strings.Contains(err.Error(), "container not found"),
		strings.Contains(err.Error(), "not running"), strings.Contains(err.Error(), "is not valid for pod"):
		return Message{Type: "error", Code: "not_running", Message: "This container isn't running, so there's nothing to open a shell in."}
	default:
		c := kerr.Classify(err)
		if c.Code == "cluster_error" {
			return Message{Type: "error", Code: "exec_failed", Message: "The terminal connection broke off."}
		}
		return Message{Type: "error", Code: c.Code, Message: c.Message}
	}
}

func noShell(err error) bool {
	var exit utilexec.CodeExitError
	if errors.As(err, &exit) && (exit.Code == 126 || exit.Code == 127) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") || strings.Contains(msg, "no such file or directory")
}
