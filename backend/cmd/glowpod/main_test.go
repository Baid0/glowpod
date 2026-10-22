package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster: {server: "https://192.0.2.10:6443"}
contexts:
- name: lab-admin
  context: {cluster: lab, user: admin}
current-context: lab-admin
users:
- name: admin
  user: {token: not-a-real-token}
`

func kubeconfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// syncBuffer is a bytes.Buffer safe to read while run writes to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var launchLink = regexp.MustCompile(`http://127\.0\.0\.1:\d+/auth\?token=\S+`)

func TestRunServesAndSignsIn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	var stderr syncBuffer
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-no-browser", "-kubeconfig", kubeconfigFile(t)}, io.Discard, &stderr)
	}()

	var link string
	for deadline := time.Now().Add(3 * time.Second); link == "" && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		link = launchLink.FindString(stderr.String())
	}
	if link == "" {
		t.Fatalf("no launch link printed: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "not-a-real-token") {
		t.Fatal("kubeconfig credentials leaked into the output")
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := link[:strings.Index(link, "/auth")]

	get := func(u string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		return client.Do(req)
	}

	resp, err := get(base + "/api/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("API without sign-in = %d, want 401", resp.StatusCode)
	}

	resp, err = get(link)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) != 1 {
		t.Fatalf("sign-in = %d with cookies %v", resp.StatusCode, resp.Cookies())
	}

	client.CloseIdleConnections()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatal("run did not stop after cancel")
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "non-loopback address", args: []string{"-addr", "0.0.0.0:0"}, want: "loopback"},
		{name: "missing kubeconfig", args: []string{"-addr", "127.0.0.1:0", "-kubeconfig", "/nonexistent/config"}, want: "kubeconfig"},
		{name: "unknown context", args: []string{"-addr", "127.0.0.1:0", "-kubeconfig", "KUBECONFIG", "-context", "nope"}, want: `"nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args
			for i, a := range args {
				if a == "KUBECONFIG" {
					args[i] = kubeconfigFile(t)
				}
			}
			err := run(t.Context(), args, io.Discard, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %s", err, tt.want)
			}
		})
	}
}
