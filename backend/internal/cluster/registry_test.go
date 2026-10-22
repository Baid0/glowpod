package cluster

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextIDs(t *testing.T) {
	long := "arn:aws:eks:eu-central-1:123456789012:cluster/payments-production-blue"
	tests := []struct {
		name  string
		names []string
		check func(t *testing.T, ids map[string]string)
	}{
		{
			name:  "readable slugs",
			names: []string{"kind-lab", "gke_shop_europe-west1_prod", "Docker Desktop"},
			check: func(t *testing.T, ids map[string]string) {
				want := map[string]string{"kind-lab": "kind-lab", "gke_shop_europe-west1_prod": "gke-shop-europe-west1-prod", "Docker Desktop": "docker-desktop"}
				for n, id := range want {
					if ids[n] != id {
						t.Errorf("%q = %q, want %q", n, ids[n], id)
					}
				}
			},
		},
		{
			name:  "names that slug the same get a hash each",
			names: []string{"lab", "LAB", "lab!"},
			check: func(t *testing.T, ids map[string]string) {
				seen := map[string]bool{}
				for n, id := range ids {
					if !strings.HasPrefix(id, "lab-") || seen[id] {
						t.Errorf("%q = %q", n, id)
					}
					seen[id] = true
				}
			},
		},
		{
			name:  "long and empty",
			names: []string{long, "日本"},
			check: func(t *testing.T, ids map[string]string) {
				if id := ids[long]; len(id) > maxIDLength || !strings.HasPrefix(id, "arn-aws-eks") {
					t.Errorf("long = %q", id)
				}
				if id := ids["日本"]; len(id) != 6 {
					t.Errorf("empty slug = %q, want a 6-character hash", id)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := ContextIDs(tt.names)
			again := ContextIDs(tt.names)
			for n := range ids {
				if ids[n] != again[n] {
					t.Fatalf("%q isn't stable: %q then %q", n, ids[n], again[n])
				}
			}
			tt.check(t, ids)
		})
	}
}

const twoContexts = `apiVersion: v1
kind: Config
current-context: kind-lab
clusters:
- name: lab
  cluster: {server: "https://127.0.0.1:6443"}
- name: prod
  cluster: {server: "https://prod.example.com", certificate-authority: /does/not/exist.pem}
users:
- name: me
  user: {token: not-a-real-token}
contexts:
- name: kind-lab
  context: {cluster: lab, user: me}
- name: prod
  context: {cluster: prod, user: me}
`

func TestLoadContexts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(twoContexts), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		current string
		wantErr error
		// failErr: any error will do.
		failErr     bool
		wantCurrent string
	}{
		{name: "kubeconfig's current context", path: path, wantCurrent: "kind-lab"},
		{name: "-context", path: path, current: "prod", wantCurrent: "prod"},
		{name: "unknown -context", path: path, current: "staging", failErr: true},
		{name: "no contexts", path: empty, wantErr: ErrNoContexts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := LoadContexts(tt.path, tt.current)
			if tt.wantErr != nil || tt.failErr {
				if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			list := r.List()
			if len(list) != 2 || list[0].ID != "kind-lab" || list[1].ID != "prod" || list[0].Server != "127.0.0.1:6443" {
				t.Fatalf("list = %+v", list)
			}
			if got := r.Current().Info().Name; got != tt.wantCurrent {
				t.Fatalf("current = %q, want %q", got, tt.wantCurrent)
			}
			// prod's CA file is missing: listed, and every request says why.
			prod, ok := r.Get("prod")
			if !ok {
				t.Fatal("prod isn't listed")
			}
			if _, err := prod.ClientsFor(t.Context(), Identity{}); err == nil {
				t.Fatal("prod should be unavailable")
			}
			if _, ok := r.Get("default"); ok {
				t.Fatal("no cluster is called default in local mode")
			}
		})
	}
}

func TestNewRegistryRejectsDuplicateIDs(t *testing.T) {
	a := NewUnavailable(Info{ID: "lab", Name: "a"}, nil)
	b := NewUnavailable(Info{ID: "lab", Name: "b"}, nil)
	if _, err := NewRegistry(a, b); err == nil {
		t.Fatal("two clusters with one id must be refused")
	}
}
