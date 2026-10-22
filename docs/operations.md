# Running Glowpod

Day-two notes for an in-cluster install: what Glowpod stores, what it logs,
how to watch it, and what to check when something is off.

## Security model in short

- **Everyone acts as themselves.** Each Kubernetes call carries the signed-in
  person's token, or impersonates them. Glowpod's service account may only
  `impersonate` users and groups (and has no rights at all with tokens only,
  or SSO with `passToken`).
- **Nothing shared between people.** Glowpod keeps no cluster data across
  requests that another person could be served; watches and lists run per
  person.
- **Buttons follow RBAC**, checked with `SelfSubjectAccessReview`; a 403 is
  still handled if rights change mid-session.
- **Secret values** stay hidden until someone clicks *Reveal* and is allowed
  to read that Secret. Helm release values are never shown.
- **Browser side:** session cookie `HttpOnly`, `Secure`, `SameSite=Strict`;
  a CSRF token on every change; a strict Content-Security-Policy with a fresh
  nonce per page; HSTS on HTTPS; only the `publicURL` host is answered (other
  hosts get `421 Misdirected Request`).
- **Container:** distroless, non-root (65532), read-only root filesystem, no
  capabilities, `RuntimeDefault` seccomp. It passes the *restricted* Pod
  Security Standard.

## What Glowpod stores

Nothing on disk. Sessions (and, for token sign-in, the token) live in
memory and are gone when the pod restarts. There's nothing to back up.

Because sessions are in memory:

- Keep **one replica**. Glowpod is light: one pod serves many people.
- A restart or upgrade signs everyone out; they sign in again.
- If you must run more replicas, set `allowManyReplicas: true` and turn on
  sticky sessions (cookie affinity) at the ingress. A session then only works
  on the pod that created it.

Sessions end after `session.ttl` (12 h) or `session.idle` (2 h without use),
and on sign-out.

## Logs

Use `logs.format: json` (the chart default) for log collectors. Every line has
`time`, `level` and `msg`. People are recorded by `user` at sign-in and by
`identity` (a stable hash, never the token) after that, so you can join the
two.

| `msg` | Level | Fields | When |
| --- | --- | --- | --- |
| `starting glowpod` | info | `version`, `mode` | Start |
| `glowpod is serving` | info | `addr`, `public_url` | Ready |
| `signed in` | info | `method`, `user`, `identity`, `ip` | A sign-in |
| `signed out` | info | `identity` | A sign-out |
| `changed resource` | info | `action`, `identity`, `group`, `resource`, `namespace`, `name` | Apply, scale, restart, delete |
| `changed node` | info | `action`, `identity`, `name` | Cordon, uncordon, drain |
| `opened a shell` | info | `identity`, `namespace`, `pod`, `container` | Exec terminal |
| `token sign-in failed`, `LDAP sign-in failed`, `SSO …` | warn | `err` | See [authentication.md](authentication.md#troubleshooting-sign-in) |
| `cluster request failed` | warn | `code`, `err` | The API server returned an unexpected error |

Glowpod never logs tokens, passwords, kubeconfigs, Secret values or the
content of a change. For a full record of *what* changed, use the Kubernetes
audit log: because Glowpod impersonates or passes tokens, it shows the real
person, with Glowpod's service account as the impersonator.

`ip` is the client's address when `trustedProxies` lists your ingress
controller; otherwise it's the proxy's.

## Health and monitoring

- `GET /healthz` answers `200` whenever the process is up; the chart uses it
  for readiness and liveness probes. It skips the host check, so probes work
  without `publicURL`.
- `helm test glowpod -n glowpod` checks the Service end to end.
- Watch the pod's restarts and memory. Memory grows with the number of
  signed-in people and open live views; the chart's 256Mi limit covers a few
  dozen people on a mid-size cluster. Raise it for more.

## Network

Glowpod needs to reach:

- the Kubernetes API server (in-cluster, `kubernetes.default.svc`);
- your SSO issuer, from the pod (discovery, keys, token exchange);
- your LDAP server;
- Prometheus, if you use the network gauge.

If you restrict egress with NetworkPolicy, allow these. `networkPolicy.enabled`
in the chart restricts **ingress** to Glowpod only.

Live updates, logs and the terminal use WebSockets on the same host. Your
ingress must pass `Upgrade` requests and allow long idle connections (an hour
or more).

## Upgrades

```bash
helm upgrade glowpod deploy/helm/glowpod -n glowpod -f glowpod-values.yaml --set image.tag=<new>
```

Read the release notes for changed values; the values schema rejects ones
that no longer exist. Pin `image.digest` for reproducible deploys.

## Troubleshooting

**The pod won't start.** Check `kubectl -n glowpod logs deploy/glowpod`.
Configuration errors are one plain line, for example:

- `-public-url is http://; serve Glowpod over HTTPS, or pass -insecure-cookies for a test`
- `SSO needs -public-url and -oidc-client-id`
- `LDAP needs -ldap-user-base`

**"Unknown host" (421).** The browser's host doesn't match `publicURL`.
Fix `publicURL`, or open Glowpod at that address.

**Sign-in works but the page bounces back to sign-in.** The browser isn't
sending the cookie: Glowpod is served over plain HTTP. Use HTTPS (or
`localhost` for a port-forward).

**Signed in, but lists are empty or say you aren't allowed.** That's the
person's RBAC. Check with
`kubectl auth can-i --list -n <ns> --as=<user> --as-group=<group>`; see
[authentication.md](authentication.md#give-people-access).

**SSO/LDAP users get errors on everything** (`impersonate` forbidden). The
impersonation ClusterRole is missing: keep `rbac.create: true`, or create
`<release>-impersonator` yourself.

**Live views stop updating after a minute.** The ingress closes idle
WebSockets; raise its read/send timeouts.

**CPU and memory gauges say "Install metrics-server".** Install
[metrics-server](https://github.com/kubernetes-sigs/metrics-server). The
network gauge appears only when Prometheus is found or `prometheusURL` is set.
