# Glowpod design plan

Oct 22, 2026 · @luka

Glowpod launches as a web app for one cluster, serving developers and platform teams through one UI with a simple/advanced toggle. These decisions shape every section below.

| Decision | Choice |
| --- | --- |
| Platform | Web first: in-cluster (Helm) and local (CLI opens the browser); desktop wrapper later |
| Audience | Developers and platform/DevOps teams; nothing is locked away from anyone |
| Modes | Toggle between Glance (simple, app-focused) and Deep (every resource and field) |
| Clusters | One cluster in v1; layout keeps room for a cluster switcher |
| Sign-in | OIDC SSO, service-account token, kubeconfig |
| Theme | Dark by default, light mode available |
| Mascot | Branding only (site, docs, social, app icon), never inside the app UI |
| Overview gauges | CPU, memory, pod capacity, storage; network only when Prometheus is found |

## Brand foundation

Glowpod makes a cluster easy to read: healthy things glow lime, problems stand out, and nothing is hidden from people who want depth. The brand is cartoon; the product UI is calm and quiet.

- **Name story:** pods that glow. Bioluminescence stands for visibility: the cluster lights up instead of hiding in kubectl output.
- **Mascot, Pip:** a lime, bean-shaped pod with a thick Bio Black outline, big eyes and a sprout antenna. Pip lives in branding only: website, docs, README, social posts, stickers, release notes. Never inside the app.
- **Cartoon in the product = shape language only:** the pod-shaped logo mark, soft 8–12px corners and half-round gauges; headings stay in bold Inter. No characters, no playful animation, no jokes in data views.
- **Personality:** friendly, plain, calm under pressure.
- **Domains:** glowpod.dev is the main site; glowpod.io redirects to it.

| Situation | Write | Avoid |
| --- | --- | --- |
| Pods crashing | 3 pods keep restarting in payments (CrashLoopBackOff) | ERROR: CrashLoopBackOff detected |
| All healthy | All 42 nodes look healthy | No issues found!!! |
| No permission | You can't view secrets in this namespace. Ask an admin for access. | 403 Forbidden |
| Empty namespace | Nothing growing here yet. Deploy something? | No resources |

The voice is short, plain and kind, but keeps the real Kubernetes term in brackets so experts still recognise it.

## Color system and tokens

Glowpod uses exactly two brand colors, Synthetic Lime #C8FF00 and Bio Black #06110D, on the neutral grey structure of the reference design system. Lime on Bio Black has about 16:1 contrast; lime on white about 1.2:1, so in light mode lime is only ever a fill.

| Token | Dark (default) | Light | Use |
| --- | --- | --- | --- |
| primary | #C8FF00 | #C8FF00 (fill only) | Primary buttons, active nav icon, healthy gauge arc, live dot, links in dark mode |
| on-primary | #06110D | #06110D | Text and icons on lime |
| canvas | #06110D | #FFFFFF | Page, sidebar, top bar |
| surface-card | #1A1A1A | #F5F5F5 | Cards, inputs, segmented toggle |
| surface-elevated | #242424 | #EBEBEB | Nested tiles, badges, secondary buttons, gauge track |
| hairline | #2A2A2A | #E0E0E0 | 1px card borders and dividers |
| hairline-strong | #3A3A3A | #CCCCCC | Gauge ticks, input outlines |
| ink | #FFFFFF | #06110D | Headlines, values; links in light mode (underlined) |
| body | #CCCCCC | #333333 | Running text, nav items |
| muted | #888888 | #666666 | Labels, metadata, timestamps |
| muted-soft | #5A5A5A | #999999 | Stale gauges, disabled; never readable text |
| state.ok | #C8FF00 | #C8FF00 with a #06110D ring | Healthy, under 70% |
| state.high | #FFFFFF | #06110D | 70–84% load, pending, waiting |
| state.critical | #EF4444 | #EF4444 | 85%+ load, CrashLoopBackOff, OOMKilled, failed |

Rules:

- Two brand colors only. Everything else is neutral grey; rose #EF4444, the reference system's error accent, is the single exception and appears only for critical states.
- Lime stays scarce: primary action, healthy state, live dot, active rail icon. Never body text or large fills inside the app.
- Status is a neutral pill with a colored dot and a word (plus an icon in lists), never color alone.
- Light mode: lime on white gets a 1–1.5px Bio Black outline (gauge arcs, dots, bars, logo). The High state, links and the active tab underline use Bio Black.
- No shadows or gradients. Depth comes from canvas vs card contrast and 1px hairlines.

