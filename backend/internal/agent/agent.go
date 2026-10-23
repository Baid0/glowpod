// Package agent is glowpod-agent: it runs in an imported cluster, connects
// out to the Hub and answers the Hub's requests with the API server, as the
// people the Hub impersonates. Its own service account may only impersonate
// and read role bindings.
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"glowpod/internal/tunnel"
)

// Keys in the agent's Secret.
const (
	KeyHubURL     = "hub-url"
	KeyCA         = "hub-ca.crt"
	KeyToken      = "import-token"
	KeyCredential = "credential"
)

// Config is where the agent finds the Hub and its own credentials.
type Config struct {
	// Namespace and Secret hold the Hub's address, its CA and the agent's
	// import token, which becomes a credential on first contact.
	Namespace string
	Secret    string
	// Upstream is the API server, as the agent's service account.
	Upstream *rest.Config
	Kube     kubernetes.Interface
	Logger   *slog.Logger
}

// RegisterRequest is the agent's first call to the Hub.
type RegisterRequest struct {
	Token string `json:"token"`
}

// RegisterResponse carries the agent's long-lived credential.
type RegisterResponse struct {
	Cluster    string `json:"cluster"`
	Credential string `json:"credential"`
}

// ErrRefused means the Hub doesn't accept this cluster any more (it was
// removed, or the token expired before first contact).
var ErrRefused = errors.New("the Hub refused this cluster's credential")

const (
	minBackoff     = time.Second
	maxBackoff     = 30 * time.Second
	refusedBackoff = 5 * time.Minute
)

// Run connects to the Hub and keeps the tunnel open until ctx ends.
func Run(ctx context.Context, cfg Config) error {
	h, err := newHandler(cfg.Upstream, cfg.Kube, cfg.Logger)
	if err != nil {
		return err
	}
	backoff := minBackoff
	for {
		start := time.Now()
		err := connectOnce(ctx, cfg, h)
		if ctx.Err() != nil {
			return nil
		}
		wait := backoff
		switch {
		case errors.Is(err, ErrRefused):
			cfg.Logger.Error("the Hub refused this cluster; was it removed from the Hub? Import it again, or uninstall the agent", "err", err)
			wait = refusedBackoff
		case err != nil:
			cfg.Logger.Warn("tunnel to the Hub closed", "err", err)
		default:
			cfg.Logger.Info("tunnel to the Hub closed")
		}
		// A tunnel that stayed up a while starts the backoff over.
		if time.Since(start) > time.Minute {
			backoff, wait = minBackoff, minBackoff
		} else {
			backoff = min(backoff*2, maxBackoff)
		}
		jitter := time.Duration(rand.Int64N(int64(wait / 4))) //nolint:gosec // spreads reconnects; not a secret
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait + jitter):
		}
	}
}

func connectOnce(ctx context.Context, cfg Config, h http.Handler) error {
	secret, err := cfg.Kube.CoreV1().Secrets(cfg.Namespace).Get(ctx, cfg.Secret, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read the agent's Secret %s/%s: %w", cfg.Namespace, cfg.Secret, err)
	}
	hubURL := strings.TrimSuffix(string(secret.Data[KeyHubURL]), "/")
	if hubURL == "" {
		return fmt.Errorf("the agent's Secret has no %s", KeyHubURL)
	}
	client, err := httpClient(secret.Data[KeyCA])
	if err != nil {
		return err
	}
	credential := string(secret.Data[KeyCredential])
	if credential == "" {
		if credential, err = register(ctx, cfg, client, hubURL, secret); err != nil {
			return err
		}
	} else if _, ok := secret.Data[KeyToken]; ok {
		// Running the import command again (for a new CA, say) brings a
		// token this agent doesn't need: it has its credential.
		delete(secret.Data, KeyToken)
		if _, err := cfg.Kube.CoreV1().Secrets(cfg.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			cfg.Logger.Warn("couldn't remove the unused import token from the agent's Secret", "err", err)
		}
	}

	wsURL, err := url.Parse(hubURL + tunnel.ConnectPath)
	if err != nil {
		return fmt.Errorf("hub URL: %w", err)
	}
	wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
	c, resp, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{ //nolint:bodyclose // closed below on failure; on success the WebSocket owns the connection
		HTTPClient:      client,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + credential}},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		refused := resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if refused {
			return ErrRefused
		}
		return fmt.Errorf("connect to the Hub: %w", err)
	}
	cfg.Logger.Info("connected to the Hub", "hub", hubURL)
	return tunnel.Serve(ctx, c, h)
}

// register trades the one-time import token for a credential and keeps the
// credential in the Secret, without the token.
func register(ctx context.Context, cfg Config, client *http.Client, hubURL string, secret *corev1.Secret) (string, error) {
	token := string(secret.Data[KeyToken])
	if token == "" {
		return "", fmt.Errorf("the agent's Secret has neither %s nor %s", KeyCredential, KeyToken)
	}
	body, _ := json.Marshal(RegisterRequest{Token: token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+tunnel.RegisterPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("register: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("register with the Hub: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrRefused
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("register with the Hub: %s", resp.Status)
	}
	var out RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("register with the Hub: a bad answer: %w", err)
	}
	if out.Credential == "" {
		return "", errors.New("register with the Hub: no credential in the answer")
	}
	secret.Data[KeyCredential] = []byte(out.Credential)
	delete(secret.Data, KeyToken)
	if _, err := cfg.Kube.CoreV1().Secrets(cfg.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("save the credential in the agent's Secret: %w", err)
	}
	cfg.Logger.Info("registered with the Hub", "cluster", out.Cluster)
	return out.Credential, nil
}

// httpClient trusts the Hub's CA when one is given (a private or
// self-signed Hub), else the system's.
func httpClient(caPEM []byte) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(bytes.TrimSpace(caPEM)) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("the agent's Secret has a %s that isn't PEM", KeyCA)
		}
		tlsCfg.RootCAs = pool
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg
	return &http.Client{Transport: tr, Timeout: 0}, nil
}
