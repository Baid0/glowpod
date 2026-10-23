package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/auth"
)

const (
	// sessionSecretPrefix names a signed-in session's Secret. The rest of
	// the name is a hash of the cookie, never the cookie itself.
	sessionSecretPrefix = "glowpod-session-" //nolint:gosec // a Secret's name, not a credential
	keySession          = "session"
	kindSession         = "session"
)

// Sessions keeps sign-ins in Secrets, so restarting the Hub doesn't sign
// everyone out.
func (h *Hub) Sessions() auth.Saver { return sessionSaver{h.store} }

type sessionSaver struct{ s *store }

func (v sessionSaver) SaveSession(ctx context.Context, key string, sess auth.SavedSession) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("encode the session: %w", err)
	}
	return v.s.putSecret(ctx, sessionSecretPrefix+key, kindSession, map[string][]byte{keySession: raw})
}

func (v sessionSaver) LoadSession(ctx context.Context, key string) (auth.SavedSession, bool, error) {
	data, err := v.s.secret(ctx, sessionSecretPrefix+key)
	if err != nil || data == nil {
		return auth.SavedSession{}, false, err
	}
	var sess auth.SavedSession
	if err := json.Unmarshal(data[keySession], &sess); err != nil {
		return auth.SavedSession{}, false, nil //nolint:nilerr // a broken record is no session; Prune deletes it
	}
	return sess, true, nil
}

func (v sessionSaver) DeleteSession(ctx context.Context, key string) error {
	return v.s.deleteSecret(ctx, sessionSecretPrefix+key)
}

func (v sessionSaver) ListSessions(ctx context.Context) (map[string]auth.SavedSession, error) {
	l, err := v.s.kube.CoreV1().Secrets(v.s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "hub.glowpod.dev/kind=" + kindSession,
	})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	out := make(map[string]auth.SavedSession, len(l.Items))
	for i := range l.Items {
		key, ok := strings.CutPrefix(l.Items[i].Name, sessionSecretPrefix)
		if !ok {
			continue
		}
		var sess auth.SavedSession
		// An unreadable record has a zero last-seen time, so Prune deletes it.
		_ = json.Unmarshal(l.Items[i].Data[keySession], &sess)
		out[key] = sess
	}
	return out, nil
}