## Typography, icons and shape

One family, Inter, carries everything, with JetBrains Mono for anything Kubernetes prints. Hierarchy comes from size and weight; bold headings use tight negative tracking.

| Token | Size / weight (px) | Tracking (px) | Use |
| --- | --- | --- | --- |
| display-md | 40 / 700 | -1.5 | Marketing site section heads |
| display-sm | 32 / 700 | -1 | Page titles (Overview), gauge values, big stat numbers |
| title-md | 18 / 600 | 0 | Card titles (Needs attention, Node load) |
| title-sm | 16 / 600 | 0 | Gauge names, workload tile titles |
| body-md | 16 / 400 | 0 | Running text |
| body-sm | 14 / 400–500 | 0 | Table text, nav items, inputs |
| caption | 13 / 500 | 0 | Badges, metadata, timestamps |
| caption-upper | 12 / 600, uppercase | +1.5 | Section labels |
| button | 14 / 600 | 0 | All buttons |
| code | 13–14 / 400–500, JetBrains Mono | 0 | Pod and node names, YAML, logs, terminal |

- **Radius:** 4 for badge accents, 6 for toggle segments and small buttons, 8 for buttons, inputs and nested tiles, 12 for cards. Fully round only for badges, avatars and icon buttons.
- **Buttons:** 40px tall. Primary = lime fill with Bio Black text; secondary = #242424 with white text.
- **Spacing:** 4px base, using 4, 8, 12, 16, 24, 32, 48. Card padding 24px (20px on gauge cards).
- **Icons:** Tabler Icons, outline, 1.75 stroke, 18px in nav and 16px inline.
- **Density:** 40px comfortable and 32px compact table rows, switched in settings.

## Main dashboard layout

The overview answers one question in two seconds: is the cluster okay, and if not, where? Gauges come first, the list of problems second, detail last.

&#91;embedded content: Overview page wireframe · desktop, Deep mode\]

A 64px icon rail and a 236px section panel stay fixed on the left; the panel folds into the rail with one click and the choice is saved per user. The minimal top bar holds only what every page needs. The Glance / Deep toggle changes the rail, the panel and rows 2 to 4, but never the gauges.

| Region | Glance mode (simple) | Deep mode (advanced) |
| --- | --- | --- |
| Rail (64px) | Overview, Apps, Logs, Events, Settings | Cluster, Workloads, Network, Storage, Config, Extensions (CRDs, Helm, plugins), Settings |
| Section panel (236px, collapsible) | Cluster picker and your apps with status dots | Cluster picker, the chosen section's items with live counts and problem dots, pinned resources |
| Panel footer | Cluster health card: CPU, memory and pod mini gauges, plus running / pending / failing counts | Same |
| Top bar (minimal, no boxes) | Page title, live update time, namespace picker, Glance / Deep text tabs, ⌘K search, avatar | Same |
| Gauges | CPU, memory, pods, storage, network\* | Same |
| Row 2 | App cards: one per app (by `app.kubernetes.io/name` label or Helm release) with status, replicas, last deploy | Workload counts by kind, ready vs total |
| Needs attention | Problems in plain language with a suggested fix | Same, plus reason codes, node pressure, failed jobs |
| Bottom row | Recent deploys | Node load and top consumers |

Nothing is locked in Glance mode: any card has a "Show in Deep" link, and the choice is saved per user.

## Half-round load gauges

Every overview gauge is a 180° arc with round caps: a thick arc for real usage and a thin inner arc for what pods have reserved. The arc takes the color of the zone its value is in, never a rainbow.

&#91;embedded content: Gauge anatomy · example values\]

In the example, CPU is 62% used but pods have requested 75%, so new pods will run out of room before the CPU runs hot. Showing both is what makes this gauge more useful than a plain percentage.

### What each gauge measures

