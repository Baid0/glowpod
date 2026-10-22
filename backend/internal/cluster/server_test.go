package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func base() *rest.Config {
	return &rest.Config{
		Host:            "https://192.0.2.10:6443",
		BearerToken:     "glowpod-service-account",
		TLSClientConfig: rest.TLSClientConfig{Insecure: true, ServerName: "kubernetes"},
	}
}

func TestConfigFor(t *testing.T) {
	t.Run("token is passed through without Glowpod's credentials", func(t *testing.T) {
		b := base()
		b.BearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		cfg := ConfigFor(b, TokenIdentity("user-token"))
		if cfg.BearerTokenFile != "" {
			t.Fatal("Glowpod's token file must not be used")
		}
		if cfg.BearerToken != "user-token" || cfg.Impersonate.UserName != "" {
			t.Fatalf("cfg = %+v", cfg)
		}
		if cfg.ServerName != "kubernetes" || cfg.Host != "https://192.0.2.10:6443" {
			t.Fatal("the cluster address and TLS settings are kept")
		}
	})
	t.Run("users are impersonated with their groups", func(t *testing.T) {
		cfg := ConfigFor(base(), UserIdentity("alice@example.com", []string{"devs", "admins"}))
		if cfg.Impersonate.UserName != "alice@example.com" || strings.Join(cfg.Impersonate.Groups, ",") != "admins,devs" {
			t.Fatalf("impersonate = %+v", cfg.Impersonate)
		}
		if cfg.BearerToken != "glowpod-service-account" {
			t.Fatal("impersonation uses Glowpod's own credentials")
		}
	})
}

func TestIdentityKeys(t *testing.T) {
	a := UserIdentity("alice", []string{"b", "a"})
	if a.Key != UserIdentity("alice", []string{"a", "b"}).Key {
		t.Fatal("group order doesn't matter")
	}
	if a.Key == UserIdentity("alice", []string{"a"}).Key || a.Key == UserIdentity("bob", []string{"a", "b"}).Key {
		t.Fatal("different users or groups get different keys")
	}
	tok := TokenIdentity("secret-token-value")
	if strings.Contains(tok.Key, "secret") || tok.Key == TokenIdentity("other").Key {
		t.Fatalf("token key = %q", tok.Key)
	}
	var logs strings.Builder
	slog.New(slog.NewTextHandler(&logs, nil)).Info("x", "id", tok)
	if strings.Contains(logs.String(), "secret-token-value") || strings.Contains(fmt.Sprint(tok), "secret-token-value") {
		t.Fatalf("token leaks into logs: %s", logs.String())
	}
}

func TestServerClients(t *testing.T) {
	s := NewServer(base(), Info{ID: DefaultID})
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := s.ClientsFor(ctx, Identity{}); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("err = %v", err)
	}
	alice := UserIdentity("alice", nil)
	c1, err := s.ClientsFor(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := s.ClientsFor(ctx, alice)
	bob, _ := s.ClientsFor(ctx, UserIdentity("bob", nil))
	if c1 != c2 || c1 == bob {
		t.Fatal("clients are reused per identity and never shared")
	}
	if c1.Config.Impersonate.UserName != "alice" || bob.Config.Impersonate.UserName != "bob" {
		t.Fatal("each identity's clients act as that identity")
	}

	now = now.Add(clientIdle + time.Minute)
	if _, err := s.ClientsFor(ctx, UserIdentity("carol", nil)); err != nil {
		t.Fatal(err)
	}
	if len(s.clients) != 1 {
		t.Fatalf("idle clients are dropped, have %d", len(s.clients))
	}
	s.Forget(UserIdentity("carol", nil))
	if len(s.clients) != 0 {
		t.Fatal("Forget drops the identity's clients")
	}
}
