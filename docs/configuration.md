# Configuration reference

Glowpod is configured with command-line flags. Every flag can also be set as
an environment variable: `GLOWPOD_` plus the flag name in upper case with `_`
for `-`, e.g. `-oidc-client-secret` → `GLOWPOD_OIDC_CLIENT_SECRET`. A flag on
the command line wins over the variable. Put secrets in variables (from a
Kubernetes Secret), never in arguments, which other processes can read.

The Helm chart sets these variables from its values; the last column says
which value. Anything without a value can go in `extraEnv` or `extraArgs`.

```bash
glowpod -h          # every flag with its default
glowpod -version
```

## General

| Flag | Default | Helm value | What it does |
| --- | --- | --- | --- |
| `-mode` | `local` | always `server` | `local`: your kubeconfig, on your machine. `server`: in the cluster, people sign in |
| `-addr` | local `127.0.0.1:7171`, server `:8080` | fixed `:8080` | Address to listen on. Local mode only accepts loopback addresses |
| `-kubeconfig` | `$KUBECONFIG` or `~/.kube/config` | — | Local mode: which kubeconfig. Server mode: use it instead of the in-cluster config (for testing server mode on a laptop) |
| `-context` | current context | — | Kubeconfig context |
| `-no-browser` | `false` | — | Local mode: don't open the browser |
| `-prometheus-url` | discover | `prometheusURL` | Prometheus base URL for the network gauge. Without it Glowpod looks for a Prometheus Service in the cluster, and hides the gauge if there is none |
| `-log-format` | `text` | `logs.format` (`json`) | `text` or `json` |
| `-log-level` | `info` | `logs.level` | `debug`, `info`, `warn`, `error` |
| `-version` | | | Print the version and exit |
| `-dev` | `false` | — | Development: serve the API only; Vite serves the UI |

## Server mode

| Flag | Default | Helm value | What it does |
| --- | --- | --- | --- |
| `-public-url` | | `publicURL` | The URL people open, e.g. `https://glowpod.example.com`. Needed for SSO. Glowpod then only answers that host and sends HSTS when it's `https://` |
| `-cluster-name` | | `clusterName` | The cluster's name in the UI |
| `-trusted-proxies` | | `trustedProxies` (list) | Comma-separated CIDRs or addresses whose `X-Forwarded-For` Glowpod believes, for rate limits and logs |
| `-session-ttl` | `12h` | `session.ttl` | Sign people out this long after they signed in |
| `-session-idle` | `2h` | `session.idle` | Sign people out after this long without use |
| `-insecure-cookies` | `false` | `extraArgs: [-insecure-cookies]` | Allow the session cookie over plain HTTP. **Testing only** |

## Token sign-in

| Flag | Default | Helm value | What it does |
| --- | --- | --- | --- |
| `-auth-token` | `true` | `auth.token.enabled` | Allow signing in with a bearer token |

## SSO (OpenID Connect)

SSO is on when `-oidc-issuer` is set (Helm: `auth.oidc.enabled`).

| Flag | Default | Helm value | What it does |
| --- | --- | --- | --- |
| `-oidc-issuer` | | `auth.oidc.issuer` | Issuer URL, exactly as the provider publishes it. For Microsoft, `https://login.microsoftonline.com/<tenant-id>/v2.0` |
| `-oidc-client-id` | | `auth.oidc.clientID` | Client ID |
| `-oidc-client-secret` | | `auth.oidc.existingSecret` + `existingSecretKey`, or `clientSecret` | Client secret |
| `-oidc-scopes` | `openid,email,profile` | `auth.oidc.scopes` (list) | Scopes to request |
| `-oidc-username-claim` | `email` | `auth.oidc.usernameClaim` | ID token claim used as the Kubernetes username |
| `-oidc-groups-claim` | `groups` | `auth.oidc.groupsClaim` | Claim with the groups (a list or one string) |
| `-oidc-username-prefix` | | `auth.oidc.usernamePrefix` | Prefix for usernames |
| `-oidc-groups-prefix` | (chart: `oidc:`) | `auth.oidc.groupsPrefix` | Prefix for groups |
| `-oidc-pass-token` | `false` | `auth.oidc.passToken` | Send the ID token to the API server instead of impersonating |
| `-oidc-name` | `SSO` | `auth.oidc.name` | Provider name on the sign-in button |

The redirect URI is always `<public-url>/auth/oidc/callback`.

