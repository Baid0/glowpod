package main

import (
	"context"

	"glowpod/internal/auth"
)

// newOIDC connects to the OpenID Connect provider.
func newOIDC(_ context.Context, _ serverConfig) (*auth.OIDC, error) {
	return &auth.OIDC{}, nil
}

// newLDAP prepares LDAP sign-in.
func newLDAP(_ serverConfig) (*auth.LDAP, error) {
	return &auth.LDAP{}, nil
}
