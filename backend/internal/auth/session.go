package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	// saved is when lastSeen last reached the Saver.
	saved time.Time
}

// Saver keeps sessions where they outlive the process: the Hub keeps them
// in Secrets, so a restart doesn't sign everyone out. Keys are hashes of
// cookie values.
type Saver interface {
	SaveSession(ctx context.Context, key string, s SavedSession) error
	// LoadSession returns false when there's no such session.
	LoadSession(ctx context.Context, key string) (SavedSession, bool, error)
	DeleteSession(ctx context.Context, key string) error
	ListSessions(ctx context.Context) (map[string]SavedSession, error)
}

// SavedSession is what a Saver keeps: names and groups, never a
// credential. Sessions that hold a token stay in memory only.
type SavedSession struct {
	CSRF     string      `json:"csrf"`
	User     string      `json:"user"`
	Groups   []string    `json:"groups,omitempty"`
	Info     SessionInfo `json:"info"`
	Created  time.Time   `json:"created"`
	LastSeen time.Time   `json:"lastSeen"`
}

// saveEvery is how often a session in use writes its last-seen time: often
// enough for the idle limit, rarely enough not to write on every request.
const saveEvery = time.Minute

// saveTimeout bounds one Saver call.
const saveTimeout = 5 * time.Second

// Store keeps sessions in memory, and with a Saver also outside it. They
// end on sign-out, after Idle without use, after Absolute in any case, and
// without a Saver when Glowpod restarts.
type Store struct {
	Idle     time.Duration
	Absolute time.Duration
	Max      int
	// Saver, when set, keeps sessions that act by name across restarts.
	Saver Saver
	now   func() time.Time

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

// savable reports whether sess can be kept outside memory: it acts by
// name and group only, with no token to store.
func savable(sess *Session) bool {
	return sess.refresh == nil && sess.Identity.Token == "" && sess.Identity.User != ""
}

func toSaved(sess *Session) SavedSession {
	return SavedSession{
		CSRF: sess.CSRF, User: sess.Identity.User, Groups: sess.Identity.Groups,
		Info: sess.Info, Created: sess.created, LastSeen: sess.lastSeen,
	}
}

func fromSaved(v SavedSession) *Session {
	return &Session{
		CSRF: v.CSRF, Identity: cluster.UserIdentity(v.User, v.Groups), Info: v.Info,
		created: v.Created, lastSeen: v.LastSeen, saved: v.LastSeen,
	}
}

// Create starts a session and returns its cookie value.
func (s *Store) Create(ctx context.Context, sess *Session) (string, error) {
	id, err := randomSecret()
	if err != nil {
		return "", err
	}
	csrf, err := randomSecret()
	if err != nil {
		return "", err
	}
	now := s.now()
	sess.CSRF, sess.created, sess.lastSeen, sess.saved = csrf, now, now, now
	key := hashID(id)
	if s.Saver != nil && savable(sess) {
		ctx, cancel := context.WithTimeout(ctx, saveTimeout)
		defer cancel()
		if err := s.Saver.SaveSession(ctx, key, toSaved(sess)); err != nil {
			return "", fmt.Errorf("save the session: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep(now)
	s.m[key] = sess
	return id, nil
}

// Get returns a live session and marks it used.
func (s *Store) Get(ctx context.Context, id string) (*Session, bool) {
	if id == "" {
		return nil, false
	}
	now := s.now()
	key := hashID(id)
	s.mu.Lock()
	sess, ok := s.m[key]
	s.mu.Unlock()
	if !ok {
		if sess, ok = s.load(ctx, key); !ok {
			return nil, false
		}
	}
	s.mu.Lock()
	if s.expired(sess, now) {
		delete(s.m, key)
		s.mu.Unlock()
		s.forget(ctx, key)
		return nil, false
	}
	sess.lastSeen = now
	touch := s.Saver != nil && savable(sess) && now.Sub(sess.saved) >= saveEvery
	if touch {
		// Set before writing, so a failing Saver is tried once a minute,
		// not on every request.
		sess.saved = now
	}
	saved := toSaved(sess)
	s.mu.Unlock()
	if touch {
		ctx, cancel := context.WithTimeout(ctx, saveTimeout)
		defer cancel()
		_ = s.Saver.SaveSession(ctx, key, saved) // the session still works; the next minute tries again
	}
	return sess, true
}

// load reads a session from the Saver into memory, after a restart.
func (s *Store) load(ctx context.Context, key string) (*Session, bool) {
	if s.Saver == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, saveTimeout)
	defer cancel()
	v, ok, err := s.Saver.LoadSession(ctx, key)
	if err != nil || !ok || v.User == "" {
		return nil, false
	}
	sess := fromSaved(v)
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.m[key]; ok { // loaded meanwhile by another request
		return cur, true
	}
	s.m[key] = sess
	return sess, true
}

// Delete ends a session.
func (s *Store) Delete(ctx context.Context, id string) (*Session, bool) {
	key := hashID(id)
	s.mu.Lock()
	sess, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	s.forget(ctx, key)
	return sess, ok
}

// forget deletes a saved session.
func (s *Store) forget(ctx context.Context, key string) {
	if s.Saver == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, saveTimeout)
	defer cancel()
	_ = s.Saver.DeleteSession(ctx, key) // Prune removes it if this fails
}

// EndUser ends every session of user, saved or in memory.
func (s *Store) EndUser(ctx context.Context, user string) {
	s.mu.Lock()
	for k, sess := range s.m {
		if sess.Info.User == user || sess.Identity.User == user {
			delete(s.m, k)
		}
	}
	s.mu.Unlock()
	if s.Saver == nil {
		return
	}
	all, err := s.Saver.ListSessions(ctx)
	if err != nil {
		return
	}
	for key, v := range all {
		if v.User == user || v.Info.User == user {
			_ = s.Saver.DeleteSession(ctx, key) // Prune removes it if this fails
		}
	}
}

// Prune deletes saved sessions that have expired. Run it now and then;
// sessions nobody comes back to are never read again otherwise.
func (s *Store) Prune(ctx context.Context) error {
	if s.Saver == nil {
		return nil
	}
	all, err := s.Saver.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("list saved sessions: %w", err)
	}
	now := s.now()
	for key, v := range all {
		if s.expired(fromSaved(v), now) {
			if err := s.Saver.DeleteSession(ctx, key); err != nil {
				return fmt.Errorf("delete an expired session: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) expired(sess *Session, now time.Time) bool {
	return now.Sub(sess.lastSeen) > s.Idle || now.Sub(sess.created) > s.Absolute
}

// sweep drops expired sessions, and the oldest one when full. A saved
// session dropped here comes back from the Saver when used.
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
