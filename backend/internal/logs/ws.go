package logs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/kerr"
)

// Message is one frame to the browser.
type Message struct {
	// Type is ready, lines, end or error.
	Type    string `json:"type"`
	Lines   []Line `json:"lines,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

const (
	writeTimeout = 10 * time.Second
	pingEvery    = 25 * time.Second
)

// Serve upgrades to a WebSocket and streams the logs until the container
// stops, the client leaves or something fails. Accept rejects other origins.
func Serve(w http.ResponseWriter, r *http.Request, kube kubernetes.Interface, namespace, pod string, opts Options) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow() //nolint:errcheck // best effort after a normal close
	ctx := conn.CloseRead(r.Context())

	write := func(m Message) bool {
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		return wsjson.Write(wctx, conn, m) == nil
	}
	end := func(m Message) {
		write(m)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}

	rc, err := Open(ctx, kube, namespace, pod, opts)
	if err != nil {
		code, msg := Explain(err, opts)
		end(Message{Type: "error", Code: code, Message: msg})
		return
	}
	defer rc.Close()
	if !write(Message{Type: "ready"}) {
		return
	}

	// Pings keep proxies from closing a quiet stream.
	pctx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go ping(pctx, conn)

	err = Pump(ctx, rc, func(lines []Line) bool { return write(Message{Type: "lines", Lines: lines}) })
	switch {
	case ctx.Err() != nil:
	case err != nil:
		end(Message{Type: "error", Code: "stream_failed", Message: "The log stream broke off."})
	case opts.Previous:
		end(Message{Type: "end", Message: "That's the whole previous run."})
	default:
		end(Message{Type: "end", Message: "The container stopped, so the log ended."})
	}
}

func ping(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(c)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// Explain turns a failure to open logs into a code and plain words.
func Explain(err error, opts Options) (code, message string) {
	var status apierrors.APIStatus
	if apierrors.IsBadRequest(err) && errors.As(err, &status) {
		// The API server says why: a container that hasn't started, no previous run, etc.
		text := status.Status().Message
		lower := strings.ToLower(text)
		switch {
		case opts.Previous && strings.Contains(lower, "previous terminated container"):
			return "no_previous", "There's no previous run of this container yet."
		case strings.Contains(lower, "is waiting to start"), strings.Contains(lower, "containercreating"),
			strings.Contains(lower, "podinitializing"):
			return "not_started", "This container hasn't started yet, so there are no logs."
		default:
			return "bad_request", text
		}
	}
	c := kerr.Classify(err)
	return c.Code, c.Message
}

// Download writes what the log holds now (no follow) as a text attachment.
// It only returns an error before anything is written; a copy cut short
// by the client just ends the response.
func Download(w http.ResponseWriter, r *http.Request, kube kubernetes.Interface, namespace, pod string, opts Options) error {
	opts.Once = true
	opts.TailLines = MaxTail
	rc, err := Open(r.Context(), kube, namespace, pod, opts)
	if err != nil {
		return err
	}
	defer rc.Close()
	name := pod
	if opts.Container != "" {
		name += "-" + opts.Container
	}
	if opts.Previous {
		name += "-previous"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.log"`)
	_, _ = io.Copy(w, rc)
	return nil
}
