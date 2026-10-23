# Glowpod

Glowpod is a web dashboard for Kubernetes. It serves two audiences through one UI:
**app developers** (simple "Glance" mode) and **platform/DevOps teams** (full "Deep" mode).
Nothing is ever locked away in Glance mode; it only simplifies what is shown.

- **Web first, many clusters.** Every API route and page URL carries a cluster id (`/api/v1/clusters/{cluster}/…`, `/c/<cluster>/…`).
- Three ways to run it from the same binary:
  - **Local mode:** `glowpod` CLI reads the user's kubeconfig, lists every context as a cluster, and opens the browser.
  - **In-cluster mode:** installed with a Helm chart; users sign in; one cluster.
  - **Hub mode:** works like Rancher (free/MIT): Helm `hub.enabled` (or a Docker quick start with K3s inside), bootstrap password, local users + SSO/LDAP, clusters imported with `kubectl apply` that run `glowpod agent`, members and projects. The agent only impersonates, unlike Rancher's. Design: `docs/design/multi-cluster.md`.
- Sign-in methods: **OIDC SSO**, **service-account token**, **kubeconfig** (local mode only).
- Theme: **dark by default**, light mode available.

Design reference: `docs/design/` (screenshots of the approved boards) and the Glowpod design plan doc.
When a design question is not answered here, follow the screenshots; if they don't answer it either, ask.

---

## Stack

| Layer | Choice |
| --- | --- |
| Backend | Go (latest stable), `k8s.io/client-go` (typed + dynamic clients, discovery), `chi` router, a WebSocket library |
| Frontend | React + TypeScript (strict) + Vite, TanStack Query, TanStack Table + TanStack Virtual, React Router |
| Editors | Monaco (YAML), xterm.js (exec terminal) |
| Icons | Tabler Icons (outline, 1.75 stroke) |
| Styling | CSS variables from the tokens below + CSS modules. **No UI component library.** |
| Tests | `go test` (table-driven), Vitest + Testing Library, Playwright smoke tests |
| Packaging | One Go binary that embeds the built frontend (`embed.FS`); Docker image; Helm chart |

Use current stable versions of all libraries. Don't add a dependency when a few lines of code will do.

## Repo layout

```
backend/
  cmd/glowpod/          # main: CLI flags, local vs in-cluster mode
  internal/api/         # HTTP handlers, routing, middleware
  internal/auth/        # OIDC, token, kubeconfig sessions
  internal/cluster/     # client factory per identity, discovery, dynamic client
  internal/metrics/     # metrics-server + optional Prometheus, gauge math
  internal/stream/      # WebSocket watch, logs, exec
frontend/
  src/app/              # shell: rail, section panel, top bar, routing
  src/components/       # Button, Badge, HalfGauge, Table, ...
  src/features/         # overview, workloads, pod, logs, yaml, nodes, events
  src/lib/              # api client, query keys, websocket helpers
  src/styles/           # tokens.css, base.css
deploy/helm/glowpod/
docs/design/
Makefile                # make dev | build | test | lint
```

---

## Security rules (never break these)

1. **Every Kubernetes call runs as the signed-in user.** Pass the user's OIDC or service-account token through, or use impersonation from a service account whose only right is `impersonate`. Never share one admin identity across users.
2. **Never serve one user's cached data to another.** Informer caches are per identity, or every response is checked with a `SelfSubjectAccessReview`.
3. **Hide actions the user can't do** using `SelfSubjectAccessReview` / `SelfSubjectRulesReview`; still handle a 403 gracefully.
4. **Secret values are hidden by default.** Revealing one needs an explicit click and the right RBAC.
5. Never log tokens, kubeconfigs or Secret data. Use HttpOnly, Secure, SameSite cookies, CSRF protection and a strict Content-Security-Policy.

## Kubernetes API rules

- Use **discovery + the dynamic client** so every resource, including CRDs, works without new code.
- Lists use the API server's **Table output** (`Accept: application/json;as=Table;g=meta.k8s.io;v=v1`) so any resource gets kubectl-style columns.
- Live updates: **watch** on the backend, stream to the browser over WebSocket. On `410 Gone` (resourceVersion too old), relist and resume.
- Large clusters: always paginate with `limit` / `continue`; support label and field selectors; virtualize tables.
- Logs and exec go through backend WebSocket proxies.

