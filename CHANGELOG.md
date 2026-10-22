# Changelog

All notable changes to Glowpod. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://github.com/Baid0/glowpod/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/Baid0/glowpod/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Baid0/glowpod/releases/tag/v0.1.0
