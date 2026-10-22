package terminal

import (
	"encoding/json"
	"strconv"
	"sync"

	"k8s.io/client-go/tools/remotecommand"
)

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) } //nolint:wrapcheck // callers only check for nil

func itoa(n int) string { return strconv.Itoa(n) }

// sizeQueue hands the latest terminal size to the executor. Sizes that
// arrive faster than it reads replace each other.
type sizeQueue struct {
	ch   chan remotecommand.TerminalSize
	done chan struct{}
	once sync.Once
}

func newSizeQueue() *sizeQueue {
	return &sizeQueue{ch: make(chan remotecommand.TerminalSize, 1), done: make(chan struct{})}
}

func (q *sizeQueue) push(cols, rows int) {
	if cols < 1 || rows < 1 || cols > maxSize || rows > maxSize {
		return
	}
	s := remotecommand.TerminalSize{Width: uint16(cols), Height: uint16(rows)} //nolint:gosec // bounded by maxSize above
	for {
		select {
		case q.ch <- s:
			return
		default:
		}
		select {
		case <-q.ch: // drop the stale size
		default:
		}
	}
}

// Next implements remotecommand.TerminalSizeQueue.
func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &s
	case <-q.done:
		return nil
	}
}

func (q *sizeQueue) close() { q.once.Do(func() { close(q.done) }) }
