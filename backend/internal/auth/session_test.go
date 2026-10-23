package auth

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"glowpod/internal/cluster"
)

// memSaver is a Saver that outlives the stores using it, like the Hub's
// Secrets outlive a restart.
type memSaver struct {
	mu    sync.Mutex
	m     map[string]SavedSession
	saves int
	fail  error
}

func newMemSaver() *memSaver { return &memSaver{m: map[string]SavedSession{}} }

func (f *memSaver) SaveSession(_ context.Context, key string, s SavedSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.saves++
	f.m[key] = s
	return nil
}

func (f *memSaver) LoadSession(_ context.Context, key string) (SavedSession, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.m[key]
	return s, ok, nil
}

func (f *memSaver) DeleteSession(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, key)
	return nil
}

func (f *memSaver) ListSessions(context.Context) (map[string]SavedSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]SavedSession, len(f.m))
	for k, v := range f.m {
		out[k] = v
	}
	return out, nil
}

func (f *memSaver) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.m)
}

// testClock is a store's clock, shared by the stores of one test so a
// "restart" keeps the time.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

func newSavedStore(saver Saver, clock *testClock) *Store {
	s := NewStore(time.Hour, 12*time.Hour)
	s.Saver, s.now = saver, clock.now
	return s
}

func TestStoreSavesOnlySessionsThatActByName(t *testing.T) {
	tests := []struct {
		name string
		sess *Session
		want bool
	}{
		{"password or LDAP", &Session{Identity: cluster.UserIdentity("glowpod:admin", nil)}, true},
		{"SSO by name", &Session{Identity: cluster.UserIdentity("oidc:ann", []string{"oidc:shop"})}, true},
		{"service-account token", &Session{Identity: cluster.TokenIdentity("secret-token")}, false},
		{"SSO passing its token", &Session{
			Identity: cluster.TokenIdentity("id-token"),
			refresh:  func() (cluster.Identity, error) { return cluster.Identity{}, nil },
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saver := newMemSaver()
			s := newSavedStore(saver, &testClock{t: time.Now()})
			if _, err := s.Create(t.Context(), tt.sess); err != nil {
				t.Fatal(err)
			}
			if got := saver.len() == 1; got != tt.want {
				t.Fatalf("saved = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStoreKeepsSessionsAcrossRestarts(t *testing.T) {
	saver := newMemSaver()
	clock := &testClock{t: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}
	before := newSavedStore(saver, clock)
	info := SessionInfo{Mode: "in-cluster", Method: "oidc", User: "oidc:ann", Groups: []string{"oidc:shop"}}
	id, err := before.Create(t.Context(), &Session{Identity: cluster.UserIdentity("oidc:ann", []string{"oidc:shop"}), Info: info})
	if err != nil {
		t.Fatal(err)
	}
	created, _ := before.Get(t.Context(), id)
	for key := range saver.m {
		if key == id {
			t.Fatal("the Saver gets a hash, not the cookie")
		}
	}

	clock.t = clock.t.Add(10 * time.Minute)
	after := newSavedStore(saver, clock) // the Hub restarted
	sess, ok := after.Get(t.Context(), id)
	if !ok {
		t.Fatal("the session didn't survive the restart")
	}
	if sess.Identity.Key != created.Identity.Key || sess.CSRF != created.CSRF || !slices.Equal(sess.Info.Groups, info.Groups) {
		t.Fatalf("got %+v, want the same identity, CSRF token and info as before", sess)
	}

	if _, ok := after.Delete(t.Context(), id); !ok {
		t.Fatal("delete")
	}
	if _, ok := newSavedStore(saver, clock).Get(t.Context(), id); ok {
		t.Fatal("signing out ends the saved session too")
	}
}

func TestStoreSavesLastSeenOnceAMinute(t *testing.T) {
	saver := newMemSaver()
	clock := &testClock{t: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}
	s := newSavedStore(saver, clock)
	id, err := s.Create(t.Context(), &Session{Identity: cluster.UserIdentity("glowpod:admin", nil)})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		clock.t = clock.t.Add(10 * time.Second)
		s.Get(t.Context(), id)
	}
	if saver.saves != 1 {
		t.Fatalf("saves = %d after 50 s of requests, want only the first", saver.saves)
	}
	clock.t = clock.t.Add(15 * time.Second)
	s.Get(t.Context(), id)
	if saver.saves != 2 {
		t.Fatalf("saves = %d after a minute, want 2", saver.saves)
	}

	// Idle for 50 minutes after a restart: the saved last-seen time counts.
	clock.t = clock.t.Add(50 * time.Minute)
	if _, ok := newSavedStore(saver, clock).Get(t.Context(), id); !ok {
		t.Fatal("used 50 minutes ago: still signed in")
	}
	clock.t = clock.t.Add(61 * time.Minute)
	if _, ok := newSavedStore(saver, clock).Get(t.Context(), id); ok {
		t.Fatal("idle for over an hour: signed out")
	}
	if saver.len() != 0 {
		t.Fatal("an expired session is deleted when it's found")
	}
}

func TestStorePrune(t *testing.T) {
	saver := newMemSaver()
	clock := &testClock{t: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}
	s := newSavedStore(saver, clock)
	if _, err := s.Create(t.Context(), &Session{Identity: cluster.UserIdentity("glowpod:old", nil)}); err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(2 * time.Hour)
	if _, err := s.Create(t.Context(), &Session{Identity: cluster.UserIdentity("glowpod:new", nil)}); err != nil {
		t.Fatal(err)
	}
	saver.m["broken"] = SavedSession{}
	if err := s.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	all, _ := saver.ListSessions(t.Context())
	if len(all) != 1 {
		t.Fatalf("left %d sessions, want only the live one", len(all))
	}
	for _, v := range all {
		if v.User != "glowpod:new" {
			t.Fatalf("kept %s", v.User)
		}
	}
}

func TestStoreCreateFailsWhenItCantSave(t *testing.T) {
	saver := newMemSaver()
	saver.fail = errors.New("API server down")
	s := newSavedStore(saver, &testClock{t: time.Now()})
	if _, err := s.Create(t.Context(), &Session{Identity: cluster.UserIdentity("glowpod:admin", nil)}); err == nil {
		t.Fatal("a sign-in that would be lost on restart should fail, not half-work")
	}
}

func TestStoreEndUser(t *testing.T) {
	saver := newMemSaver()
	clock := &testClock{t: time.Now()}
	s := newSavedStore(saver, clock)
	bob := &Session{Identity: cluster.UserIdentity("glowpod:bob", nil), Info: SessionInfo{User: "glowpod:bob"}}
	ann := &Session{Identity: cluster.UserIdentity("glowpod:ann", nil), Info: SessionInfo{User: "glowpod:ann"}}
	bobID, _ := s.Create(t.Context(), bob)
	annID, _ := s.Create(t.Context(), ann)
	s.EndUser(t.Context(), "glowpod:bob")
	if _, ok := s.Get(t.Context(), bobID); ok {
		t.Fatal("bob is signed out")
	}
	if _, ok := newSavedStore(saver, clock).Get(t.Context(), bobID); ok {
		t.Fatal("bob's saved session is gone too")
	}
	if _, ok := s.Get(t.Context(), annID); !ok {
		t.Fatal("ann stays signed in")
	}
}
