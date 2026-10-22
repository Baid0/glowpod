package auth

import (
	"errors"
	"strings"
)

// systemPrefix marks Kubernetes' own users and groups, such as
// system:masters (full admin) and system:serviceaccounts.
const systemPrefix = "system:"

// errSystemUser means an SSO or LDAP user maps to a Kubernetes system user.
var errSystemUser = errors.New("refusing to impersonate a system: user")

// impersonable checks a user Glowpod is about to impersonate. Its service
// account may impersonate anyone, so a directory or identity provider that
// says "system:masters" must not become cluster admin through Glowpod:
// system users are refused and system groups are dropped. Configure a
// groups prefix (e.g. oidc:) and bind RBAC to the prefixed names instead.
func impersonable(user string, groups []string) ([]string, error) {
	if strings.HasPrefix(user, systemPrefix) {
		return nil, errSystemUser
	}
	out := groups[:0:0]
	for _, g := range groups {
		if !strings.HasPrefix(g, systemPrefix) {
			out = append(out, g)
		}
	}
	return out, nil
}
