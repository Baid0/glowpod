package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	var stderr bytes.Buffer
	go func() { done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-no-browser"}, &stderr) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatal("run did not stop after cancel")
	}
	if !bytes.Contains(stderr.Bytes(), []byte("glowpod is running")) {
		t.Fatalf("expected start-up log, got %q", stderr.String())
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	err := run(t.Context(), []string{"-addr", "0.0.0.0:0"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want error for non-loopback address")
	}
}
