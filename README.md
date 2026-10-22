# Glowpod

A web dashboard for Kubernetes with two views of the same cluster: **Glance**
for app developers and **Deep** for platform teams. Nothing is hidden in
Glance mode; it only shows less at once.

## Run it on your machine

Download `glowpod` for your system from the
[latest release](https://github.com/glowpod-dev/glowpod/releases/latest), unpack it
and run it:

```bash
tar -xzf glowpod_*_darwin_arm64.tar.gz && cd glowpod_*_darwin_arm64
./glowpod                     # uses your kubeconfig's current context
./glowpod -context lab        # or pick one
```

Glowpod listens on `127.0.0.1` only and opens your browser with a one-time
sign-in link. Everything runs as your kubeconfig's user. On macOS, if the
system blocks a downloaded program, allow it once with
`xattr -d com.apple.quarantine ./glowpod`. Or build it yourself with
`make build` (the binary lands in `bin/glowpod`).

## Install it in a cluster

```bash
helm install glowpod oci://ghcr.io/glowpod-dev/charts/glowpod -n glowpod --create-namespace \
  --set publicURL=https://glowpod.example.com \
  --set ingress.enabled=true --set ingress.host=glowpod.example.com
```

Or from the classic chart repository:

```bash
helm repo add glowpod https://glowpod.dev/charts
helm install glowpod glowpod/glowpod -n glowpod --create-namespace \
  --set publicURL=https://glowpod.example.com \
  --set ingress.enabled=true --set ingress.host=glowpod.example.com
```

People sign in with **SSO (OpenID Connect)**, **LDAP / Active Directory** or a
**service-account token**, and act as themselves: every Kubernetes call runs
with their own RBAC.

| Guide | |
| --- | --- |
| [Install](docs/install.md) | Image, Helm values, ingress and TLS, upgrade, uninstall |
| [Sign-in and access](docs/authentication.md) | Tokens, SSO (Okta, Entra ID, Google, Keycloak, Dex, …), LDAP and Active Directory, RBAC for people |
| [Configuration](docs/configuration.md) | Every flag, environment variable and Helm value |
| [Running Glowpod](docs/operations.md) | Security model, logs, health, troubleshooting |
| [SSO demo](deploy/examples/sso-demo/README.md) | Dex + LDAP in a throwaway namespace, to try it all in ten minutes |

## What it does

- Overview: CPU, memory, pods, storage and network gauges; workloads or your
  apps; what needs attention; live events.
- Every resource, CRDs included, as live kubectl-style lists, with a plain
  summary, conditions, events and YAML for each object.
- Pods: a plain-language story of what happened, live logs, a terminal.
- Changes: edit YAML with a server-side dry-run diff, scale, restart,
  delete (type the name), cordon and drain nodes. Buttons appear only for
  what you're allowed to do.
- Helm releases (never their values), a resource map per namespace.

## Develop

```bash
make dev        # Go API + Vite dev server on http://localhost:5173
make lint test  # before every commit
make e2e        # Playwright smoke tests
make chart      # lint and render the Helm chart
make dist       # glowpod downloads for every platform, in dist/
```

### Release

1. Move the changes under `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md)
   to a new version heading, and bump `version` and `appVersion` in
   `deploy/helm/glowpod/Chart.yaml`.
2. Commit, then tag and push: `git tag v0.2.0 && git push origin main v0.2.0`.

The release workflow publishes the signed image and chart, then a GitHub
Release with the downloads, checksums, SBOM and the changelog section as
notes. A tag with a dash (`v0.2.0-rc.1`) makes a pre-release.

Project conventions and the design system are in [CLAUDE.md](CLAUDE.md);
choices that depart from it are in [docs/decisions.md](docs/decisions.md).

## License

[MIT](LICENSE). Security issues: see [SECURITY.md](SECURITY.md).
