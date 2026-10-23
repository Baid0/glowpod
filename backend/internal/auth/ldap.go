package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-ldap/ldap/v3"

	"glowpod/internal/cluster"
)

// ErrBadCredentials means the username or password is wrong. Which one
// isn't said, so usernames can't be probed.
var ErrBadCredentials = errors.New("wrong username or password")

const (
	maxUsername   = 256
	maxPassword   = 1024
	ldapTimeout   = 10 * time.Second
	maxLDAPGroups = 500
)

// LDAPConfig describes the directory and how its users map to Kubernetes.
type LDAPConfig struct {
	URL string
	// BindDN and BindPassword are Glowpod's own search account; empty means
	// anonymous search.
	BindDN       string
	BindPassword string
	UserBase     string
	// UserFilter finds one user; {username} is replaced, escaped.
	UserFilter        string
	UsernameAttribute string
	// UsernamePrefix is put before the username, e.g. ldap: (a Hub needs one).
	UsernamePrefix string
	// GroupBase empty means no groups are looked up.
	GroupBase string
	// GroupFilter finds the user's groups; {dn} and {username} are replaced, escaped.
	GroupFilter    string
	GroupAttribute string
	GroupsPrefix   string
	StartTLS       bool
	TLS            *tls.Config
}

// ldapConn is the part of *ldap.Conn Glowpod uses.
type ldapConn interface {
	Bind(username, password string) error
	Search(req *ldap.SearchRequest) (*ldap.SearchResult, error)
	StartTLS(cfg *tls.Config) error
	Close() error
}

// LDAP signs people in against a directory, then Glowpod impersonates them.
type LDAP struct {
	cfg  LDAPConfig
	dial func(ctx context.Context) (ldapConn, error)
}

// NewLDAP checks the config; it connects on each sign-in.
func NewLDAP(cfg LDAPConfig) (*LDAP, error) {
	if cfg.URL == "" || cfg.UserBase == "" {
		return nil, errors.New("LDAP needs a URL and a user base")
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = "(uid={username})"
	}
	if !strings.Contains(cfg.UserFilter, "{username}") {
		return nil, errors.New("the LDAP user filter must contain {username}")
	}
	if cfg.UsernameAttribute == "" {
		cfg.UsernameAttribute = "uid"
	}
	if cfg.GroupFilter == "" {
		cfg.GroupFilter = "(member={dn})"
	}
	if cfg.GroupAttribute == "" {
		cfg.GroupAttribute = "cn"
	}
	if cfg.TLS == nil {
		cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	l := &LDAP{cfg: cfg}
	l.dial = func(context.Context) (ldapConn, error) {
		d := ldap.DialWithDialer(&net.Dialer{Timeout: ldapTimeout})
		conn, err := ldap.DialURL(cfg.URL, d, ldap.DialWithTLSConfig(cfg.TLS))
		if err != nil {
			return nil, fmt.Errorf("connect to LDAP: %w", err)
		}
		conn.SetTimeout(ldapTimeout)
		return conn, nil
	}
	return l, nil
}

// Authenticate checks a username and password and returns the Kubernetes
// username and groups.
func (l *LDAP) Authenticate(ctx context.Context, username, password string) (string, []string, error) {
	// An empty password is an "unauthenticated bind" that many servers
	// accept for anyone. Never let it through.
	if password == "" || username == "" {
		return "", nil, ErrBadCredentials
	}
	conn, err := l.dial(ctx)
	if err != nil {
		return "", nil, err
	}
	defer conn.Close() //nolint:errcheck // nothing to do if closing fails
	if l.cfg.StartTLS {
		if err := conn.StartTLS(l.cfg.TLS); err != nil {
			return "", nil, fmt.Errorf("LDAP StartTLS: %w", err)
		}
	}
	if err := l.bindService(conn); err != nil {
		return "", nil, err
	}

	res, err := conn.Search(ldap.NewSearchRequest(
		l.cfg.UserBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, int(ldapTimeout.Seconds()), false,
		strings.ReplaceAll(l.cfg.UserFilter, "{username}", ldap.EscapeFilter(username)),
		[]string{l.cfg.UsernameAttribute}, nil,
	))
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return "", nil, ErrBadCredentials // the filter matched more than one user
		}
		return "", nil, fmt.Errorf("search LDAP users: %w", err)
	}
	if len(res.Entries) != 1 {
		return "", nil, ErrBadCredentials
	}
	user := res.Entries[0]

	if err := conn.Bind(user.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return "", nil, ErrBadCredentials
		}
		return "", nil, fmt.Errorf("LDAP bind as user: %w", err)
	}
	name := user.GetAttributeValue(l.cfg.UsernameAttribute)
	if name == "" {
		name = username
	}
	name = l.cfg.UsernamePrefix + name

	groups, err := l.groups(conn, user.DN, username)
	if err != nil {
		return "", nil, err
	}
	if groups, err = impersonable(name, groups); err != nil {
		return "", nil, ErrBadCredentials
	}
	return name, groups, nil
}

