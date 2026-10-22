package main

import (
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr bool
		isHelp  bool
	}{
		{name: "defaults", args: nil, want: config{addr: defaultAddr}},
		{name: "dev mode", args: []string{"-dev"}, want: config{addr: defaultAddr, dev: true}},
		{name: "no browser", args: []string{"--no-browser"}, want: config{addr: defaultAddr, noBrowser: true}},
		{name: "localhost", args: []string{"-addr", "localhost:9000"}, want: config{addr: "localhost:9000"}},
		{name: "ipv6 loopback", args: []string{"-addr", "[::1]:9000"}, want: config{addr: "[::1]:9000"}},
		{name: "random port", args: []string{"-addr", "127.0.0.1:0"}, want: config{addr: "127.0.0.1:0"}},
		{name: "all interfaces rejected", args: []string{"-addr", "0.0.0.0:7171"}, wantErr: true},
		{name: "empty host rejected", args: []string{"-addr", ":7171"}, wantErr: true},
		{name: "lan address rejected", args: []string{"-addr", "192.168.1.5:7171"}, wantErr: true},
		{name: "missing port", args: []string{"-addr", "127.0.0.1"}, wantErr: true},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: true},
		{name: "stray argument", args: []string{"serve"}, wantErr: true},
		{name: "help", args: []string{"-h"}, wantErr: true, isHelp: true},
		{
			name: "cluster flags",
			args: []string{"-kubeconfig", "/tmp/k", "-context", "lab", "-prometheus-url", "http://localhost:9090"},
			want: config{addr: defaultAddr, kubeconfig: "/tmp/k", context: "lab", prometheusURL: "http://localhost:9090"},
		},
		{name: "prometheus url needs a scheme", args: []string{"-prometheus-url", "localhost:9090"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFlags(tt.args, io.Discard)
			tt.want.mode = "local"
			tt.want.server = got.server // server defaults are covered below
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got config %+v", got)
				}
				if errors.Is(err, flag.ErrHelp) != tt.isHelp {
					t.Fatalf("help error = %v, want %v (err: %v)", !tt.isHelp, tt.isHelp, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestServerFlags(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got, err := parseFlags([]string{"-mode", "server"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if got.addr != defaultServerAddr || !got.server.token || got.server.sessionTTL != 12*time.Hour || got.server.insecureCookies {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("environment fills what flags don't", func(t *testing.T) {
		t.Setenv("GLOWPOD_OIDC_CLIENT_SECRET", "from-env")
		t.Setenv("GLOWPOD_OIDC_CLIENT_ID", "ignored")
		t.Setenv("GLOWPOD_AUTH_TOKEN", "false")
		got, err := parseFlags([]string{
			"-mode", "server", "-public-url", "https://glowpod.example.com",
			"-oidc-issuer", "https://sso.example.com", "-oidc-client-id", "glowpod",
		}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if got.server.oidcClientSecret != "from-env" || got.server.oidcClientID != "glowpod" || got.server.token {
			t.Fatalf("got %+v", got.server)
		}
	})
	t.Run("bad environment value", func(t *testing.T) {
		t.Setenv("GLOWPOD_SESSION_TTL", "forever")
		if _, err := parseFlags([]string{"-mode", "server"}, io.Discard); err == nil || !strings.Contains(err.Error(), "GLOWPOD_SESSION_TTL") {
			t.Fatalf("err = %v", err)
		}
	})

	bad := []struct {
		name string
		args []string
	}{
		{"unknown mode", []string{"-mode", "cloud"}},
		{"dev in server mode", []string{"-mode", "server", "-dev"}},
		{"no sign-in method", []string{"-mode", "server", "-auth-token=false"}},
		{"sso without public url", []string{"-mode", "server", "-oidc-issuer", "https://sso.example.com", "-oidc-client-id", "x"}},
		{"sso without client id", []string{"-mode", "server", "-public-url", "https://g.example.com", "-oidc-issuer", "https://sso.example.com"}},
		{"plain http public url", []string{"-mode", "server", "-public-url", "http://g.example.com"}},
		{"public url with a path", []string{"-mode", "server", "-public-url", "https://example.com/glowpod"}},
		{"ldap without user base", []string{"-mode", "server", "-ldap-url", "ldaps://ldap.example.com"}},
		{"zero session", []string{"-mode", "server", "-session-ttl", "0s"}},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseFlags(tt.args, io.Discard); err == nil {
				t.Fatal("want an error")
			}
		})
	}
	if _, err := parseFlags([]string{"-mode", "server", "-addr", "0.0.0.0:8080"}, io.Discard); err != nil {
		t.Fatalf("server mode listens on any address: %v", err)
	}
	if _, err := parseFlags([]string{"-mode", "server", "-public-url", "http://localhost:8080", "-insecure-cookies"}, io.Discard); err != nil {
		t.Fatalf("plain http is fine for a test with -insecure-cookies: %v", err)
	}
}
