package quickstart

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The CRD the container applies must be the chart's: one Hub, two installs.
func TestManifestsMatchTheChart(t *testing.T) {
	chart, err := os.ReadFile("../../../deploy/helm/glowpod/templates/hub.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec := func(doc string) string {
		start := strings.Index(doc, "spec:\n  group: hub.glowpod.dev")
		if start < 0 {
			t.Fatal("no Cluster CRD spec")
		}
		end := strings.Index(doc[start:], "---")
		return strings.TrimSpace(doc[start : start+end])
	}
	if spec(string(chart)) != spec(string(manifests)) {
		t.Fatal("manifests.yaml's CRD spec differs from the chart's; copy it over")
	}
	for _, want := range []string{"kind: Namespace\nmetadata:\n  name: " + Namespace, `name: "glowpod:admin"`} {
		if !strings.Contains(string(manifests), want) {
			t.Fatalf("manifests.yaml lacks %q", want)
		}
	}
}

func TestWriteManifests(t *testing.T) {
	p := Paths{Data: t.TempDir()}
	if err := WriteManifests(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(p.K3s(), "server", "manifests", "glowpod-hub.yaml"))
	if err != nil || string(got) != string(manifests) {
		t.Fatalf("K3s's auto-deploy folder should get the manifests: %v", err)
	}
	args := strings.Join(K3sArgs(p), " ")
	for _, want := range []string{"--data-dir " + p.K3s(), "--write-kubeconfig " + p.Kubeconfig(), "--disable traefik", "--node-name glowpod-hub"} {
		if !strings.Contains(args, want) {
			t.Errorf("k3s args %q lack %q", args, want)
		}
	}
}

func TestCAIsKeptAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.PEM) != string(again.PEM) {
		t.Fatal("a restart must keep the CA, or every imported agent stops trusting the Hub")
	}
	info, err := os.Stat(filepath.Join(dir, caKeyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the CA key is readable by its owner only: %v %v", info.Mode(), err)
	}
}

func TestCertsCoverTheNamesAgentsUse(t *testing.T) {
	ca, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	certs := &Certs{CA: ca, Names: func() []string { return []string{"192.168.1.20"} }}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.PEM)

	tests := []struct {
		name string
		sni  string
		host string // what the client verifies
	}{
		{"agent by IP sends no name", "", "192.168.1.20"},
		{"browser by host name", "hub.example.com", "hub.example.com"},
		{"on the server itself", "localhost", "localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert, err := certs.GetCertificate(&tls.ClientHelloInfo{ServerName: tt.sni})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cert.Leaf.Verify(x509.VerifyOptions{DNSName: tt.host, Roots: pool}); err != nil {
				t.Fatalf("doesn't verify for %s: %v", tt.host, err)
			}
		})
	}

	a, _ := certs.GetCertificate(&tls.ClientHelloInfo{})
	b, _ := certs.GetCertificate(&tls.ClientHelloInfo{})
	if a != b {
		t.Fatal("the same names reuse the same certificate")
	}
	certs.now = func() time.Time { return time.Now().AddDate(0, 11, 15) }
	if c, _ := certs.GetCertificate(&tls.ClientHelloInfo{}); c == a {
		t.Fatal("a certificate near its end is replaced")
	}
	for i := range maxCached + 5 {
		_, _ = certs.GetCertificate(&tls.ClientHelloInfo{ServerName: "n" + strings.Repeat("x", i) + ".example.com"})
	}
	if len(certs.cache) > maxCached {
		t.Fatalf("cache grew to %d", len(certs.cache))
	}
	var ips []string
	for _, ip := range a.Leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	if !slices.Contains(ips, "192.168.1.20") || slices.Contains(a.Leaf.DNSNames, "192.168.1.20") {
		t.Fatalf("IPs go in IP SANs: ips %v, names %v", ips, a.Leaf.DNSNames)
	}
}

func TestRedirect(t *testing.T) {
	tests := []struct {
		name, publicURL, host, path, want string
	}{
		{"to the public URL", "https://hub.example.com", "10.0.0.5", "/c/local/?x=1", "https://hub.example.com/c/local/?x=1"},
		{"same host, default port", "", "hub.example.com:80", "/sign-in", "https://hub.example.com/sign-in"},
		{"an IPv6 host", "", "[::1]:80", "/", "https://[::1]/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			Redirect(tt.publicURL).ServeHTTP(rec, req)
			if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != tt.want {
				t.Fatalf("got %d %q, want %q", rec.Code, rec.Header().Get("Location"), tt.want)
			}
		})
	}
}

func TestCertsTrustedByASlowClock(t *testing.T) {
	ca, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cert, err := (&Certs{CA: ca}).GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.PEM)
	behind := time.Now().Add(-6 * time.Hour) // an agent whose clock is six hours slow
	if _, err := cert.Leaf.Verify(x509.VerifyOptions{DNSName: "localhost", Roots: pool, CurrentTime: behind}); err != nil {
		t.Fatalf("an agent with a slow clock must still trust the Hub: %v", err)
	}
}
