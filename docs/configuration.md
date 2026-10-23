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
| `-context` | current context | — | Kubeconfig context to open first; every context is listed on the Clusters page |
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
| `-ldap-username-prefix` | (Hub: `ldap:`) | `auth.ldap.usernamePrefix` | Prefix for usernames |
| `-ldap-group-base` | no groups | `auth.ldap.groupBase` | Where to search for groups |
| `-ldap-group-filter` | `(member={dn})` | `auth.ldap.groupFilter` | Filter for the user's groups; `{dn}` and `{username}` |
| `-ldap-group-attribute` | `cn` | `auth.ldap.groupAttribute` | Attribute used as the group name |
| `-ldap-groups-prefix` | (chart: `ldap:`) | `auth.ldap.groupsPrefix` | Prefix for groups |
| `-ldap-start-tls` | `false` | `auth.ldap.startTLS` | Upgrade `ldap://` with StartTLS |
| `-ldap-ca-file` | system roots | `auth.ldap.caSecret` (Secret with `ca.crt`) | CA bundle for the directory's certificate |
| `-ldap-insecure-skip-verify` | `false` | `auth.ldap.insecureSkipVerify` | Don't check the certificate. **Testing only** |

## Glowpod Hub

See [Glowpod Hub](hub.md). With `-hub`, token sign-in is off and SSO and
LDAP names always get the `oidc:` and `ldap:` prefixes, the only ones agents
impersonate.

| Flag | Default | Helm value | What it does |
| --- | --- | --- | --- |
| `-hub` | `false` | `hub.enabled` | Run as a Hub: local accounts, and clusters imported with `kubectl apply` |
| `-hub-namespace` | `$POD_NAMESPACE`, else `glowpod` | the release namespace | Where the Hub keeps its Secrets and settings |
| `-hub-admins` | | `hub.admins` (list) | Users or groups, as Kubernetes sees them (`oidc:platform`), who are Hub administrators besides `admin` |
| `-hub-ca-file` | | `hub.caSecret` (Secret with `ca.crt`) | CA that agents should trust for the Hub's address |
| `-agent-image` | this version's image | `hub.agentImage` | Image the agent runs in imported clusters |
| `-kubeconfig-ttl` | `8h` | `hub.kubeconfigTTL` | How long a kubeconfig downloaded from the Hub works (1m to 720h) |
| `-shell-image` | this version's `glowpod-shell` image | `hub.shell.image` | Image for browser shells; `hub.shell.enabled: false` turns shells off |
| `-shell-addr` | | set by the chart (`:8443`) | Also serve kubectl's proxy here over HTTPS, for shell pods, from a CA made at start |
| `-shell-hub-url` | the server URL | set by the chart (the `<release>-shell` Service) | The `https://` address shell pods reach `-shell-addr` by |
| | | `hub.localAdmin` (`true`) | Make `glowpod:admin` cluster-admin on the Hub's own cluster |

### The `glowpod-hub` image

`glowpod hub run` is the image's entrypoint: it starts K3s, then the Hub on
it with `-hub`, serving HTTPS. Set the Hub's other options with `GLOWPOD_`
variables.

| Flag | Default | What it does |
| --- | --- | --- |
| `-data-dir` | `/var/lib/glowpod` (`$GLOWPOD_DATA_DIR`) | K3s, the CA and the Hub's state; keep it on a volume |
| `-tls-dir` | `/etc/glowpod/tls` | Your certificate (`tls.crt`, `tls.key`, and `ca.crt` for a private CA); without it the Hub uses its own CA |
| `-https-addr` | `:443` | HTTPS address |
| `-http-addr` | `:80` | Plain-HTTP address, which redirects to HTTPS; empty turns it off |
| `-k3s` | `k3s` | The K3s binary |

`glowpod hub bootstrap-password` prints the first-start password
(`docker exec <container> glowpod hub bootstrap-password`).

`glowpod agent` runs in imported clusters; the import manifest sets it up.
Its flags (`-namespace`, `-secret`, `-kubeconfig`, `-log-format`) are only
for testing it outside a cluster.

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
glowpod                      # opens your current context
glowpod -context staging     # opens staging first
glowpod -no-browser -addr 127.0.0.1:9000
```

Every context in your kubeconfig (all files in `KUBECONFIG`) is a cluster.
The **Clusters** page (the house icon in the rail, or **All clusters** in
the cluster picker) shows them all: whether each one answers, its Kubernetes
version, ready nodes, CPU, memory and pods, and what needs attention. Each
cluster acts as the user its context names, exactly as `kubectl --context`
would.

Below the table, **Needs attention** lists the problems of every cluster in
one place, the same rows as each cluster's Overview: most severe and newest
first, each with the cluster it's on and a **View** link into that cluster.
It follows the filter above the table and the Glance / Deep mode, and says
how many clusters couldn't be checked.

**⌘K** (Ctrl+K), on the Clusters page or inside a cluster, also finds
objects by name in every cluster at once: type two letters or more of a
name. It looks through pods, deployments, stateful sets, daemon sets, jobs,
cron jobs, services, ingresses, config maps, secrets (names only),
volume claims, namespaces and nodes, as each cluster's user. Words narrow it:
`deploy web` finds deployments with "web" in the name, `payments api` objects
named "api" in the namespace payments. The cluster on screen comes first.
People who may not list a kind in all namespaces are searched in the
namespaces they picked in that cluster; the palette says when a cluster
needs one picked.

| State | What it means |
| --- | --- |
| Connected | The cluster answered as that context's user |
| Unreachable | It didn't answer within 10 seconds (the cluster is off, or the network or VPN is down) |
| Credentials refused | The cluster rejected the context's credentials (an expired token or cloud login) |
| No access | The user can't list nodes or pods |

Addresses carry the cluster, so you can bookmark or share a page of one
cluster: `http://127.0.0.1:7171/c/<cluster>/workloads/pods`. The id is the
context name in lowercase letters, digits and dashes (`kind-lab`,
`gke-shop-europe-west1-prod`). Glowpod reads the kubeconfig when it starts;
restart it to see a context you just added.
