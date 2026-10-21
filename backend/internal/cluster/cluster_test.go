package cluster

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const kubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster:
    server: https://192.0.2.10:6443
- name: prod
  cluster:
    server: https://prod.example.com
contexts:
- name: lab-admin
  context: {cluster: lab, user: admin}
- name: prod-ro
  context: {cluster: prod, user: viewer}
current-context: %s
users:
- name: admin
  user: {token: not-a-real-token}
- name: viewer
  user: {token: also-not-real}
`

func writeKubeconfig(t *testing.T, current string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	content := []byte(fmt.Sprintf(kubeconfig, current))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadKubeconfig(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		context    string
		wantName   string
		wantServer string
		wantErr    error
		anyErr     bool
	}{
		{name: "current context", current: "lab-admin", wantName: "lab-admin", wantServer: "192.0.2.10:6443"},
		{name: "explicit context wins", current: "lab-admin", context: "prod-ro", wantName: "prod-ro", wantServer: "prod.example.com"},
		{name: "no current context", current: `""`, wantErr: ErrNoContext},
		{name: "unknown context", current: "lab-admin", context: "nope", anyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, info, err := LoadKubeconfig(writeKubeconfig(t, tt.current), tt.context)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			case tt.anyErr:
				if err == nil {
					t.Fatal("want an error")
				}
				return
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
			if info.ID != DefaultID || info.Name != tt.wantName || info.Server != tt.wantServer {
				t.Fatalf("info = %+v", info)
			}
			if cfg.UserAgent != "glowpod" {
				t.Fatalf("user agent = %q", cfg.UserAgent)
			}
		})
	}
}

func TestLoadKubeconfigMissingFile(t *testing.T) {
	if _, _, err := LoadKubeconfig(filepath.Join(t.TempDir(), "missing"), ""); err == nil {
		t.Fatal("want an error for a missing kubeconfig")
	}
}
