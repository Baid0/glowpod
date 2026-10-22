package main

import (
	"context"
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

// newLDAP prepares LDAP sign-in.
func newLDAP(_ serverConfig) (*auth.LDAP, error) {
	return &auth.LDAP{}, nil
}
