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
- **One Hub replica:** each agent keeps one tunnel to one pod, so a second
  pod couldn't reach that cluster. The chart refuses `replicaCount` above 1
  with `hub.enabled`. Running several needs pods to pass requests to the pod
  holding the tunnel, as Rancher's do.
- **Sign-ins survive a restart:** a Hub saves each session in a
  Secret, `glowpod-session-<SHA-256 of the cookie>`, holding the name,
  groups, CSRF token and times as JSON, never the cookie. Memory stays the
  cache; last-seen is written at most once a minute; expired ones are
  deleted when found and every 10 minutes. Only sessions that act by name
  are saved: a session holding a person's own token (token sign-in,
  `-oidc-pass-token`) stays in memory, so in-cluster Glowpod still signs
  those out on restart rather than storing tokens. A sign-in fails if its
  Secret can't be written, instead of silently ending at the next restart.
- **Cluster list:** the agent answers `/glowpod/status` itself (version and
  the subjects of every binding, cached 15 s); the Hub asks every 30 s and
  on connect. A Hub always shows the Clusters page at `/`, even with one
  cluster, because that's where clusters are imported.
- **Tested** with the Hub on a laptop against the lab as `local`, the import
  manifest applied to the lab with kubectl, and the agent run with its own
  service account's token (so with exactly the manifest's rights). Not yet
  tested: the agent's image in a cluster; that needs a release.

## Docker quick start (Hub phase 3)

- **One image, one process tree:** `glowpod-hub` is the `rancher/k3s` image
  (pinned, `v1.36.5-k3s1`) plus the `glowpod` binary, built from a `hub`
  stage of the same Dockerfile. `glowpod hub run` is PID 1: it moves the
  container's processes to a child cgroup (cgroup v2, as k3d and Rancher
  do), starts `k3s server` as a child and serves the Hub in-process. If K3s
  stops, the Hub stops, and Docker's restart policy brings both back.
- **No Helm inside:** the CRD, the `glowpod` namespace and the
  `glowpod:admin` binding go in K3s's auto-deploy folder, applied at every
  start. A test keeps that CRD the same as the chart's. The Hub uses K3s's
  admin kubeconfig as its own identity: it's alone in the container, and
  people still act through impersonation.
- **HTTPS with its own CA,** made at first start and kept in the volume
  (ECDSA P-256, ten years). Server certificates are minted per name set:
  localhost, the name the client asked for, and the server URL's host, read
  from the Hub's settings every 15 seconds, because agents connecting by IP
  send no name and Docker's port mapping hides the server's address from
  the container. Certificates start 24 hours back: the lab's clock is an
  hour behind and refused certificates that started an hour back. The
  import manifest already carries the CA (`-hub-ca-file`). Your own
  certificate in `/etc/glowpod/tls` replaces all of this.
- **The bootstrap password** is read with
  `docker exec … glowpod hub bootstrap-password`, which prints to the
  terminal of whoever runs it, not to the log.
- **Tested** on Docker Desktop (arm64): first start in 4 seconds with the
  image pulled; the CA survived recreating the container; the lab imported
  through the `curl --insecure` command; the published 0.4.0 agent
  connected over HTTPS trusting the Hub's CA; summaries of both `local` and
  `lab`; `docker restart` kept the sign-in and the agent reconnected by
  itself.

## kubectl through the Hub (Hub phase 4, first half)

- **One proxy, one rule:** `/k8s/clusters/<id>/…` takes a kubeconfig
  token only (no cookie, so no CSRF), drops `Authorization`, `Cookie` and
  every `Impersonate-*` header the client sent, and calls the cluster with
  the Hub's own identity impersonating the token's owner, through client-go's
  impersonating round tripper. For imported clusters that goes through the
  tunnel, where the agent's guard still applies. Upgrades (exec, attach,
  port-forward, kubectl's WebSockets) use an HTTP/1.1 transport; watches
  flush at once. Errors are Kubernetes `Status` objects, so kubectl prints
  the message.
- **Who sees which cluster** is the same rule as the Clusters page: a
  cluster you can't see answers 404, as if it didn't exist.
