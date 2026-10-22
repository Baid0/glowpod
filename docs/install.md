# Install Glowpod in a cluster

In-cluster ("server") mode runs Glowpod as one Deployment. Everyone signs in
(SSO, LDAP or a token) and **acts as themselves**: what they see and change is
exactly what their own Kubernetes RBAC allows. Glowpod's service account can
only *impersonate* users; it never reads or changes anything for itself.

This page takes you from nothing to a working HTTPS install. For sign-in
setup see [authentication.md](authentication.md); every setting is in
[configuration.md](configuration.md); day-two work is in
[operations.md](operations.md).

Want to try it first with no identity provider of your own? The
[SSO demo](../deploy/examples/sso-demo/README.md) sets up an LDAP server and
Dex next to Glowpod in a throwaway namespace.

## What you need

- Kubernetes 1.28 or newer and `kubectl` access as a cluster admin (to create
  the impersonation ClusterRole).
- Helm 3.8 or newer.
- An ingress controller and a DNS name for Glowpod, e.g. `glowpod.example.com`.
- A TLS certificate for that name. The session cookie is `Secure`, so
  browsers only send it over HTTPS.
- Optional: metrics-server (CPU and memory gauges) and Prometheus (network
  gauge). Glowpod works without them and says what's missing.

## 1. Get the image

Glowpod is one static binary in a distroless image (`linux/amd64` and
`linux/arm64`, runs as non-root user 65532).

Tagged releases (`v*`) are built by the release workflow and pushed to
`ghcr.io/<owner>/glowpod`, with the chart as an OCI artifact next to it. To
build and push your own:

```bash
make image-push IMAGE=registry.example.com/platform/glowpod VERSION=0.1.0
```

This needs Docker with `buildx`. `make image` builds for your machine only.

## 2. Pick a namespace and the URL

```bash
kubectl create namespace glowpod
```

`publicURL` is the address people open. Glowpod uses it for the SSO redirect
and **only answers requests for that host**, which blocks DNS-rebinding and
host-header tricks. Use the bare origin, no path:
`https://glowpod.example.com`.

## 3. Write your values

Start from this and add a sign-in method from
[authentication.md](authentication.md):

```yaml
# glowpod-values.yaml
image:
  repository: registry.example.com/platform/glowpod
  tag: "0.1.0"

publicURL: https://glowpod.example.com
clusterName: prod-eu-1

ingress:
  enabled: true
  className: nginx
  host: glowpod.example.com
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt
    # Live logs, watches and the terminal are WebSockets; keep them open.
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
  tls:
    - secretName: glowpod-tls
      hosts: [glowpod.example.com]

# The ingress controller's pod network, so sign-in rate limits and logs see
# the real client address from X-Forwarded-For.
trustedProxies:
  - 10.244.0.0/16

# Only the ingress controller may reach Glowpod.
networkPolicy:
  enabled: true
  from:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: ingress-nginx

auth:
  token:
    enabled: true   # people paste a service-account token; see authentication.md
```

Notes:

- **TLS.** The example uses [cert-manager](https://cert-manager.io). With a
  certificate you already have, create the Secret yourself:
  `kubectl -n glowpod create secret tls glowpod-tls --cert=tls.crt --key=tls.key`.
  When `publicURL` is `https://`, Glowpod also sends
  `Strict-Transport-Security`.
- **Other ingress controllers.** Traefik and most cloud load balancers keep
  WebSockets open without extra settings. Whatever you use, make sure
  WebSocket upgrades pass through and idle timeouts are an hour or more.
- **trustedProxies.** List only the addresses your ingress controller connects
  from (often the pod CIDR, or the load balancer's subnet). Leave it empty if
  nothing sits in front of Glowpod; a wrong entry lets clients choose the
  address Glowpod logs.
- **No ingress?** Leave `ingress.enabled: false` and expose the `glowpod`
  Service your own way (Gateway API, a LoadBalancer Service, a mesh).

## 4. Install

```bash
helm upgrade --install glowpod deploy/helm/glowpod -n glowpod -f glowpod-values.yaml
kubectl -n glowpod rollout status deploy/glowpod
helm test glowpod -n glowpod
```

`helm test` runs a pod that asks Glowpod for its sign-in methods through the
Service, with your `publicURL` host. The install notes print the redirect URI to register with
your SSO provider and an example RoleBinding.

The chart refuses settings that can't work, for example SSO without
`publicURL`, more than one replica without `allowManyReplicas`, or no sign-in
method at all.

## 5. Sign in and give people access

Open `publicURL`. Until people have RBAC of their own they can sign in but
see nothing. The quickest start is a binding per team:

```bash
# Everyone in the SSO group "payments" may change things in their namespace.
kubectl create rolebinding payments-edit -n payments --clusterrole=edit --group=oidc:payments
# The platform team sees and does everything.
kubectl create clusterrolebinding platform-admin --clusterrole=cluster-admin --group=oidc:platform
```

See [authentication.md](authentication.md#give-people-access) for the
group prefixes and what Glowpod needs to list namespaces.

## Try it without an ingress

For a quick look, skip the ingress and HTTPS and port-forward. Browsers send
`Secure` cookies to `localhost`, so this works:

```bash
helm upgrade --install glowpod deploy/helm/glowpod -n glowpod
kubectl -n glowpod port-forward svc/glowpod 8080:80
```

Open <http://localhost:8080> and sign in with a token.

## Upgrade

```bash
helm upgrade glowpod deploy/helm/glowpod -n glowpod -f glowpod-values.yaml --set image.tag=0.2.0
```

Sessions are kept in memory, so an upgrade (or any restart) signs everyone out.
They sign in again; nothing else is lost, because Glowpod stores nothing.

## Uninstall

```bash
helm uninstall glowpod -n glowpod
kubectl delete namespace glowpod
```

This removes the impersonation ClusterRole and its binding too. RoleBindings
you made for people stay; they're plain Kubernetes RBAC.
