package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"glowpod/internal/cluster"
)

// Session is one signed-in browser. It lives on the server only; the
// browser holds a random ID in an HttpOnly cookie.
type Session struct {
	CSRF     string
	Identity cluster.Identity
	Info     SessionInfo
	// refresh, when set, returns a fresh identity (OIDC tokens expire).
	refresh  func() (cluster.Identity, error)
	created  time.Time
	lastSeen time.Time
}

// Store keeps sessions in memory. They end on sign-out, after Idle without
// use, after Absolute in any case, and when Glowpod restarts.
type Store struct {
	Idle     time.Duration
	Absolute time.Duration
	Max      int
	now      func() time.Time

	mu sync.Mutex
	// Keyed by a hash of the cookie value, so a memory dump holds no
	// usable session IDs.
	m map[string]*Session
}

// NewStore returns an empty store.
func NewStore(idle, absolute time.Duration) *Store {
	return &Store{Idle: idle, Absolute: absolute, Max: 10000, now: time.Now, m: map[string]*Session{}}
}

func hashID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// Create starts a session and returns its cookie value.
func (s *Store) Create(sess *Session) (string, error) {
	id, err := randomSecret()
	if err != nil {
		return "", err
	}
	csrf, err := randomSecret()
	if err != nil {
		return "", err
	}
	now := s.now()
	sess.CSRF, sess.created, sess.lastSeen = csrf, now, now
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep(now)
	s.m[hashID(id)] = sess
	return id, nil
}

// Get returns a live session and marks it used.
func (s *Store) Get(id string) (*Session, bool) {
	if id == "" {
		return nil, false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	key := hashID(id)
	sess, ok := s.m[key]
	if !ok {
		return nil, false
	}
	if s.expired(sess, now) {
		delete(s.m, key)
		return nil, false
	}
	sess.lastSeen = now
	return sess, true
}

// Delete ends a session.
func (s *Store) Delete(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := hashID(id)
	sess, ok := s.m[key]
	delete(s.m, key)
	return sess, ok
}

func (s *Store) expired(sess *Session, now time.Time) bool {
	return now.Sub(sess.lastSeen) > s.Idle || now.Sub(sess.created) > s.Absolute
}

// sweep drops expired sessions, and the oldest one when full.
func (s *Store) sweep(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for k, sess := range s.m {
		if s.expired(sess, now) {
			delete(s.m, k)
			continue
		}
		if oldestKey == "" || sess.lastSeen.Before(oldest) {
			oldestKey, oldest = k, sess.lastSeen
		}
	}
	if len(s.m) >= s.Max && oldestKey != "" {
		delete(s.m, oldestKey)
	}
}
