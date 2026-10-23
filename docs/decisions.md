# Decisions

Choices made while building Glowpod that aren't obvious from CLAUDE.md, or
that depart from it. Newest last.

## YAML editor: CodeMirror 6 instead of Monaco (Phase 3)

CLAUDE.md lists Monaco. Glowpod uses CodeMirror 6 instead because:

- **CSP.** Monaco needs web workers (`worker-src blob:`) and injects inline
  styles in many places. CodeMirror creates one `<style>` element, which
  takes a nonce, so the strict policy stays: `script-src 'self'`, and
  `style-src 'self' 'nonce-…'` with a fresh nonce on every page load.
- **Size.** About 370 KB, loaded only when someone edits, against several MB
  for Monaco.
- **Accessibility.** CodeMirror's content is a real `contenteditable` with
  an `aria-label`. Esc then Tab leaves the editor.

To switch back, replace `frontend/src/features/yaml/CodeEditor.tsx`; the edit
flow around it (`EditYaml.tsx`) doesn't depend on the editor.

## Edits are full updates with a dry run first (Phase 3)

Saving YAML sends `PUT` (update) with the `resourceVersion` the editor
started from, after a server-side dry run that returns before/after for the
diff. A change someone else made in between is a conflict (409), never
silently overwritten. Name, namespace, kind and apiVersion can't change in
an edit. A Secret can only be edited after its values are revealed, and the
server refuses a Secret sent back with every value blank.

## CSRF (Phase 3)

Every write needs the session's CSRF token in `X-CSRF-Token` (from
`GET /api/v1/session`) and must not come from another origin
(`Origin` / `Sec-Fetch-Site`). This is on top of the `SameSite=Strict`
session cookie.

## Hiding actions (Phase 3)

`POST /api/v1/clusters/{cluster}/access` runs one `SelfSubjectAccessReview`
per check. The UI shows a button only when discovery says the API supports
the verb **and** the review allows it. A 403 on the real request is still
shown in plain words, with the cluster's reason.

## Server mode identities (Phase 5)

- **Token sign-in** passes the token through on its own; none of Glowpod's
  credentials are mixed in.
- **SSO and LDAP** impersonate the user by default, so they work on any
  cluster without API-server flags. Glowpod's service account has only the
  `impersonate` verb on users and groups. Glowpod refuses `system:` users and
  drops `system:` groups; the chart prefixes groups (`oidc:`, `ldap:`).
- SSO can instead pass the ID token through (`passToken`) where the API
  server trusts the issuer.
- Sessions are in memory, so the chart runs one replica.
- People who can't list nodes or namespaces still get a working UI: the
  load gauges say "No access", and the namespace picker lets them type one.

## Libraries added in Phase 5

`github.com/coreos/go-oidc/v3` (ID token verification, discovery) and
`github.com/go-ldap/ldap/v3`. Both are security-sensitive protocol code
that shouldn't be hand-written.

## Preferences live in the browser (Phase 6)

Theme, density, refresh interval, view mode and gauge thresholds are saved
per browser (localStorage), and thresholds per cluster. CLAUDE.md asks for
"saved per user"; in local mode the browser is the user, and in server mode
it avoids storing state in the cluster. The server's thresholds stay the
default; a person's own only change how their gauges are colored, not the
"needs attention" list.

The theme is applied before first paint by `public/theme.js`, a same-origin
script, because the CSP blocks inline scripts.

## Log lines: orange warnings, red errors

CLAUDE.md allows one extra hue, Rose, and only for critical states. In the
log viewer, warnings are orange and errors (error, fatal, panic, critical)
are red, as asked for: long logs are scanned by color first. The colored
margin marker and text come with the level word in the line itself, so
color is never the only signal. Orange stays in the log viewer
(`--log-warn`, `--log-error`); everywhere else "high" is still white or Bio
Black.

Text needs AA contrast, so light mode uses darker shades: orange `#C2410C`
and red `#B91C1C`, since Rose text on white is only 3.8:1. Dark mode uses
`#FB923C` and Rose.

A line's own level decides its color: a JSON or logfmt `level` /
`severity` field, or Kubernetes' klog prefix (`E1022 …`, `W1022 …`). Only
lines without one are judged by words such as ERROR, and a key named `err`
doesn't count.

## The terminal app (`glowpod term`)

- **Libraries:** Bubble Tea v2, Lip Gloss v2 and Bubbles v2 (`charm.land/…`),
  the usual Go terminal UI stack; drawing a full-screen UI by hand isn't
  "a few lines". `muesli/cancelreader` (already pulled in by Bubble Tea)
  stops the shell's stdin copy when the shell exits, so it doesn't eat the
  next key.
- **One code path with the web:** the app reads through a `Source`
  interface whose methods return the HTTP API's shapes. Local mode uses
  the same internal packages as the server (`overview`, `resources`,
  `logs`, `access`, `events`, `terminal`), so signing in to an in-cluster
  Glowpod later is a second `Source`, not a second app.
