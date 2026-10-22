package terminal

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilexec "k8s.io/client-go/util/exec"
)

const token = "csrf-token"

// echo is a fake shell: it reports the first size, echoes lines, and
// exits with the code a line "exit N" asks for.
func echo(t *testing.T) Starter {
	return func(ctx context.Context, req Request, s Streams) error {
		if req.Container != "app" {
			t.Errorf("container = %q", req.Container)
		}
		size := s.Sizes.Next()
		if _, err := s.Stdout.Write([]byte("size " + itoa(int(size.Width)) + "x" + itoa(int(size.Height)) + "\r\n")); err != nil {
			return err
		}
		go func() { // later sizes
			for sz := s.Sizes.Next(); sz != nil; sz = s.Sizes.Next() {
				_, _ = s.Stdout.Write([]byte("resized " + itoa(int(sz.Width)) + "\r\n"))
			}
		}()
		sc := bufio.NewScanner(s.Stdin)
		for sc.Scan() {
			line := sc.Text()
			if code, ok := strings.CutPrefix(line, "exit "); ok {
				if code == "0" {
					return nil
				}
				return utilexec.CodeExitError{Err: errors.New("exited"), Code: 3}
			}
			if _, err := s.Stdout.Write([]byte("echo " + line + "\r\n")); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
}

func dial(t *testing.T, start Starter) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(w, r, start, Request{Namespace: "shop", Pod: "web-1", Container: "app"}, token)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// next reads one message: output text, or a control message's type.
func next(t *testing.T, conn *websocket.Conn) (string, Message) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ == websocket.MessageBinary {
		return string(data), Message{}
	}
	var m Message
	if err := unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return "", m
}

func TestServe(t *testing.T) {
	conn := dial(t, echo(t))
	ctx := t.Context()
	if err := wsjson.Write(ctx, conn, Message{Type: "start", CSRF: token, Cols: 120, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	if _, m := next(t, conn); m.Type != "ready" {
		t.Fatalf("first message = %+v", m)
	}
	if out, _ := next(t, conn); out != "size 120x40\r\n" {
		t.Fatalf("out = %q", out)
	}

	if err := wsjson.Write(ctx, conn, Message{Type: "resize", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	if out, _ := next(t, conn); out != "resized 80\r\n" {
		t.Fatalf("out = %q", out)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	if out, _ := next(t, conn); out != "echo ls\r\n" {
		t.Fatalf("out = %q", out)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("exit 3\n")); err != nil {
		t.Fatal(err)
	}
	_, m := next(t, conn)
	if m.Type != "exit" || m.Exit == nil || *m.Exit != 3 {
		t.Fatalf("exit = %+v", m)
	}
}

func TestServeNeedsTheToken(t *testing.T) {
	tests := []struct {
		name  string
		first Message
	}{
		{"wrong token", Message{Type: "start", CSRF: "guess", Cols: 80, Rows: 24}},
		{"no start", Message{Type: "resize", CSRF: token, Cols: 80, Rows: 24}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := false
			conn := dial(t, func(context.Context, Request, Streams) error {
				started = true
				return nil
			})
			if err := wsjson.Write(t.Context(), conn, tt.first); err != nil {
				t.Fatal(err)
			}
			if _, m := next(t, conn); m.Type != "error" || m.Code != "csrf" {
				t.Fatalf("message = %+v", m)
			}
			if started {
				t.Fatal("the shell must not start")
			}
		})
	}
}

func TestExitMessage(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType string
		wantCode string
		wantExit int
	}{
		{name: "clean exit", err: nil, wantType: "exit", wantExit: 0},
		{name: "exit code", err: utilexec.CodeExitError{Err: errors.New("x"), Code: 2}, wantType: "exit", wantExit: 2},
		{name: "no shell (127)", err: utilexec.CodeExitError{Err: errors.New("x"), Code: 127}, wantType: "error", wantCode: "no_shell"},
		{name: "no shell (runtime)", err: errors.New(`exec: "/bin/sh": stat /bin/sh: no such file or directory`), wantType: "error", wantCode: "no_shell"},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods/exec"}, "web-1", errors.New("rbac")), wantType: "error", wantCode: "forbidden"},
		{name: "other", err: errors.New("stream reset"), wantType: "error", wantCode: "exec_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := exitMessage(tt.err)
			if m.Type != tt.wantType || m.Code != tt.wantCode || m.Message == "" {
				t.Fatalf("got %+v", m)
			}
			if tt.wantType == "exit" && *m.Exit != tt.wantExit {
				t.Fatalf("exit = %d", *m.Exit)
			}
		})
	}
}

func TestSizeQueueKeepsTheLatest(t *testing.T) {
	q := newSizeQueue()
	q.push(10, 10)
	q.push(20, 20)
	q.push(0, 5)    // ignored
	q.push(5000, 5) // ignored
	if s := q.Next(); s.Width != 20 {
		t.Fatalf("size = %+v", s)
	}
	q.close()
	if s := q.Next(); s != nil {
		t.Fatalf("closed queue gave %+v", s)
	}
}
