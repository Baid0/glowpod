package logs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		raw  string
		want Line
	}{
		{"2026-10-22T03:00:00.123456789Z hello world\n", Line{Time: "2026-10-22T03:00:00.123456789Z", Text: "hello world"}},
		{"2026-10-22T03:00:00Z ", Line{Time: "2026-10-22T03:00:00Z", Text: ""}},
		{"no timestamp here", Line{Text: "no timestamp here"}},
		{"2026 not a time", Line{Text: "2026 not a time"}},
		{"windows line\r\n", Line{Text: "windows line"}},
	}
	for _, tt := range tests {
		if got := ParseLine(tt.raw); got != tt.want {
			t.Errorf("ParseLine(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
}

func TestPumpBatches(t *testing.T) {
	var b strings.Builder
	for i := range 1200 {
		b.WriteString("2026-10-22T03:00:00Z line ")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\n")
	}
	var batches [][]Line
	err := Pump(context.Background(), strings.NewReader(b.String()), func(l []Line) bool {
		batches = append(batches, l)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, batch := range batches {
		if len(batch) > batchLines {
			t.Fatalf("batch of %d lines, max %d", len(batch), batchLines)
		}
		total += len(batch)
	}
	if total != 1200 || len(batches) < 3 {
		t.Fatalf("total = %d in %d batches", total, len(batches))
	}
}

func TestPumpCutsHugeLines(t *testing.T) {
	huge := strings.Repeat("a", maxLineBytes*3)
	var got []Line
	_ = Pump(context.Background(), strings.NewReader(huge+"\nnext\n"), func(l []Line) bool {
		got = append(got, l...)
		return true
	})
	if len(got) != 2 || len(got[0].Text) != maxLineBytes+len(" …") || got[1].Text != "next" {
		t.Fatalf("got %d lines, first %d bytes", len(got), len(got[0].Text))
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestPumpStops(t *testing.T) {
	if err := Pump(context.Background(), failingReader{}, func([]Line) bool { return true }); err == nil {
		t.Fatal("a read error should be returned")
	}
	calls := 0
	_ = Pump(context.Background(), strings.NewReader(strings.Repeat("l\n", 2000)), func([]Line) bool {
		calls++
		return false
	})
	if calls != 1 {
		t.Fatalf("send was called %d times after returning false", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	defer pw.Close()
	done := make(chan error, 1)
	go func() { done <- Pump(ctx, pr, func([]Line) bool { return true }) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pump didn't stop on cancel")
	}
}

func TestExplain(t *testing.T) {
	bad := func(msg string) error {
		return apierrors.NewBadRequest(msg)
	}
	tests := []struct {
		name string
		err  error
		opts Options
		code string
	}{
		{"no previous run", bad(`previous terminated container "app" in pod "x" not found`), Options{Previous: true}, "no_previous"},
		{"not started", bad(`container "app" in pod "x" is waiting to start: ContainerCreating`), Options{}, "not_started"},
		{"other bad request", bad(`a container name must be specified`), Options{}, "bad_request"},
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "pods/log"}, "x", errors.New("rbac")), Options{}, "forbidden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := Explain(tt.err, tt.opts)
			if code != tt.code || msg == "" {
				t.Fatalf("got %s %q, want %s", code, msg, tt.code)
			}
		})
	}
}

func TestServe(t *testing.T) {
	// The fake clientset answers every log request with "fake logs".
	kube := fake.NewClientset()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(w, r, kube, "payments", "api-1", Options{Container: "app"})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	defer c.CloseNow() //nolint:errcheck

	var types []string
	var lines []Line
	for {
		var m Message
		if err := wsjson.Read(ctx, c, &m); err != nil {
			break
		}
		types = append(types, m.Type)
		lines = append(lines, m.Lines...)
	}
	if strings.Join(types, ",") != "ready,lines,end" {
		t.Fatalf("messages = %v", types)
	}
	if len(lines) != 1 || lines[0].Text != "fake logs" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestDownload(t *testing.T) {
	kube := fake.NewClientset()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if err := Download(rec, req, kube, "payments", "api-1", Options{Container: "app", Previous: true}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "fake logs" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="api-1-app-previous.log"` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
}