- **Gauge track:** the arc is drawn with Braille dots, which are thin, so
  the track uses `--hairline-strong` (`#3A3A3A`) instead of
  `--surface-elevated`, which disappears on Bio Black. Card borders use the
  same grey for the same reason.
- **Colour:** the terminal's background is set to Bio Black while the app
  runs (and restored on exit). Status is always a dot and a word. Orange
  is used for warning log lines only, as on the web.
- **Learning from k9s:** keys people know from k9s (`:`, `/`, `l`, `y`,
  `s`, `r`, `ctrl+d`) work the same way. No k9s code is in Glowpod; we
  studied its source (Apache-2.0) and the running binary.
- **Not yet:** revealing Secret values, editing YAML, port-forward and the
  light theme; signing in to an in-cluster Glowpod.

## Many clusters in local mode (Hub phase 1)

- **One cluster per kubeconfig context.** Local mode lists every context
  (all files in `KUBECONFIG`), each with its own clients built from that
  context, so each acts as the user it names. A context whose config won't
  build (a missing certificate file) is still listed and says why.
- **Ids from context names.** `/c/<id>/` uses the context name in lowercase
  letters, digits and dashes. Names that collide, come out empty or are
  longer than 40 characters get a short hash of the full name, so ids are
  unique and stable across restarts. An in-cluster Glowpod keeps the id
  `default`, so its bookmarks stay the same shape.
- **Opening a cluster.** `glowpod` opens the current context (or `-context`),
  not the Clusters page: that's what `kubectl` users expect. `/` shows the
  Clusters page when there's more than one cluster, and goes straight into
  the only one otherwise. Old addresses without `/c/` open on the current
  cluster.
- **Links.** Pages link with plain paths (`/workloads/pods`).
  `lib/clusterLinks.tsx` (`Link`, `NavLink`, `Navigate`) and
  `lib/clusterPaths.ts` (`useNavigate`) add the cluster the page is on, so
  no page builds `/c/…` itself.
- **Per cluster in the browser:** picked namespaces (`glowpod.namespaces`,
  one entry per cluster; the old single value moves to the in-cluster one),
  gauge thresholds, and the live stream (one WebSocket for the cluster on
  screen).
- **Unreachable clusters fail fast.** Connecting to a kubeconfig's API
  server gives up after 10 seconds. The Overview stops every other call as
  soon as nodes or pods fail for a reason other than RBAC, and a Clusters
  row answers at 10 seconds even if a call (the server version) can't be
  cancelled. The browser doesn't retry `cluster_timeout` or
  `cluster_unreachable`.
- **Not yet:** re-reading the kubeconfig while Glowpod runs; the terminal
  app still opens one context (`:ctx` is planned); the live dot can say Live
  for a cluster whose watches are failing, because the stream reports ready
  before its first list.

## Glowpod Hub (Hub phase 2)

- **Tunnel:** the agent opens one WebSocket to the Hub (`/v3/connect`) and
  both sides run yamux over it (`hashicorp/yamux`, MPL-2.0). The Hub opens a
  stream per request and speaks plain HTTP on it; the WebSocket is the TLS.
  client-go works unchanged through a custom `rest.Config.Dial`.
- **Shells through the tunnel use SPDY.** client-go's WebSocket executor
  ignores a custom dialer, so when a config has `Dial` set, `terminal.Remote`
  speaks SPDY over that dialer (`UpgradeTransport`). The agent sends
  upgrades over HTTP/1.1.
- **The agent's guard:** exactly one `Impersonate-User` with a Glowpod
  prefix (`glowpod:`, `oidc:`, `ldap:`), prefixed groups or
  `system:authenticated`, and no other `Impersonate-*` header. Its own token
  replaces any `Authorization` the Hub sent. A Hub therefore forces those
  prefixes, turns token sign-in off and refuses `-oidc-pass-token`.
- **Secrets:** an import token and an agent credential are
  `<cluster>.<32 random bytes>`; the Hub stores only their SHA-256. Local
  passwords are Argon2id (19 MiB, 2 passes), in one Secret per user, not in
  a User CRD as the design first said: password hashes belong in Secrets.
  The bootstrap password is in a Secret and never in the log.
- **One Hub replica:** sessions are in memory and each agent keeps one
  tunnel to one pod. The chart refuses `replicaCount` above 1 with
  `hub.enabled`.
- **Cluster list:** the agent answers `/glowpod/status` itself (version and
  the subjects of every binding, cached 15 s); the Hub asks every 30 s and
  on connect. A Hub always shows the Clusters page at `/`, even with one
  cluster, because that's where clusters are imported.
- **Tested** with the Hub on a laptop against the lab as `local`, the import
  manifest applied to the lab with kubectl, and the agent run with its own
  service account's token (so with exactly the manifest's rights). Not yet
  tested: the agent's image in a cluster; that needs a release.