| Gauge | Value | 100% means | Source | Shown when |
| --- | --- | --- | --- | --- |
| CPU | Cores in use (inner arc: requested cores) | Allocatable cores on all nodes | metrics-server (`metrics.k8s.io`) + node status | metrics-server is installed |
| Memory | Memory in use (inner arc: requested memory) | Allocatable memory | metrics-server + node status | metrics-server is installed |
| Pods | Running pods | Sum of each node's pod capacity | Pod list + node status | Always |
| Storage | Used space on persistent volumes | Total PVC capacity | kubelet stats summary, or Prometheus | Always (capacity only if usage stats are missing) |
| Network | Traffic in + out, per second | No fixed max: the arc scales to the 7-day peak | Prometheus (`container_network_*` metrics) | Prometheus is found; otherwise the gauge is not rendered |

### Zones

| Zone | Range | Arc color | Word shown |
| --- | --- | --- | --- |
| Healthy | 0–69% | Lime #C8FF00 | OK |
| Warning | 70–84% | White #FFFFFF | High |
| Critical | 85–100% | Rose #EF4444 | Critical |

Thresholds are editable per cluster in settings. The word appears in the tooltip and screen-reader text, so color is never the only signal.

### Sizes

| Size | Width (px) | Arc stroke (px) | Value text (px) | Used in |
| --- | --- | --- | --- | --- |
| L | 220 | 22 | 34 | Overview gauge row |
| M | 140 | 14 | 22 | Node cards, namespace pages |
| S | 64 | 7 | 13, beside the arc | Table cells, node list |

### States

| State | Look |
| --- | --- |
| Loading | Grey track only; a short skeleton bar replaces the value. No spinner |
| Live | Arc sweeps from 0 to the value in 600 ms on first load; later updates ease over 300 ms |
| Stale | Arc and value turn muted, with "updated 3 min ago" under the value |
| No metrics | Dashed track and "Install metrics-server to see usage" with a docs link |
| Error | Dashed track, "Couldn't load" and a Retry button |

With reduced motion turned on, values change without animation.

### Behavior

- Refresh every 15 seconds; pause while the browser tab is hidden.
- Hover shows a tooltip: usage, requests, limits, allocatable and the top 3 namespaces.
- Click opens the Nodes page sorted by that metric.
- Accessibility: `role="meter"` with `aria-valuenow`, `aria-valuemin`, `aria-valuemax` and text such as "CPU 62 percent, OK".

### Component API

```tsx
<HalfGauge
  label="CPU"
  value={24.8}
  max={40}
  unit="cores"
  requested={30}
  thresholds={[0.7, 0.85]}
  size="L"
  trend={0.04}
  state="live"
/>
```

Build it with plain SVG: two arc paths, `stroke-dasharray` for the value and `stroke-linecap: round`. No chart library is needed.

## Other key screens

Twelve screens cover v1. Glance and Deep share every screen; Deep adds fields and actions rather than different pages.

| Screen | Job | Glance mode | Deep mode adds |
| --- | --- | --- | --- |
| Sign in | Get into the cluster | OIDC button first, then token, then kubeconfig upload (local mode) | Context picker when a kubeconfig has several |
| Overview | Is the cluster okay? | Gauges, app cards, problems, recent deploys | Workload counts, node load map, top consumers |
| Apps / Workloads | Find and check what runs | Apps grouped by label or Helm release | Lists per kind (Deployments, StatefulSets, DaemonSets, Jobs, CronJobs) with filters and bulk actions |
| Pod detail | Why is this pod unhappy? | Status story: what happened, in order, with a suggested fix | Containers, conditions, probes, volumes, resource usage vs limits (S gauges) |
| Logs | Read what the app says | Live tail, search, follow toggle | Multi-container and multi-pod merge, previous container, timestamps, download |
| Terminal | Run commands inside a container | Exec with shell picker | Same; ephemeral debug container |
| YAML editor | See and change the real object | Read-only by default, "Edit" opens Monaco | Diff before apply, schema validation, managed-fields hidden |
| Nodes | Is any machine struggling? | Node cards with M gauges | Taints, labels, conditions, cordon / drain |
| Network | How traffic reaches apps | Services and Ingress with their URLs | Endpoints, NetworkPolicies, Gateway API |
| Events | What just happened? | Warnings in plain language | Full stream with filters by kind, reason, object |
| Resource map | How things connect | Deployment → ReplicaSet → Pods → Service → Ingress graph | Same, plus ConfigMaps, Secrets, PVCs |
| Settings | Make it yours | Theme, mode, density, language | Gauge thresholds, refresh rate, plugins, cluster connection |

