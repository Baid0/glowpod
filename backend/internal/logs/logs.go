// Package logs streams container logs from the API server to the browser,
// as the signed-in user, in small batches over a WebSocket.
package logs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Limits that keep one noisy container from swamping the browser.
const (
	DefaultTail  = 1000
	MaxTail      = 10_000
	maxLineBytes = 64 << 10
	batchLines   = 500
	batchEvery   = 100 * time.Millisecond
)

// Options pick what to read.
type Options struct {
	Container string
	// Previous reads the last terminated run instead of following the current one.
	Previous bool
	// Once reads what's there and stops, without following (downloads).
	Once      bool
	TailLines int64
}

// Line is one log line. Time is the API server's timestamp (RFC 3339).
type Line struct {
	Time string `json:"t,omitempty"`
	Text string `json:"x"`
}

// Open starts reading. Following the current run streams until the
// container stops; a previous run, or Once, is read once.
func Open(ctx context.Context, kube kubernetes.Interface, namespace, pod string, opts Options) (io.ReadCloser, error) {
	tail := opts.TailLines
	if tail <= 0 || tail > MaxTail {
		tail = DefaultTail
	}
	rc, err := kube.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     !opts.Previous && !opts.Once,
		Previous:   opts.Previous,
		TailLines:  &tail,
		Timestamps: true,
	}).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("read logs of %s/%s: %w", namespace, pod, err)
	}
	return rc, nil
}

// ParseLine splits the timestamp Kubernetes puts in front of each line.
func ParseLine(raw string) Line {
	raw = strings.TrimRight(raw, "\r\n")
	if ts, rest, ok := strings.Cut(raw, " "); ok && looksLikeTime(ts) {
		return Line{Time: ts, Text: rest}
	}
	return Line{Text: raw}
}

func looksLikeTime(s string) bool {
	return len(s) >= 20 && s[4] == '-' && s[10] == 'T' && strings.HasSuffix(s, "Z")
}

// Pump reads lines from r and calls send with batches: every batchEvery, or
// sooner when a batch fills up. It returns when r ends (nil), ctx ends, or
// send returns false.
func Pump(ctx context.Context, r io.Reader, send func([]Line) bool) error {
	lines := make(chan Line, batchLines)
	readErr := make(chan error, 1)
	go func() {
		defer close(lines)
		br := bufio.NewReaderSize(r, 64<<10)
		for {
			raw, err := readLine(br)
			if raw != "" {
				select {
				case lines <- ParseLine(raw):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					readErr <- err
				}
				return
			}
		}
	}()

	tick := time.NewTicker(batchEvery)
	defer tick.Stop()
	var batch []Line
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		ok := send(batch)
		batch = nil
		return ok
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // plain cancellation
		case l, open := <-lines:
			if !open {
				if !flush() {
					return nil
				}
				select {
				case err := <-readErr:
					return err
				default:
					return nil
				}
			}
			batch = append(batch, l)
			if len(batch) >= batchLines && !flush() {
				return nil
			}
		case <-tick.C:
			if !flush() {
				return nil
			}
		}
	}
}

// readLine reads one line, cutting very long lines at maxLineBytes and
// discarding the rest of them.
func readLine(br *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		chunk, isPrefix, err := br.ReadLine()
		if b.Len() < maxLineBytes {
			room := maxLineBytes - b.Len()
			if len(chunk) > room {
				chunk = chunk[:room]
			}
			b.Write(chunk)
		}
		if err != nil {
			return b.String(), err //nolint:wrapcheck // io.EOF is checked by the caller
		}
		if !isPrefix {
			if b.Len() >= maxLineBytes {
				b.WriteString(" …")
			}
			return b.String(), nil
		}
	}
}
