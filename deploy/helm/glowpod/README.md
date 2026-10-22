# Glowpod Helm chart

Installs Glowpod in your cluster. Everyone signs in and **acts as
themselves**: what they can see and change is exactly what their Kubernetes
RBAC allows. Glowpod's own service account can only *impersonate* (for SSO
and LDAP users); it never reads or changes anything on its own behalf.

```bash
helm install glowpod oci://ghcr.io/baid0/charts/glowpod -n glowpod --create-namespace \
  --set publicURL=https://glowpod.example.com \
  --set ingress.enabled=true --set ingress.host=glowpod.example.com
```

Or add the classic chart repository and install `glowpod/glowpod`:

```bash
helm repo add glowpod https://glowpod.dev/charts
```

Serve it over HTTPS: the session cookie is `Secure`, so browsers only send it
over HTTPS (and to `localhost`, which is enough for a port-forward test).

Full guides: [install](https://glowpod.dev/docs/install/),
[sign-in and access](https://glowpod.dev/docs/authentication/) (more provider
examples, Active Directory, troubleshooting),
[configuration](https://glowpod.dev/docs/configuration/) and
[operations](https://glowpod.dev/docs/operations/). To try SSO and LDAP without a
provider of your own, run the [SSO demo](https://glowpod.dev/docs/sso-demo/).

## Sign-in methods

Turn on any mix. The sign-in page shows the ones you enable.

### Service-account token (on by default)

People paste a bearer token and get exactly its rights. Handy for a first try
and for break-glass access:

```bash
kubectl create serviceaccount alice -n team-a
kubectl create rolebinding alice-edit --clusterrole=edit --serviceaccount=team-a:alice -n team-a
kubectl create token alice -n team-a --duration=8h
```

The token stays on the Glowpod server for that session only; the browser
gets a random session ID. Turn it off with `auth.token.enabled=false`.

### SSO with OpenID Connect

Works with Okta, Google, Microsoft Entra ID, Keycloak, Dex, Authentik and
any other OIDC provider. Register a web application with the redirect URI
`<publicURL>/auth/oidc/callback`, then:

```yaml
publicURL: https://glowpod.example.com
auth:
  oidc:
    enabled: true
    name: Okta
    issuer: https://example.okta.com
    clientID: 0oa1example
    existingSecret: glowpod-oidc   # key: client-secret
    scopes: [openid, email, profile, groups]
```

By default Glowpod **impersonates** the user: username from the `email`
claim, groups from `groups`, with the `oidc:` prefix on groups. Bind RBAC to
the prefixed names:

```bash
kubectl create rolebinding platform-admin --clusterrole=admin --group=oidc:platform -n payments
```

If your API server already trusts the same issuer and client
(`--oidc-issuer-url`), set `auth.oidc.passToken: true` to send the ID token
itself instead; Glowpod refreshes it as it expires and needs no RBAC of its
own.

Provider notes:

| Provider | issuer | Groups |
| --- | --- | --- |
| Okta | `https://<org>.okta.com` (or an authorization server URL) | Add a `groups` claim to the ID token |
| Google | `https://accounts.google.com` | Google doesn't send groups; bind RBAC to users |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant>/v2.0` | Emit `groups` (object IDs) in the token configuration |
| Keycloak | `https://<host>/realms/<realm>` | Add a *Group Membership* mapper named `groups` |
| Dex | your Dex URL | Request the `groups` scope |

### LDAP and Active Directory

```yaml
auth:
  ldap:
    enabled: true
    url: ldaps://ldap.example.com:636
    bindDN: cn=glowpod,ou=services,dc=example,dc=com
    existingSecret: glowpod-ldap        # key: bind-password
    userBase: ou=people,dc=example,dc=com
    userFilter: (uid={username})        # AD: (sAMAccountName={username})
    usernameAttribute: uid              # AD: sAMAccountName
    groupBase: ou=groups,dc=example,dc=com
    groupFilter: (member={dn})
    groupAttribute: cn
    caSecret: ldap-ca                   # a Secret with ca.crt, if your CA isn't public
```

Groups arrive as `ldap:<cn>`. Glowpod checks the password by binding as the
user, refuses empty passwords, escapes usernames in filters, and gives the
same answer for an unknown user and a wrong password.

## Security notes

- Glowpod refuses to impersonate `system:` users and drops `system:` groups,
  so an identity provider can't make someone cluster admin through Glowpod.
  Keep the group prefixes.
- Sessions are in memory: keep `replicaCount: 1`. Restarting Glowpod signs
  everyone out.
- Writes need a CSRF token and a same-origin request; the session cookie is
  `HttpOnly`, `Secure`, `SameSite=Strict`. The page runs under a strict
  Content-Security-Policy.
- Glowpod logs who changed what (never the content), and never logs tokens,
  passwords or Secret values.

## Values

See [values.yaml](https://github.com/glowpod-dev/glowpod/blob/main/deploy/helm/glowpod/values.yaml); every setting there is commented. Anything
else can be passed as a `GLOWPOD_*` variable in `extraEnv` (run
`glowpod -h` for the full list).
