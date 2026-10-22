package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"glowpod/internal/auth"
)

// newOIDC prepares SSO; the provider is contacted on the first sign-in.
func newOIDC(_ context.Context, s serverConfig) (*auth.OIDC, error) {
	var scopes []string
	for _, sc := range strings.Split(s.oidcScopes, ",") {
		if sc = strings.TrimSpace(sc); sc != "" {
			scopes = append(scopes, sc)
		}
	}
	return auth.NewOIDC(auth.OIDCConfig{
		Issuer:         s.oidcIssuer,
		ClientID:       s.oidcClientID,
		ClientSecret:   s.oidcClientSecret,
		RedirectURL:    strings.TrimSuffix(s.publicURL, "/") + auth.OIDCCallbackPath,
		Scopes:         scopes,
		UsernameClaim:  s.oidcUsernameClaim,
		GroupsClaim:    s.oidcGroupsClaim,
		UsernamePrefix: s.oidcUsernamePrefix,
		GroupsPrefix:   s.oidcGroupsPrefix,
		PassToken:      s.oidcPassToken,
	})
}

// newLDAP prepares LDAP sign-in; it connects on each sign-in.
func newLDAP(s serverConfig) (*auth.LDAP, error) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: s.ldapInsecureSkipVerify, //nolint:gosec // an explicit, documented test-only switch
	}
	if s.ldapCAFile != "" {
		pem, err := os.ReadFile(s.ldapCAFile)
		if err != nil {
			return nil, fmt.Errorf("read -ldap-ca-file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("-ldap-ca-file %s has no PEM certificates", s.ldapCAFile)
		}
		tlsCfg.RootCAs = pool
	}
	return auth.NewLDAP(auth.LDAPConfig{
		URL:               s.ldapURL,
		BindDN:            s.ldapBindDN,
		BindPassword:      s.ldapBindPassword,
		UserBase:          s.ldapUserBase,
		UserFilter:        s.ldapUserFilter,
		UsernameAttribute: s.ldapUsernameAttribute,
		GroupBase:         s.ldapGroupBase,
		GroupFilter:       s.ldapGroupFilter,
		GroupAttribute:    s.ldapGroupAttribute,
		GroupsPrefix:      s.ldapGroupsPrefix,
		StartTLS:          s.ldapStartTLS,
		TLS:               tlsCfg,
	})
}