CRDs get an automatic generic list and detail view in Deep mode, built from the API's table output, so custom resources work on day one.

## Component library

About 25 components cover every screen above. Design them in this order, because each group builds on the one before.

| Group | Components | Notes |
| --- | --- | --- |
| Foundations | Color tokens, type styles, icons, spacing, radius, focus ring | Dark and light variants for every token |
| Basics | Button (primary, secondary, ghost, danger), icon button, input, select, checkbox, switch, tooltip | Primary button = lime fill, Bio Black text |
| Status | Status chip, status dot, severity badge, empty-state block, inline alert | Every status = icon + word + color |
| Data display | Half-round gauge (L, M, S), stat tile, sparkline, app card, node card, resource table with sticky header | Tables virtualized for 10,000+ rows |
| Navigation | Icon rail, collapsible section panel, cluster health card, minimal top bar, Glance / Deep text tabs, namespace picker, command palette (Cmd+K) | Active tab = white text + 2px lime underline (Bio Black in light mode) |
| Overlays | Dialog, drawer, popover, menu, toast | Destructive actions confirm with the resource name |
| Code | YAML editor (Monaco theme), log viewer, terminal (xterm.js theme), diff view | One shared code theme built from the tokens |
| Graph | Resource map node, edge, group | Same rounded shapes as cards |

## Motion, loading, empty and error states

Motion only explains a change; it never decorates. No mascot, confetti, bounces or looping animations anywhere in the app.

- **Durations:** 150 ms for hover and press, 250 ms for panels and drawers, 600 ms only for the first gauge sweep. Ease-out curves.
- **Live updates:** values tween quietly; new table rows fade in once. Nothing flashes or blinks, even for critical states.
- **Loading:** skeleton blocks in `surface-elevated` with the final layout's shape. No spinners except inside buttons.
- **Reduced motion:** all transitions become instant.

| State | What the user sees |
| --- | --- |
| Empty namespace | Tabler icon, "Nothing deployed here yet", buttons "Apply YAML" and "Read the docs" |
| No permission (RBAC) | Lock icon, "You can't view secrets in this namespace", the exact permission needed, "Copy request for admin" |
| No metrics-server | Gauges show the No metrics state; one banner explains how to install it |
| Cluster unreachable | Full-width banner with last contact time, auto-retry countdown, "Retry now" |
| Session expired | Dialog: "Sign in again"; unsaved YAML edits are kept |
| 404 | Plain page with search box and link to Overview |

## Accessibility and responsive rules

Target WCAG 2.2 AA in both themes, and design desktop first: people debug clusters on laptops, while phones are for checking status.

- **Contrast:** body text at least 4.5:1, large numbers and icons at least 3:1. Lime is never text on light backgrounds.
- **Keyboard:** every action reachable by keyboard; Cmd+K opens the command palette; `/` focuses search; arrow keys move through tables.
- **Focus ring:** 2px lime with a 2px gap in dark mode, 2px Bio Black in light mode.
- **Screen readers:** gauges use `role="meter"`; status chips read their word; live regions announce new critical problems once.

| Width | Layout |
| --- | --- |
| 1440px and up | Rail + open panel; five gauges in one row; two-column rows below |
| 1024–1439px | Panel folds into the rail; gauges stay in one row at a smaller L size |
| 768–1023px | Rail and panel become a drawer; gauges in rows of 2 or 3 |
| Under 768px | One column, M gauges, bottom tab bar; read-mostly (no terminal or YAML editing) |

## Design prompt

Paste this into an AI design tool (Claude, v0, Figma AI, Lovable) or hand it to a designer. It asks for the overview screen first; change the last block to request other screens.

