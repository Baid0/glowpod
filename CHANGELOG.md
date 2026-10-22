# Changelog

All notable changes to Glowpod. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `glowpod term`: Glowpod in the terminal. It starts on Glance (half-gauges drawn in the
  terminal, what needs attention in plain words, recent events); `Tab` goes to Deep, with the
  API server's columns for any resource or CRD, `/` filter, namespace switching, live updates
  and a `:` command palette. Pods open to live logs (warnings orange, errors red), events and
  YAML. Scale, restart, delete (type the name) and shell show only when RBAC allows them.
- A live demo at https://glowpod.dev/demo/: the real app on a sample cluster that runs in the
  browser, with a crash-looping pod, a pod that doesn't fit and a node short of memory to look
  into. Scaling, restarting, deleting, editing YAML, the terminal and Secret reveal all work on
  the sample data, which resets on reload. The home page links to it.

## [0.1.4] - 2026-10-22

### Added

- The chart has an icon (the Glowpod mark on lime), so Artifact Hub and other chart browsers show
  the logo instead of the generic Helm one.

## [0.1.3] - 2026-10-22

### Changed

- Glowpod moved to the glowpod-dev organization: the repository is
  https://github.com/glowpod-dev/glowpod, the image `ghcr.io/glowpod-dev/glowpod` and the OCI
  chart `oci://ghcr.io/glowpod-dev/charts/glowpod`. Old links redirect, and 0.1.2 and earlier stay
  at `ghcr.io/baid0`. Releases are now signed by the glowpod-dev/glowpod workflow; see the install
  guide for the new verify commands.
- The home page installs with `helm repo add`.

### Added

- Classic chart repository: `helm repo add glowpod https://glowpod.dev/charts`. Every release
  adds its version, and the chart file is attached to the GitHub Release (with a checksum and a
  build attestation). 0.1.1 and 0.1.2 are in it already.
- Artifact Hub: the chart lists its image (for security scanning), each release's changes from
  this changelog, a category and links to the website, docs and security policy. The chart's
  README links to glowpod.dev, so they work on the package page.

## [0.1.2] - 2026-10-22

### Security

- go-jose 4.1.6 fixes [GHSA-mgcc-cww5-fp2q](https://github.com/go-jose/go-jose/security/advisories/GHSA-mgcc-cww5-fp2q):
  oversized RSA keys in an identity provider's key set could slow SSO sign-in down. Also
  oauth2 0.37 and sync 0.24.

### Changed

- Logs: warnings are orange, errors and critical lines red, on the margin marker and the
  text, with darker shades in light mode.

### Fixed

- Logs: a line's own level decides its color (JSON or logfmt `level` / `severity`, or
  klog's `E`/`W` prefix). Warnings that carried an `"err"` field were shown as errors.
- Helm releases: the Revision column is centered under its header.

### Added

- The glowpod.dev website, with every guide as a docs page, deployed to Cloudflare Pages
  with a strict Content-Security-Policy.
- Dependabot keeps GitHub Actions, Go, npm and Docker images current.

## [0.1.1] - 2026-10-22

### Added

- `glowpod` downloads for Linux, macOS and Windows (amd64 and arm64) on every
  GitHub Release, with SHA-256 checksums.
- Signed releases: the image and the Helm chart are signed with Sigstore
  cosign (keyless); the image carries an SBOM and build provenance; the
  downloads have GitHub build attestations and an SBOM.
- MIT license, security policy and this changelog.

## [0.1.0] - 2026-10-22

First release.

### Added

- **Overview** with CPU, memory, pods, storage and network gauges; workload
  tiles (Deep) or app cards (Glance); what needs attention; live events.
- **Glance and Deep modes** for app developers and platform teams.
- **Every resource, CRDs included**, as live kubectl-style lists with
  paging, filters and several namespaces at once.
- **Object pages**: a plain summary, conditions, events and YAML; for pods a
  status timeline, live logs and a terminal.
- **Changes**: YAML edit with a server-side dry-run diff, scale, restart,
  delete with name confirmation, cordon and drain. Buttons follow RBAC.
- **Helm releases** (never their values) and a resource map per namespace.
- **Local mode**: `glowpod` with your kubeconfig, on `127.0.0.1` only.
- **In-cluster mode** with sign-in by SSO (OpenID Connect), LDAP / Active
  Directory or a service-account token. Everyone acts as themselves through
  token pass-through or impersonation.
- Helm chart with a values schema, `helm test`, NetworkPolicy and a
  restricted security context; multi-arch image (amd64, arm64).
- Light and dark themes, settings for thresholds, density and refresh.

[Unreleased]: https://github.com/glowpod-dev/glowpod/compare/v0.1.4...HEAD
[0.1.4]: https://github.com/glowpod-dev/glowpod/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/glowpod-dev/glowpod/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/glowpod-dev/glowpod/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/glowpod-dev/glowpod/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/glowpod-dev/glowpod/releases/tag/v0.1.0