## LDAP / Active Directory

LDAP is on when `-ldap-url` is set (Helm: `auth.ldap.enabled`).

| Flag | Default | Helm value | What it does |
| --- | --- | --- | --- |
| `-ldap-url` | | `auth.ldap.url` | `ldaps://host:636` or `ldap://host:389` |
| `-ldap-bind-dn` | anonymous | `auth.ldap.bindDN` | Account Glowpod searches with |
| `-ldap-bind-password` | | `auth.ldap.existingSecret` + `existingSecretKey`, or `bindPassword` | Its password |
| `-ldap-user-base` | | `auth.ldap.userBase` | Where to search for users (required) |
| `-ldap-user-filter` | `(uid={username})` | `auth.ldap.userFilter` | Filter that finds exactly one user |
| `-ldap-username-attribute` | `uid` | `auth.ldap.usernameAttribute` | Attribute used as the Kubernetes username |
| `-ldap-group-base` | no groups | `auth.ldap.groupBase` | Where to search for groups |
| `-ldap-group-filter` | `(member={dn})` | `auth.ldap.groupFilter` | Filter for the user's groups; `{dn}` and `{username}` |
| `-ldap-group-attribute` | `cn` | `auth.ldap.groupAttribute` | Attribute used as the group name |
| `-ldap-groups-prefix` | (chart: `ldap:`) | `auth.ldap.groupsPrefix` | Prefix for groups |
| `-ldap-start-tls` | `false` | `auth.ldap.startTLS` | Upgrade `ldap://` with StartTLS |
| `-ldap-ca-file` | system roots | `auth.ldap.caSecret` (Secret with `ca.crt`) | CA bundle for the directory's certificate |
| `-ldap-insecure-skip-verify` | `false` | `auth.ldap.insecureSkipVerify` | Don't check the certificate. **Testing only** |

## Helm-only values

| Value | Default | What it does |
| --- | --- | --- |
| `image.repository`, `image.tag`, `image.digest`, `image.pullPolicy` | chart's appVersion | Which image. A `digest` (`sha256:…`) wins over the tag |
| `imagePullSecrets` | `[]` | For a private registry |
| `replicaCount` | `1` | Keep at 1: sessions are in memory |
| `allowManyReplicas` | `false` | Allow more replicas; needs sticky sessions at the ingress |
| `rbac.create` | `true` | Create the impersonation ClusterRole (only when SSO without `passToken`, or LDAP, is on) |
| `serviceAccount.create`, `.name`, `.annotations` | create | Glowpod's service account |
| `service.type`, `service.port` | `ClusterIP`, `80` | The Service |
| `ingress.enabled`, `.className`, `.host`, `.annotations`, `.tls` | off | An Ingress for `publicURL` |
| `networkPolicy.enabled`, `.from` | off | Only let these peers reach Glowpod |
| `resources` | 50m CPU, 64Mi request; 256Mi limit | Enough for a few dozen people; raise memory for big clusters |
| `podSecurityContext`, `securityContext` | non-root 65532, read-only root, no capabilities, `RuntimeDefault` seccomp | Restricted Pod Security Standard |
| `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints`, `priorityClassName` | | Scheduling |
| `podAnnotations`, `podLabels` | | Extra metadata |
| `extraEnv`, `extraArgs` | `[]` | More settings |
| `command` | image entrypoint | Replace the entrypoint |
| `extraVolumes`, `extraVolumeMounts`, `extraInitContainers` | `[]` | E.g. a corporate CA bundle |
| `tests.image` | `busybox:1.37` | Image for `helm test` |

The chart ships a `values.schema.json`, so a typo in a value name fails the
install instead of being ignored.

## Trusting a corporate CA (for SSO)

If your identity provider uses a private CA, mount it and point Go at it:

```yaml
extraVolumes:
  - name: corp-ca
    configMap:
      name: corp-ca            # key: ca.crt
extraVolumeMounts:
  - name: corp-ca
    mountPath: /etc/glowpod/ca
    readOnly: true
extraEnv:
  - name: SSL_CERT_DIR
    value: /etc/glowpod/ca:/etc/ssl/certs
```

## Local mode

Local mode takes no sign-in settings: it uses your kubeconfig's user, listens
on `127.0.0.1:7171` only, and opens the browser with a one-time sign-in link.

```bash
glowpod                      # current context
glowpod -context staging     # another context
glowpod -no-browser -addr 127.0.0.1:9000
```
