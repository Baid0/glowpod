// Package tunnel carries HTTP between a Glowpod Hub and the agents in
// imported clusters. The agent opens one WebSocket to the Hub (outbound
// only, so the cluster needs no inbound access) and the two sides run a
// stream multiplexer (yamux) over it. The Hub opens a stream per request and
// speaks plain HTTP on it; the agent answers each one with its proxy to the
// cluster's API server.
package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// Paths on the Hub, outside /api so they can't collide with the UI's API.
const (
	// ConnectPath is the agent's WebSocket.
	ConnectPath = "/v3/connect"
	// RegisterPath trades an import token for the agent's credential.
	RegisterPath = "/v3/register"
	// ImportPrefix serves the import manifest: /v3/import/<token>.yaml.
	ImportPrefix = "/v3/import/"
)

// StatusPath is answered by the agent itself, not the API server.
const StatusPath = "/glowpod/status"

// ErrNotConnected means the cluster's agent has no tunnel open.
var ErrNotConnected = errors.New("the cluster's agent isn't connected to the Hub")

// keepAlive notices a dead tunnel (a VM that went away) within a minute.
const keepAlive = 20 * time.Second

func muxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = keepAlive
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.LogOutput = io.Discard
	return cfg
}

// netConn adapts a WebSocket to a byte stream. yamux frames can be larger
// than the library's default message limit, so there is none: the peer is
// authenticated before this point.
func netConn(ctx context.Context, c *websocket.Conn) net.Conn {
	c.SetReadLimit(-1)
	return websocket.NetConn(ctx, c, websocket.MessageBinary)
}

// Sessions are the open tunnels on the Hub, one per cluster.
type Sessions struct {
	// OnChange, when set, hears about every connect and disconnect.
	OnChange func(clusterID string, connected bool)

	mu sync.Mutex
	m  map[string]*session
}

type session struct {
	mux   *yamux.Session
	since time.Time
}

// NewSessions returns an empty set.
func NewSessions() *Sessions { return &Sessions{m: map[string]*session{}} }

// Accept upgrades an authenticated agent's request and serves its tunnel
// until it closes. A new tunnel for the same cluster replaces the old one.
func (s *Sessions) Accept(w http.ResponseWriter, r *http.Request, clusterID string) error {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return err //nolint:wrapcheck // Accept already answered the request
	}
	mux, err := yamux.Client(netConn(r.Context(), c), muxConfig())
	if err != nil {
		_ = c.Close(websocket.StatusInternalError, "")
		return err //nolint:wrapcheck // yamux only fails on a bad config
	}
	sess := &session{mux: mux, since: time.Now()}
	s.mu.Lock()
	old := s.m[clusterID]
	s.m[clusterID] = sess
	s.mu.Unlock()
	if old != nil {
		_ = old.mux.Close()
	}
	s.changed(clusterID, true)

	select {
	case <-mux.CloseChan():
	case <-r.Context().Done():
		_ = mux.Close()
	}

	s.mu.Lock()
	current := s.m[clusterID] == sess
	if current {
		delete(s.m, clusterID)
	}
	s.mu.Unlock()
	if current {
		s.changed(clusterID, false)
	}
	return nil
}

func (s *Sessions) changed(clusterID string, connected bool) {
	if s.OnChange != nil {
		s.OnChange(clusterID, connected)
	}
}

// Dial opens a stream to the cluster's agent.
func (s *Sessions) Dial(_ context.Context, clusterID string) (net.Conn, error) {
	s.mu.Lock()
	sess := s.m[clusterID]
	s.mu.Unlock()
	if sess == nil {
		return nil, &net.OpError{Op: "dial", Net: "tunnel", Err: ErrNotConnected}
	}
	conn, err := sess.mux.Open()
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: "tunnel", Err: err}
	}
	return conn, nil
}

// DialFunc dials one cluster, for rest.Config.Dial and http.Transport.
func (s *Sessions) DialFunc(clusterID string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) { return s.Dial(ctx, clusterID) }
}

// Connected says whether the cluster's tunnel is open, and since when.
func (s *Sessions) Connected(clusterID string) (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.m[clusterID]; sess != nil {
		return true, sess.since
	}
	return false, time.Time{}
}

// Close ends a cluster's tunnel, e.g. when it's removed from the Hub.
func (s *Sessions) Close(clusterID string) {
	s.mu.Lock()
	sess := s.m[clusterID]
	s.mu.Unlock()
	if sess != nil {
		_ = sess.mux.Close()
	}
}

// Serve runs the agent's side of a tunnel on c: every stream the Hub opens
// is an HTTP connection to h. It returns when the tunnel closes.
func Serve(ctx context.Context, c *websocket.Conn, h http.Handler) error {
	mux, err := yamux.Server(netConn(ctx, c), muxConfig())
	if err != nil {
		return err //nolint:wrapcheck // yamux only fails on a bad config
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		select {
		case <-ctx.Done():
		case <-mux.CloseChan():
		}
		_ = srv.Close()
		_ = mux.Close()
	}()
	if err := srv.Serve(mux); err != nil && !errors.Is(err, http.ErrServerClosed) && !mux.IsClosed() {
		return err //nolint:wrapcheck // the caller reconnects either way
	}
	return nil
}
