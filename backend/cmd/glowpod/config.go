package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAddr       = "127.0.0.1:7171"
	defaultServerAddr = ":8080"
	envPrefix         = "GLOWPOD_"
)

type config struct {
	mode          string
	addr          string
	noBrowser     bool
	dev           bool
	kubeconfig    string
	context       string
	prometheusURL string
	server        serverConfig
}

// serverConfig is for -mode=server: Glowpod installed in the cluster for
// many people, each signing in as themselves.
type serverConfig struct {
	publicURL       string
	clusterName     string
	token           bool
	sessionTTL      time.Duration
	sessionIdle     time.Duration
	insecureCookies bool

	oidcIssuer         string
	oidcClientID       string
	oidcClientSecret   string
	oidcScopes         string
	oidcUsernameClaim  string
	oidcGroupsClaim    string
	oidcUsernamePrefix string
	oidcGroupsPrefix   string
	oidcPassToken      bool
	oidcName           string

	ldapURL                string
	ldapBindDN             string
	ldapBindPassword       string
	ldapUserBase           string
	ldapUserFilter         string
	ldapUsernameAttribute  string
	ldapGroupBase          string
	ldapGroupFilter        string
	ldapGroupAttribute     string
	ldapGroupsPrefix       string
	ldapStartTLS           bool
	ldapCAFile             string
	ldapInsecureSkipVerify bool
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("glowpod", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	s := &cfg.server
	fs.StringVar(&cfg.mode, "mode", "local", "local: your kubeconfig, on this machine; server: in the cluster, people sign in")
	fs.StringVar(&cfg.addr, "addr", "", "address to listen on (local default "+defaultAddr+", server default "+defaultServerAddr+")")
	fs.BoolVar(&cfg.noBrowser, "no-browser", false, "don't open the browser on start (local mode)")
	fs.BoolVar(&cfg.dev, "dev", false, "development mode: serve the API only, the Vite dev server serves the UI")
	fs.StringVar(&cfg.kubeconfig, "kubeconfig", "", "kubeconfig file (default: $KUBECONFIG or ~/.kube/config; server mode: in-cluster config)")
	fs.StringVar(&cfg.context, "context", "", "kubeconfig context (default: the current context)")
	fs.StringVar(&cfg.prometheusURL, "prometheus-url", "", "Prometheus base URL; skips discovery in the cluster")

	fs.StringVar(&s.publicURL, "public-url", "", "server: the URL people open, e.g. https://glowpod.example.com (needed for SSO)")
	fs.StringVar(&s.clusterName, "cluster-name", "", "server: the cluster's name in the UI")
	fs.BoolVar(&s.token, "auth-token", true, "server: allow signing in with a service-account token")
	fs.DurationVar(&s.sessionTTL, "session-ttl", 12*time.Hour, "server: sign people out after this long")
	fs.DurationVar(&s.sessionIdle, "session-idle", 2*time.Hour, "server: sign people out after this long without use")
	fs.BoolVar(&s.insecureCookies, "insecure-cookies", false, "server: allow the session cookie over plain HTTP (testing only)")

	fs.StringVar(&s.oidcIssuer, "oidc-issuer", "", "server: OpenID Connect issuer URL; turns on SSO")
	fs.StringVar(&s.oidcClientID, "oidc-client-id", "", "server: OIDC client ID")
	fs.StringVar(&s.oidcClientSecret, "oidc-client-secret", "", "server: OIDC client secret (prefer $GLOWPOD_OIDC_CLIENT_SECRET)")
	fs.StringVar(&s.oidcScopes, "oidc-scopes", "openid,email,profile", "server: OIDC scopes, comma-separated (add groups if your provider needs it)")
	fs.StringVar(&s.oidcUsernameClaim, "oidc-username-claim", "email", "server: ID token claim to use as the Kubernetes username")
	fs.StringVar(&s.oidcGroupsClaim, "oidc-groups-claim", "groups", "server: ID token claim with the user's groups")
	fs.StringVar(&s.oidcUsernamePrefix, "oidc-username-prefix", "", "server: prefix for impersonated usernames, to match your RBAC bindings")
	fs.StringVar(&s.oidcGroupsPrefix, "oidc-groups-prefix", "", "server: prefix for impersonated group names")
	fs.BoolVar(&s.oidcPassToken, "oidc-pass-token", false, "server: send the ID token to the API server instead of impersonating (the API server must trust this issuer)")
	fs.StringVar(&s.oidcName, "oidc-name", "SSO", "server: the provider's name on the sign-in button, e.g. Okta")

	fs.StringVar(&s.ldapURL, "ldap-url", "", "server: LDAP server, ldaps://host:636 or ldap://host:389; turns on LDAP sign-in")
	fs.StringVar(&s.ldapBindDN, "ldap-bind-dn", "", "server: DN Glowpod binds as to search for users (empty: anonymous)")
	fs.StringVar(&s.ldapBindPassword, "ldap-bind-password", "", "server: password for -ldap-bind-dn (prefer $GLOWPOD_LDAP_BIND_PASSWORD)")
	fs.StringVar(&s.ldapUserBase, "ldap-user-base", "", "server: where to search for users, e.g. ou=people,dc=example,dc=com")
	fs.StringVar(&s.ldapUserFilter, "ldap-user-filter", "(uid={username})", "server: filter that finds one user; {username} is escaped")
	fs.StringVar(&s.ldapUsernameAttribute, "ldap-username-attribute", "uid", "server: user attribute to use as the Kubernetes username")
	fs.StringVar(&s.ldapGroupBase, "ldap-group-base", "", "server: where to search for groups (empty: no groups)")
	fs.StringVar(&s.ldapGroupFilter, "ldap-group-filter", "(member={dn})", "server: filter for a user's groups; {dn} and {username} are escaped")
	fs.StringVar(&s.ldapGroupAttribute, "ldap-group-attribute", "cn", "server: group attribute to use as the Kubernetes group name")
	fs.StringVar(&s.ldapGroupsPrefix, "ldap-groups-prefix", "", "server: prefix for LDAP group names, to match your RBAC bindings")
	fs.BoolVar(&s.ldapStartTLS, "ldap-start-tls", false, "server: upgrade ldap:// connections with StartTLS")
	fs.StringVar(&s.ldapCAFile, "ldap-ca-file", "", "server: CA bundle for the LDAP server's certificate")
	fs.BoolVar(&s.ldapInsecureSkipVerify, "ldap-insecure-skip-verify", false, "server: don't check the LDAP server's certificate (testing only)")

	if err := fs.Parse(args); err != nil {
		return config{}, err //nolint:wrapcheck // flag errors are already user-facing
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if err := fromEnv(fs); err != nil {
		return config{}, err
	}
	return cfg, cfg.check()
}

// fromEnv fills flags not given on the command line from GLOWPOD_<FLAG>,
// e.g. -oidc-client-secret from GLOWPOD_OIDC_CLIENT_SECRET. Secrets belong
// in the environment (a Kubernetes Secret), not in arguments others can read.
func fromEnv(fs *flag.FlagSet) error {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	var errs []error
	fs.VisitAll(func(f *flag.Flag) {
		if set[f.Name] {
			return
		}
		name := envPrefix + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if v, ok := os.LookupEnv(name); ok {
			if err := f.Value.Set(v); err != nil {
				errs = append(errs, fmt.Errorf("$%s: %w", name, err))
			}
		}
	})
	return errors.Join(errs...)
}

func (c *config) check() error {
	switch c.mode {
	case "local":
		if c.addr == "" {
			c.addr = defaultAddr
		}
		if err := checkLoopback(c.addr); err != nil {
			return err
		}
	case "server":
		if c.addr == "" {
			c.addr = defaultServerAddr
		}
		if c.dev {
			return errors.New("-dev is for local mode")
		}
		if err := c.server.check(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("-mode %q must be local or server", c.mode)
	}
	if c.prometheusURL != "" {
		u, err := url.Parse(c.prometheusURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("-prometheus-url %q must be an http(s) URL", c.prometheusURL)
		}
	}
	return nil
}

func (s *serverConfig) check() error {
	if s.publicURL != "" {
		u, err := url.Parse(s.publicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("-public-url %q must be an http(s) URL without a path", s.publicURL)
		}
		if u.Scheme == "http" && !s.insecureCookies {
			return errors.New("-public-url is http://; serve Glowpod over HTTPS, or pass -insecure-cookies for a test")
		}
	}
	if s.oidcIssuer != "" {
		if s.publicURL == "" || s.oidcClientID == "" {
			return errors.New("SSO needs -public-url and -oidc-client-id")
		}
	}
	if s.ldapURL != "" && s.ldapUserBase == "" {
		return errors.New("LDAP needs -ldap-user-base")
	}
	if !s.token && s.oidcIssuer == "" && s.ldapURL == "" {
		return errors.New("turn on a sign-in method: -auth-token, -oidc-issuer or -ldap-url")
	}
	if s.sessionTTL <= 0 || s.sessionIdle <= 0 {
		return errors.New("-session-ttl and -session-idle must be positive")
	}
	return nil
}

// checkLoopback makes sure local mode, which acts with the user's kubeconfig,
// is never reachable from other machines.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid -addr %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-addr %q must be a loopback address such as 127.0.0.1 in local mode", addr)
	}
	return nil
}
