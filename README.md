<p align="center">
  <a href="https://glowpod.dev">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="site/public/brand/glowpod-mark-on-dark.svg">
      <img src="site/public/brand/glowpod-mark-on-light.svg" alt="Glowpod logo" width="84" height="96">
    </picture>
  </a>
</p>

<h1 align="center">Glowpod</h1>

<p align="center">
  <a href="https://github.com/glowpod-dev/glowpod/actions/workflows/ci.yml"><img src="https://github.com/glowpod-dev/glowpod/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/glowpod-dev/glowpod/releases/latest"><img src="https://img.shields.io/github/v/release/glowpod-dev/glowpod" alt="Latest release"></a>
  <a href="https://artifacthub.io/packages/helm/glowpod/glowpod"><img src="https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/glowpod" alt="Artifact Hub"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="License: MIT"></a>
</p>

A web dashboard for Kubernetes with two views of the same cluster: **Glance**
for app developers and **Deep** for platform teams. Nothing is hidden in
Glance mode; it only shows less at once.

**[Try the demo](https://glowpod.dev/demo/)**: the real app on a sample cluster, in your
browser. Nothing to install.

## Four ways to run it

| | For | Runs on | Signs in with | Start |
| --- | --- | --- | --- | --- |
| **Local dashboard** | You, in the browser | Your machine (127.0.0.1 only) | Your kubeconfig | [`glowpod`](#run-it-on-your-machine) |
| **Terminal app** | You, in a terminal or over SSH | Your machine | Your kubeconfig | [`glowpod term`](#in-the-terminal) |
| **In the cluster** | Your team | The cluster, with Helm | SSO, LDAP or a token, each person as themselves | [Install it in a cluster](#install-it-in-a-cluster) |
| **Hub** (0.4.0+) | Your company, many clusters | One cluster, with Helm, or one `docker run` to try it (0.5.0+); others connect with an agent, like Rancher | A Glowpod account, SSO or LDAP | [Glowpod Hub](docs/hub.md) |

The first two are the same `glowpod` program and need nothing in the cluster.

## Run it on your machine

Install `glowpod` with Homebrew on macOS and Linux:

```bash
brew install glowpod-dev/tap/glowpod
```

or with [Scoop](https://scoop.sh) on Windows:

```powershell
scoop bucket add glowpod https://github.com/glowpod-dev/scoop-bucket
scoop install glowpod
```

or download it for your system from the
[latest release](https://github.com/glowpod-dev/glowpod/releases/latest)
(Linux, macOS and Windows, Intel and ARM). Then run it:

```bash
glowpod                       # opens your kubeconfig's current context
glowpod -context lab          # or opens lab first
```

Glowpod listens on `127.0.0.1` only and opens your browser with a one-time
sign-in link. Every context in your kubeconfig shows up on the **Clusters**
page, and each one runs as the user its context names. On macOS, if the
system blocks a program you downloaded by hand, allow it once with
`xattr -d com.apple.quarantine ./glowpod` (Homebrew doesn't need this). Or
build it yourself with `make build` (the binary lands in `bin/glowpod`).

### In the terminal

```bash
glowpod term                  # Glance: gauges, what needs attention, recent events
glowpod term -n team deploy   # start on a table, in a namespace
```

`Tab` switches Glance and Deep, `:` opens the command palette (`:pods`,
`:deploy`, any CRD, `:ns kube-system`), `/` filters, `l` shows logs, `y`
YAML and `?` every key. Scale, restart, delete and shell appear only when
your RBAC allows them. See the [terminal guide](docs/terminal.md) (with screenshots), or
[its design](docs/design/terminal-app.md).

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
| [Glowpod Hub](docs/hub.md) | Many clusters, one sign-in, like Rancher |

## Many clusters: Glowpod Hub

One Hub, every cluster: people sign in once, see the clusters their RBAC
gives them, and open any of them. Each cluster joins with one
`kubectl apply`, which installs a small agent that connects out to the Hub.

Try it with one container, K3s inside (0.5.0+):

```bash
docker run -d --name glowpod --restart=unless-stopped --privileged \
  -p 80:80 -p 443:443 -v glowpod:/var/lib/glowpod \
  ghcr.io/glowpod-dev/glowpod-hub:0.6.0
docker exec glowpod glowpod hub bootstrap-password
```

For production, install it with Helm on a cluster of its own:

```bash
helm install glowpod oci://ghcr.io/glowpod-dev/charts/glowpod -n glowpod --create-namespace \
  --set hub.enabled=true --set publicURL=https://glowpod.example.com \
  --set ingress.enabled=true --set ingress.host=glowpod.example.com
```

Then open it, sign in as `admin`, and import clusters under **Clusters →
Import**. The [Hub guide](docs/hub.md) has the details.

## What it does

- Overview: CPU, memory, pods, storage and network gauges; workloads or your
  apps; what needs attention; live events.
- Every resource, CRDs included, as live kubectl-style lists, with a plain
  summary, conditions, events and YAML for each object.
- Pods: a plain-language story of what happened, live logs, a terminal.
- Changes: create from a template or apply any YAML, edit YAML with a
  server-side dry-run diff, scale, restart, delete (type the name), cordon
  and drain nodes. Buttons appear only for what you're allowed to do.
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
