# Security policy

Glowpod sits between people and their Kubernetes clusters, so we take
security reports seriously and fix them first.

## Report a vulnerability

**Please don't open a public issue.** Report it privately instead:
[Security → Report a vulnerability](https://github.com/glowpod-dev/glowpod/security/advisories/new)
on GitHub.

Tell us what you found, how to reproduce it, and what an attacker could do
with it. We'll answer within 3 working days, keep you posted while we fix it,
and credit you in the advisory unless you'd rather we didn't.

## Supported versions

Only the latest release gets security fixes. Upgrade with
`helm upgrade` (see [docs/install.md](docs/install.md#upgrade)).

## What counts

Especially welcome: anything that lets someone act as another person or
beyond their own RBAC, read another person's data, see Secret values without
revealing them, bypass CSRF or the Content-Security-Policy, or get tokens,
passwords or Secret data into logs. The security model is in
[docs/operations.md](docs/operations.md#security-model-in-short).

Out of scope: problems that need cluster-admin rights to begin with, and
installs that turn off protections on purpose (`-insecure-cookies`,
`ldap.insecureSkipVerify`).

## Verify what you run

Release images and charts are signed with Sigstore cosign, and every release
artifact has build provenance and an SBOM. See
[docs/install.md](docs/install.md#verify-what-you-download).
