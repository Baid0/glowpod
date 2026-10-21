package stream

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
)

const (
	pingInterval = 25 * time.Second
	writeTimeout = 10 * time.Second
	// queueSize bounds messages waiting for a slow browser. When it fills,
	// the connection closes and the client reconnects and refetches.
	queueSize = 512
)

// Handler serves GET /api/v1/clusters/{cluster}/stream?namespace=.
// The router validates the namespace and attaches the identity first.
type Handler struct {
	Provider cluster.Provider
	Logger   *slog.Logger
	Now      func() time.Time
	Backoff  func() Backoff
}

// ServeHTTP upgrades to a WebSocket. Accept rejects cross-origin requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	clients, err := h.Provider.ClientsFor(r.Context(), id)
	if err != nil {
		http.Error(w, "no cluster client", http.StatusInternalServerError)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the response
	}
	defer conn.CloseNow() //nolint:errcheck // best effort after a normal close

	namespace := r.URL.Query().Get("namespace")
	now := h.Now
	if now == nil {
		now = time.Now
	}
	backoff := h.Backoff
	if backoff == nil {
		backoff = DefaultBackoff
	}
	sources := []Source{
		PodSource(clients.Kube, namespace, now),
		EventSource(clients.Kube, namespace),
	}

	ctx := conn.CloseRead(r.Context()) // we never read; this handles pings and close
	status, reason := serve(ctx, conn, sources, backoff)
	if err := conn.Close(status, reason); err != nil && !errors.Is(err, context.Canceled) && h.Logger != nil {
		h.Logger.Debug("websocket close", "err", err)
	}
}

func serve(ctx context.Context, c *websocket.Conn, sources []Source, backoff func() Backoff) (websocket.StatusCode, string) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	out := make(chan Message, queueSize)
	var overflow sync.Once
	slow := make(chan struct{})
	send := func(m Message) bool {
		select {
		case out <- m:
			return true
		case <-ctx.Done():
			return false
		default:
			overflow.Do(func() { close(slow) })
			return false
		}
	}

	for _, src := range sources {
		wg.Go(func() { Run(ctx, src, send, backoff()) })
	}

	if err := write(ctx, c, Message{Type: "ready"}); err != nil {
		return websocket.StatusGoingAway, ""
	}
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return websocket.StatusNormalClosure, ""
		case <-slow:
			return websocket.StatusTryAgainLater, "too far behind; reconnect"
		case m := <-out:
			if err := write(ctx, c, m); err != nil {
				return websocket.StatusGoingAway, ""
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return websocket.StatusGoingAway, ""
			}
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, m) //nolint:wrapcheck // only used to decide to hang up
}