func (l *LDAP) bindService(conn ldapConn) error {
	if l.cfg.BindDN == "" {
		return nil
	}
	if err := conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
		return fmt.Errorf("LDAP bind as %s: %w", l.cfg.BindDN, err)
	}
	return nil
}

func (l *LDAP) groups(conn ldapConn, dn, username string) ([]string, error) {
	if l.cfg.GroupBase == "" {
		return nil, nil
	}
	// Search as Glowpod again: users often may not read group entries.
	if err := l.bindService(conn); err != nil {
		return nil, err
	}
	filter := strings.NewReplacer(
		"{dn}", ldap.EscapeFilter(dn),
		"{username}", ldap.EscapeFilter(username),
	).Replace(l.cfg.GroupFilter)
	res, err := conn.Search(ldap.NewSearchRequest(
		l.cfg.GroupBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, maxLDAPGroups, int(ldapTimeout.Seconds()), false,
		filter, []string{l.cfg.GroupAttribute}, nil,
	))
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return nil, fmt.Errorf("search LDAP groups: %w", err)
	}
	var out []string
	if res != nil {
		for _, e := range res.Entries {
			if g := e.GetAttributeValue(l.cfg.GroupAttribute); g != "" {
				out = append(out, l.cfg.GroupsPrefix+g)
			}
		}
	}
	return out, nil
}

type ldapLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func validUsername(u string) bool {
	if u == "" || len(u) > maxUsername {
		return false
	}
	for _, r := range u {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (s *Server) loginLDAP(w http.ResponseWriter, r *http.Request) {
	var req ldapLogin
	if !s.loginRequest(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if !validUsername(username) || req.Password == "" || len(req.Password) > maxPassword {
		writeJSONError(w, http.StatusBadRequest, "bad_credentials", "Enter your username and password.")
		return
	}
	if !s.idLimit.allow("ldap:" + strings.ToLower(username)) {
		writeJSONError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), loginTimeout)
	defer cancel()
	user, groups, err := s.cfg.LDAP.Authenticate(ctx, username, req.Password)
	if errors.Is(err, ErrBadCredentials) {
		writeJSONError(w, http.StatusUnauthorized, "bad_credentials", "That username and password didn't work.")
		return
	}
	if err != nil {
		s.cfg.Logger.Warn("LDAP sign-in failed", "err", err)
		writeJSONError(w, http.StatusBadGateway, "directory_unavailable", "Couldn't reach the directory. Try again in a moment.")
		return
	}
	sess := &Session{
		Identity: cluster.UserIdentity(user, groups),
		Info:     SessionInfo{Mode: "in-cluster", Method: "ldap", User: user, Groups: groups},
	}
	if err := s.begin(w, r, sess); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "session_failed", "Couldn't start a session.")
		return
	}
	writeOK(w, sess.Info)
}