---

## Design tokens

Brand colors are exactly **Synthetic Lime `#C8FF00`** and **Bio Black `#06110D`**. Everything else is neutral grey.
Rose `#EF4444` is the only extra hue and is used **only for critical states**.

`frontend/src/styles/tokens.css`:

| Token | Dark (default) | Light | Use |
| --- | --- | --- | --- |
| `--primary` | `#C8FF00` | `#C8FF00` (fill only) | Primary buttons, active rail icon, healthy arcs, live dot |
| `--on-primary` | `#06110D` | `#06110D` | Text and icons on lime |
| `--canvas` | `#06110D` | `#FFFFFF` | Page, rail, panel, top bar |
| `--surface-card` | `#1A1A1A` | `#F5F5F5` | Cards, inputs |
| `--surface-elevated` | `#242424` | `#EBEBEB` | Nested tiles, badges, secondary buttons, gauge track |
| `--hairline` | `#2A2A2A` | `#E0E0E0` | 1px borders, dividers |
| `--hairline-strong` | `#3A3A3A` | `#CCCCCC` | Gauge ticks, input outlines |
| `--ink` | `#FFFFFF` | `#06110D` | Headlines, values |
| `--body` | `#CCCCCC` | `#333333` | Running text, nav items |
| `--muted` | `#888888` | `#666666` | Labels, metadata |
| `--muted-soft` | `#5A5A5A` | `#999999` | Stale/disabled only, never readable text |
| `--state-ok` | `#C8FF00` | `#C8FF00` + `#06110D` outline | Healthy, < 70% |
| `--state-high` | `#FFFFFF` | `#06110D` | 70–84%, pending, waiting |
| `--state-critical` | `#EF4444` | `#EF4444` | ≥ 85%, CrashLoopBackOff, OOMKilled, failed |

Rules:
- Lime is never body text, and never text on a light background.
- In light mode, lime fills (arcs, dots, bars, logo) get a 1–1.5px `#06110D` outline. Links and the active-tab underline are `#06110D`.
- Status is always a **neutral pill + colored dot + word** (plus an icon in lists). Never color alone.
- No shadows, no gradients. Depth = canvas vs card contrast + 1px hairlines.

Type: **Inter** for everything, **JetBrains Mono** for pod/node names, YAML, logs.

| Token | px / weight | Tracking |
| --- | --- | --- |
| page title (top bar) | 20 / 700 | -0.5px |
| display-sm (gauge values, big numbers) | 32 / 700 | -1px |
| title-md (card titles) | 18 / 600 | 0 |
| title-sm (gauge names) | 16 / 600 | 0 |
| body-sm (tables, nav) | 14 / 400–500 | 0 |
| caption (badges, metadata) | 13 / 500 | 0 |
| caption-upper (section labels) | 12 / 600, uppercase | +1.5px |

Radius: 4 badge accents · 6 tab segments · 8 buttons, inputs, nested tiles · 12 cards · full only for badges, avatars, icon buttons.
Spacing: 4px base (4, 8, 12, 16, 24, 32, 48). Buttons are 40px tall. Use tabular numbers for all metrics.

## App shell

- **Icon rail, 64px:** logo, section icons (Deep: Cluster, Workloads, Network, Storage, Config, Extensions; Glance: Overview, Apps, Logs, Events), Settings at the bottom. Active icon = lime square with a Bio Black icon.
- **Section panel, 236px, collapsible into the rail (saved per user):** section title, cluster picker, the section's items with live counts and problem dots, pinned resources. Glance mode lists the user's apps instead.
- **Cluster health card** at the bottom of the panel: CPU, memory and pod mini gauges, plus a "running · pending · failing" line.
- **Minimal top bar, 64px, no boxed controls:** page title, live "updated N s ago", plain-text namespace picker, Glance / Deep text tabs (active = ink + 2px underline), ⌘K search, avatar.

**Overview page:** five gauges (CPU, Memory, Pods, Storage, Network) → workload tiles (Deep) or app cards (Glance) → "Needs attention" + "Recent events" → node load + top consumers (Deep) or recent deploys (Glance).

