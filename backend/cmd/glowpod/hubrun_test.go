package main

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glowpod/internal/quickstart"
)

func TestRunHubCommands(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
		wantOut string
	}{
		{args: nil, wantOut: "glowpod hub run"},
		{args: []string{"-h"}, wantOut: "bootstrap-password"},
		{args: []string{"nope"}, wantErr: `unknown command "nope"`},
		{args: []string{"run", "agent"}, wantErr: `unexpected argument "agent"`},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), append([]string{"hub"}, tt.args...), &stdout, &stderr)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(stderr.String(), tt.wantOut) {
				t.Fatalf("err %v, output %q", err, stderr.String())
			}
		})
	}
}

func TestHubTLS(t *testing.T) {
	paths := quickstart.Paths{Data: t.TempDir()}

	t.Run("its own CA", func(t *testing.T) {
		cfg, caFile, certs, err := hubTLS(paths, filepath.Join(t.TempDir(), "none"))
		if err != nil || certs == nil || cfg.GetCertificate == nil {
			t.Fatalf("want minted certificates: %v", err)
		}
		if caFile != quickstart.CAPath(paths.TLS()) {
			t.Fatalf("agents should trust %s, got %q", quickstart.CAPath(paths.TLS()), caFile)
		}
	})

	t.Run("your own certificate", func(t *testing.T) {
		// Use a minted certificate as "yours".
		ca, err := quickstart.LoadOrCreateCA(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cert, err := (&quickstart.Certs{CA: ca}).GetCertificate(&tls.ClientHelloInfo{ServerName: "hub.example.com"})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writePEM(t, filepath.Join(dir, "tls.crt"), "CERTIFICATE", cert.Certificate[0])
		writeKey(t, filepath.Join(dir, "tls.key"), cert.PrivateKey)

		cfg, caFile, certs, err := hubTLS(paths, dir)
		if err != nil || certs != nil || len(cfg.Certificates) != 1 {
			t.Fatalf("want your certificate served as is: %v", err)
		}
		if caFile != "" {
			t.Fatalf("no ca.crt: a publicly trusted certificate, got %q", caFile)
		}
		if err := os.WriteFile(filepath.Join(dir, "ca.crt"), ca.PEM, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, caFile, _, _ = hubTLS(paths, dir); caFile != filepath.Join(dir, "ca.crt") {
			t.Fatalf("with ca.crt, agents trust it; got %q", caFile)
		}
	})
}

func writePEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeKey(t *testing.T, path string, key crypto.PrivateKey) {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, path, "PRIVATE KEY", der)
}
