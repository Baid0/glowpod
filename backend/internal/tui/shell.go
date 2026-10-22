package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
	"k8s.io/client-go/tools/remotecommand"

	"glowpod/internal/terminal"
)

// shellCmd hands the terminal to a shell in the container and takes it back
// when the shell exits. An empty container means the pod's main one.
func shellCmd(a *app, ns, pod, container string) tea.Cmd {
	return tea.Exec(&shellExec{a: a, req: terminal.Request{Namespace: ns, Pod: pod, Container: container}},
		func(err error) tea.Msg {
			if err != nil {
				return actionDone{err: err}
			}
			return actionDone{text: "Shell closed"}
		})
}

// shellExec implements tea.ExecCommand.
type shellExec struct {
	a      *app
	req    terminal.Request
	stdin  io.Reader
	stdout io.Writer
}

func (s *shellExec) SetStdin(r io.Reader)  { s.stdin = r }
func (s *shellExec) SetStdout(w io.Writer) { s.stdout = w }
func (s *shellExec) SetStderr(io.Writer)   {}

func (s *shellExec) Run() error {
	ctx, cancel := context.WithCancel(s.a.ctx)
	defer cancel()
	if s.req.Container == "" {
		d, err := s.a.src.Pod(ctx, s.req.Namespace, s.req.Pod)
		if err != nil {
			return err
		}
		s.req.Container = defaultContainer(d.Containers)
	}
	fmt.Fprintf(s.stdout, "\r\nShell in %s/%s (%s). Type exit to go back to Glowpod.\r\n\r\n", s.req.Namespace, s.req.Pod, s.req.Container)

	if f, ok := s.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		old, err := term.MakeRaw(int(f.Fd()))
		if err != nil {
			return fmt.Errorf("raw terminal: %w", err)
		}
		defer func() { _ = term.Restore(int(f.Fd()), old) }()
	}
	// The exec stream keeps reading stdin after the shell exits; cancel that
	// read so it doesn't swallow the next key meant for Glowpod.
	in, err := cancelreader.NewReader(s.stdin)
	if err != nil {
		return fmt.Errorf("read keys: %w", err)
	}
	defer in.Cancel()

	return s.a.src.Shell(ctx, s.req, terminal.Streams{Stdin: in, Stdout: s.stdout, Sizes: newSizes(ctx, s.stdout)})
}

// sizes reports the terminal size to the shell, and every change to it.
// It polls, so it works the same on every OS.
type sizes struct {
	ctx  context.Context
	fd   int
	ok   bool
	mu   sync.Mutex
	last remotecommand.TerminalSize
}

func newSizes(ctx context.Context, out io.Writer) *sizes {
	s := &sizes{ctx: ctx}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		s.fd, s.ok = int(f.Fd()), true
	}
	return s
}

// Next implements remotecommand.TerminalSizeQueue; nil ends it.
func (s *sizes) Next() *remotecommand.TerminalSize {
	if !s.ok {
		return nil
	}
	for {
		w, h, err := term.GetSize(s.fd)
		if err == nil {
			size := remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)} //nolint:gosec // terminal sizes fit
			s.mu.Lock()
			changed := size != s.last
			s.last = size
			s.mu.Unlock()
			if changed {
				return &size
			}
		}
		select {
		case <-s.ctx.Done():
			return nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}