## HalfGauge component (the signature element)

```tsx
<HalfGauge
  label="CPU" value={24.8} max={40} unit="cores"
  requested={30}                 // optional thin inner arc (CPU, memory)
  thresholds={[0.7, 0.85]}       // editable per cluster
  size="L"                       // L 220px | M 140px | S 64px
  trend={0.04}                   // change over the last hour
  state="live"                   // loading | live | stale | no-metrics | error
/>
```

- Plain SVG, no chart library: a 180° arc, left = 0%, round caps, `stroke-dasharray` for the value.
- Track `--surface-elevated`; value arc colored by zone (ok / high / critical tokens); thin inner arc for requests in `--muted`; tick marks at 70% and 85%; a round knob at the arc's end.
- Center: value (display-sm), "used of total" in `--muted`. Header: name + status badge. Footer: requested share and trend.
- States: **loading** = track + skeleton, no spinner · **stale** = muted arc + "updated N min ago" · **no-metrics** = dotted track + "Install metrics-server" link · **error** = dotted track + Retry button.
- Motion: sweep in over 600 ms on first load, ease over 300 ms on updates, none with `prefers-reduced-motion`.
- Accessibility: `role="meter"`, `aria-valuenow/min/max`, `aria-valuetext` such as "CPU 62 percent, OK".

Gauge data:

| Gauge | Value | 100% | Source |
| --- | --- | --- | --- |
| CPU | cores in use (inner arc: requested) | allocatable cores, all nodes | metrics.k8s.io + node status + pod specs |
| Memory | bytes in use (inner arc: requested) | allocatable memory | same |
| Pods | running pods | sum of node pod capacity | pod list + node status |
| Storage | used bytes on PVs | total PVC capacity | kubelet stats summary, or Prometheus |
| Network | bytes/s in + out | 7-day peak | Prometheus only. **Hide the gauge when Prometheus isn't found.** |

Refresh every 15 s; pause while the browser tab is hidden.

---

## Build phases

1. **Foundation + Overview (local mode):** repo skeleton, Makefile, tokens and base components, kubeconfig connection, app shell (rail, panel, top bar), Overview with real gauges, workload tiles, needs-attention list, live events. Glance / Deep toggle works.
2. **Workloads:** generic resource lists (Table output, pagination, filters), pod detail with status timeline, live logs, read-only YAML view.
3. **Changing things:** YAML edit with diff and server-side dry-run before apply, scale, restart, delete with name confirmation, exec terminal.
4. **More sections:** Nodes (cordon, drain), Network, Storage, Config, Events page, generic CRD views, resource map.
5. **In-cluster mode:** OIDC and token sign-in, Helm chart, RBAC-aware UI everywhere.
6. **Polish:** light theme, settings (thresholds, density, refresh), empty and error states, responsive layouts.

Then Glowpod Hub (`docs/design/multi-cluster.md`):

7. **Many clusters in local mode:** cluster registry, Clusters page, real cluster picker, every kubeconfig context. ✅
8. **Hub and agent:** Helm `hub.enabled`, bootstrap password, local users, `kubectl apply` import, outbound tunnel, impersonation guards, `local` cluster, cluster list from RBAC subjects. ✅
9. **Docker quick start:** `glowpod-hub` image with K3s inside.
10. **Cluster shell and kubeconfig download.**
11. **Members, projects, Hub settings** (SSO/LDAP in the UI), backup and restore.
12. **Across clusters:** search and "Needs attention" over all clusters.

Finish and test each phase before starting the next.

## Conventions

- Go: `gofmt`, `golangci-lint`, `context.Context` on every call, wrapped errors, table-driven tests for every handler.
- TypeScript: strict mode, no `any`, ESLint + Prettier. Query keys in one place.
- Accessibility: real `<button>` and `<a>`, labels on inputs, `aria-label` on icon-only buttons, full keyboard support, ⌘K command palette, WCAG 2.2 AA in both themes.
- Copy: short, plain, kind; keep the Kubernetes term in brackets, e.g. "3 pods keep restarting (CrashLoopBackOff)".
- Small commits with clear messages. Run `make lint test` before saying a task is done.