```text
You are a senior product designer. Design the main Overview screen of Glowpod, a web dashboard for Kubernetes.

PRODUCT
- Glowpod lets developers and platform/DevOps teams see and manage one Kubernetes cluster in the browser.
- Two modes, switched by a segmented toggle in the top bar: "Glance" (simple, app-focused, plain language) and "Deep" (every resource and field). Nothing is locked in Glance mode.
- Design this screen in Deep mode, dark theme, at 1440 x 900.

COLORS (exactly two brand colors, nothing else colorful)
- Synthetic Lime #C8FF00: primary buttons, active nav icon, healthy gauge arcs, live dot, links. Text on lime is always #06110D.
- Bio Black #06110D: page canvas, sidebar, top bar.
- Neutral greys only for structure: cards #1A1A1A, nested tiles and badges #242424, hairline borders #2A2A2A, strong hairline #3A3A3A.
- Text: headlines and values #FFFFFF, body #CCCCCC, labels #888888.
- Status: OK = lime, High = white, Critical = rose #EF4444 (the only extra hue, critical states only). Status badges are #242424 pills with a 7px colored dot and a word.
- No gradients, no glow, no shadows. Depth comes from canvas vs card contrast and 1px hairlines.

TYPE AND SHAPE
- Inter for everything: page title 32px/700 with -1px tracking, card titles 18px/600, labels 16px/600, body 14px, captions 13px/500. Gauge values 32px/700, -1px tracking.
- JetBrains Mono for pod names, node names and namespaces in lists.
- Radius: buttons and inputs 8px (40px tall), cards 12px, nested tiles 8px, pills only for badges.
- Icons: Tabler Icons outline, 1.75 stroke.

LAYOUT
- Icon rail 64px on Bio Black with a hairline border: lime pod-shaped logo at top, 40px icon buttons for Cluster (active: lime square, Bio Black icon), Workloads, Network, Storage, Config, Extensions; Settings at the bottom.
- Section panel 236px beside the rail, collapsible: title "Cluster" with a collapse chevron, cluster picker ("prod-eu-1" with a lime dot), items Overview (active, #1A1A1A), Nodes ("1 hot · 6" with rose dot), Namespaces 5, Events 3 (rose dot), a PINNED group with two pod names in mono. At the bottom, a "Cluster health" card: three mini half-round gauges (CPU 62%, Memory 78%, Pods 45%) and a line "290 running · 4 pending · 3 failing" with dots.
- Minimal top bar 64px, no boxed controls: page title "Overview" (Inter 20/700), lime dot + "updated 12 s ago", then on the right plain-text "All namespaces" dropdown, a hairline divider, Glance / Deep as text tabs (active: white + 2px lime underline), a divider, search icon with ⌘K, avatar.
- Row 1: five half-round load gauges in equal cards: CPU, Memory, Pods, Storage, Network.
- Row 2: four workload tiles: Deployments, StatefulSets, DaemonSets, Jobs, each with a big number, ready/total and a thin segmented status bar.
- Row 3: "Needs attention" list (2/3 width) and "Recent events" stream (1/3 width).
- Row 4: "Node load" (one tile per node with a small gauge for CPU and a bar for memory) and "Top consumers" table.

HALF-ROUND GAUGES (the hero of this screen)
- 180 degree arc, left = 0%, right = 100%, round caps, 18px thick. Track #242424.
- Value arc color by zone: under 70% lime, 70-84% white, 85%+ rose.
- For CPU and Memory, a thin 4px #888888 inner arc shows the share requested by pods.
- Small #3A3A3A tick marks outside the arc at 70% and 85%.
- A round knob at the end of the value arc in the zone color with a 3px #06110D outline.
- Inside the arc: value ("62%"), and used of total in #888888 ("24.8 of 40 cores"). Badge with dot + word in the card header.
- Footer: requested share and trend ("+4% in 1h").
- Example values: CPU 62% (75% requested), Memory 78% (High), Pods 45%, Storage 31%, Network 120 MB/s at 60% of its 7-day peak.

CONTENT
- Realistic data: 6 nodes, namespaces payments, checkout, search, monitoring, kube-system.
- Needs attention: 2 pods CrashLoopBackOff in payments, 1 OOMKilled in checkout, 1 Pending (no node has enough memory) in search. Each row: status badge, pod name in mono, namespace and reason, age, secondary "View" button.

DELIVER
- The Overview screen in dark theme, then in light theme (canvas #FFFFFF, cards #F5F5F5, text #06110D, lime only as fills with a thin #06110D outline, High state, links and the active tab underline in #06110D).
- A component sheet with the gauge in L, M and S sizes and in its Loading, Live, Stale, No metrics and Error states.
```

## Design roadmap

Design in five phases, each closed by a gate. The overview and its gauges come right after the foundations, because every later screen reuses their cards, colors and states.

&#91;embedded content: Design phases · 5 phases, 5 gates\]

Coding can start after gate 2: the overview and gauge component are enough to build the real data layer against, while phases 3 to 5 continue in parallel.