- **Tokens** are `<id>.<32 random bytes>`; a Secret per token keeps its
  SHA-256, the name and groups it acts as (as they were at download), and
  its times. Checked tokens are trusted for 30 seconds in memory; revoking
  clears that at once, and the Hub has one replica. Last use is written at
  most every 5 minutes. Expired ones are deleted every 10 minutes. 20 per
  person, oldest replaced. Download is a POST with CSRF (it makes a token),
  saved in the browser from a blob.
- **Sign-in limits** count failed attempts per person (`blocked`/`fail`);
  the per-address limit still counts every attempt.
- **Tested** on the Docker Hub with your mkcert certificate and the lab
  imported: `auth whoami` (and `--as system:admin` ignored), nodes on `lab`
  and `local`, discovery, `exec` and `port-forward` through the agent,
  `logs`, `get -w`, and revoke ending kubectl at once with the message.

## Browser shell (Hub phase 4, second half)

- **A pod per shell,** in the Hub's namespace, from `glowpod-shell` (Alpine
  3.24, kubectl and Helm checked against their published checksums, jq, yq,
  glowpod). The Hub creates it with its own identity and opens bash in it
  through pods/exec, the same terminal WebSocket as pod shells; people get
  no rights in that namespace. Locked down to the restricted Pod Security
  profile, 12-hour deadline, 30 minutes idle, deleted when the WebSocket
  ends; a restart deletes leftovers, since shells don't survive one.
- **Its kubeconfig** holds a token scoped to one cluster and marked as a
  shell's (not listed or capped with downloaded kubeconfigs), in a Secret
  owned by the pod. It goes through the same `/k8s` proxy.
- **The proxy inside the cluster is HTTPS:** kubectl won't send a token to a
  plain-HTTP server. The Hub makes a CA in memory at start and serves the
  proxy on `-shell-addr` with certificates from it (the chart's
  `<release>-shell` ClusterIP Service on 8443; the container's own address
  on 8081 in Docker). Shells end with the process, so the CA never needs
  saving.
- **NetworkPolicy:** shell pods may reach only that port (and DNS, in the
  chart). Tested on K3s, which enforces it; the lab's flannel doesn't.
- **A bug found on the way:** a kubeconfig's config has a dialer (for its
  10-second connect timeout), and exec then took the tunnel path, whose
  upgrade transport had no TLS settings, so a client certificate was never
  sent and the API server answered Unauthorized. Pod shells in local mode
  with certificate kubeconfigs had failed since 0.4.0. The transport now
  takes the config's TLS; a test sends exec to an HTTPS server that checks
  for the certificate.
- **Tested** on the Docker Hub (K3s, mkcert certificate, lab imported) and
  on the lab's Helm Hub: whoami, nodes, `helm list`, `kubectl exec` from
  inside the shell, no service-account token, K3s's API refused by the
  NetworkPolicy, the token refused on another cluster, read-only root,
  uid 1000, and closing the drawer deleting pod, Secret and token.

## Members and projects (Hub phase 5, first step)

- **RBAC only,** no Glowpod store: a cluster member is a ClusterRoleBinding,
  a project member a RoleBinding in each of the project's namespaces, a
  project a namespace label. Works in every mode, not only on a Hub.
- **Made as the person**, through their impersonated clients (through the
  agent on a Hub), so Kubernetes' escalation rules decide what they may
  grant; the UI hides what a SelfSubjectAccessReview says they can't do.
- **Cluster roles are Owner, Read-only and Custom.** The design also had
  "Member" (view, plus edit in projects), but its binding would have been
  identical to Read-only's; project roles carry the difference.
- **Bindings are named by a hash** of the subject (names like
  `oidc:ann@example.com` can't be in a binding's name), with the subject in
  annotations and the role in a label. A role change deletes and recreates
  the binding, since a role ref is immutable. Bindings Glowpod didn't make
  are never changed, even with a matching name.
- **Projects copy, they don't watch:** adding a namespace through Glowpod
  copies the project's bindings; one labelled by hand shows each member as
  "Not in …" with a button to apply it. A controller would need an identity
  of its own in the cluster, which Glowpod avoids.
- **Tested** on the lab through the Docker Hub and the agent: a Read-only
  member (can list pods, can't delete them), a change to a custom `edit`
  role, a project over two namespaces with a group as Member (edit in both,
  nothing in `default`, can't create RoleBindings), a binding deleted with
  kubectl shown as missing and applied again, and deleting the project and
  the member leaving no binding or label behind.
