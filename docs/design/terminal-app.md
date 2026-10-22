# Glowpod in the terminal

`glowpod term` opens Glowpod as a full-screen terminal app. It is the same
product as the web dashboard, not a second one: Glance first, Deep one key
away, the same plain words, the same colours, the same rule that every call
runs as you. It is free and MIT, part of the open-source core.

People who know k9s should feel at home in Deep mode (`:` commands, `/`
filter, `l` logs, `y` YAML). Everything else is Glowpod's own.

## What we took from k9s, and what we didn't

We studied k9s 0.51.0 (Apache-2.0) by reading its source and by running the
Homebrew binary against the lab cluster in read-only mode, using REA to
record the screens. No k9s code is in Glowpod.

Worth matching, because people already have it in their fingers:

- `:` to jump to any resource by name, short name or CRD (`:po`, `:deploy`,
  `:certificates`), with completion as you type.
- `/` to filter the table as you type; `/-l app=web` for a label selector.
- `l` logs, `y` YAML, `s` shell (pods) or scale (workloads),
  `r` restart, `ctrl+d` delete, `Enter` to drill down, `Esc` to go back.
- Live tables that update without a refresh key.
- `?` shows every key for the current screen.

Where Glowpod differs on purpose:

| k9s | Glowpod |
| --- | --- |
| Starts on a pods table | Starts on **Glance**: half-gauges, what needs attention in plain words, recent events |
| Hides write keys only in global `--readonly` mode; you find out about RBAC from a 403 | Hides each action unless a **SelfSubjectAccessReview** allows it, per resource and namespace |
| Hand-written columns per resource type | The API server's **Table output** for every type, CRDs included (same columns as `kubectl get`) |
| Skins, many colours, colour alone means state | One palette; status is always **dot + word** |
| Delete is a yes/no dialog | Delete asks you to **type the name** |
| Needs a kubeconfig on every laptop | Later: **sign in** to an in-cluster Glowpod with SSO, LDAP or a token |
| Logs are plain text | Log lines coloured by level: **orange** warnings, **rose** errors |

## Screens

### Glance (start screen)

```
 glowpod  lab · v1.36.5 · 3 nodes          all namespaces   Glance  Deep    ? help
 ● updated 4 s ago
╭ CPU ──────── ● OK ╮╭ Memory ───── ● OK ╮╭ Pods ─────── ● OK ╮╭ Storage ── ● OK ╮
│    ⣠⣴⣶⣶⣶⣶⣦⣄      ││    ⣠⣴⣶⣶⣶⣦⣄       ││    ⣠⣴⣶⣦⣄          ││   ⣠⣴⣦⣄            │
│  ⣰⣿⠟      ⠻⣿⣆    ││  ⣰⣿⠟      ⠙⢷⣆   ││  ⣰⣿⠟      ⠙⢷⣆   ││ ⣰⡿⠋     ⠙⢷⣆    │
│  ⣿⡏   21%   ⢹⣿   ││  ⣿⡏   57%   ⢹⡇   ││  ⣿⡏   12%   ⢹⡇   ││ ⣿⡇  31%   ⢸⡇    │
│   1.3 of 6 cores  ││ 4.9 of 8.6 GiB    ││  41 of 330 pods   ││ 1.2 of 3.9 GiB  │
╰───────────────────╯╰───────────────────╯╰───────────────────╯╰─────────────────╯
 NEEDS ATTENTION · 2
 ● Critical  Keeps restarting (CrashLoopBackOff)   payments-api-7d9f6b-x2kq9   23m
             payments · restarted 14 times · exit code 1
 ● High      Waiting to start (Pending)            search-indexer-0             6m
 RECENT EVENTS                                                              ● live
 ⚠ BackOff restarting container          payments-api-7d9f6b-x2kq9          1m
 · Scaled up to 4 replicas                deploy/checkout-web                3m
```

- **Half-gauges** drawn with Braille dots: a 180° arc, track in
  `--surface-elevated` grey, value in the zone colour (lime / white / rose),
  ticks at 70 % and 85 %. Value in the middle, "used of total" under it,
  status dot + word in the header. Gauges that can't be read say why ("No
  metrics: install metrics-server", "No access"). Network shows only when
  Prometheus is found, like the web.
- **Needs attention** uses the same problem list as the web (the
  `summary` package): plain title first, Kubernetes term in brackets, the
  name in mono, a short detail line. `Enter` opens the pod or workload.
- **Recent events**, newest first, live.
- Narrow terminals stack the gauges 2 × 2, then drop to one-line bars
  (`CPU ███████░░░░ 62% OK`) under 60 columns.
- Refreshes every 15 s; data comes from the same `overview` package as the
  web Overview.

### Deep (resource tables)

`Tab` switches between Glance and Deep. Deep remembers its last table
(pods to start with).

```
 glowpod  Deep · pods · monitoring          monitoring ▾    Glance  Deep    ? help
 / prom                                                              6 of 6 · live
 NAME                                               READY  STATUS   RESTARTS  AGE
 ▌prometheus-kube-prometheus-operator-5b5c76dc59…   1/1    ● Running       0  17h
  prometheus-kube-state-metrics-6b59d8c97f-whfbf    1/1    ● Running       0  17h
 ─────────────────────────────────────────────────────────────────────────────────
 Enter open  l logs  y YAML  s shell  ctrl+d delete  / filter  : command  ? help
```

- Columns come from the API server's Table output (priority-0 columns;
  `w` adds the wide ones), so any resource and any CRD works with no new code.
- Pages of 500 rows with `continue`; the list re-reads when a watch says
  something changed (debounced), and relists after `410 Gone`.
- `/` filters rows as you type (name and cells, case-insensitive);
  `/-l key=value` sends a label selector to the API server.
- Status cells get a dot + the word: lime for Running/Ready/Succeeded,
  white for Pending/waiting, rose for CrashLoopBackOff/Error/OOMKilled/Failed.
- The footer lists only the keys you can use on this row (see RBAC).

### Pod

`Enter` on a pod (or on a problem in Glance) opens it.

```
 glowpod  pod · payments/payments-api-7d9f6b-x2kq9            ● Critical CrashLoopBackOff
 Logs   Events   YAML                                     container: api ▾  following
 10:02:11.120  starting server on :8080
 10:02:11.410  WARN  cache miss rate high                 ← orange
 10:02:12.002  ERROR db: connection refused               ← rose
```

- **Logs**: follows the current run (`p` for the previous run), container
  picker `c`, `/` highlights matches, `t` timestamps, `w` wrap, `G` back to the
  live end. Warnings orange `#FB923C`, errors rose `#EF4444`, using the same
  rules as the web log viewer (stated level first: JSON, logfmt, klog; then
  words). This is the only place orange is used (see `docs/decisions.md`).
- **Events** for this pod, newest first.
- **YAML**, read-only, Secrets redacted unless revealed (and allowed).
- **Shell** (`s`): the terminal hands itself to `kubectl exec`-style
  streaming through the API server and comes back when you exit.

Workloads (Deployment, StatefulSet, DaemonSet, Job) open to their pods, plus
YAML and events.

## Keys

Global:

| Key | Does |
| --- | --- |
| `Tab` | Glance ↔ Deep |
| `:` or `ctrl+k` | Command palette |
| `/` | Filter the current list |
| `n` | Namespace picker (all, one, or type a name) |
| `Esc` | Back / clear filter |
| `?` | Help for this screen |
| `q` | Back; quits from the top screen |
| `ctrl+c` | Quit |

Lists: `↑↓` or `j k`, `g G`, `PgUp PgDn`, `Enter` open, `y` YAML, `l` logs,
`w` wide columns.

Actions, only shown when allowed: `s` scale (workloads) or shell (pods),
`r` restart, `ctrl+d` delete.

## Command palette

`:` opens one prompt with suggestions under it, fuzzy-matched:

- **Resources:** every type discovery returns, by plural, singular, kind or
  short name, CRDs included: `:po`, `:deploy`, `:svc`, `:applications`.
  `:pods kube-system` also sets the namespace.
- **Namespaces:** `:ns monitoring`, `:ns all`.
- **Screens:** `:glance`, `:deep`, `:help`, `:quit`.
- **Actions on the selected row:** `:logs`, `:yaml`, `:scale 3`, `:restart`,
  `:delete`, listed only when allowed.

`Tab` completes, `↑↓` picks, `Enter` runs, `Esc` closes.

## How it connects

**Now (local mode).** `glowpod term` reads your kubeconfig the way kubectl
does (`-kubeconfig`, `-context`, `$KUBECONFIG`, `~/.kube/config`) and calls
the API server as that user, through the same `internal/cluster`,
`resources`, `overview`, `logs`, `metrics` and `terminal` packages the web
server uses. No server, no port, no browser.

**Later (sign in).** `glowpod term --server https://glowpod.example.com`
signs in to an in-cluster Glowpod with SSO, LDAP or a service-account token
(a browser sign-in that hands a short-lived session back to the terminal,
like `gh auth login`). The terminal then talks to Glowpod's HTTP API, and
Glowpod calls Kubernetes as that person by impersonation or token
pass-through. Nobody needs a kubeconfig; access is the RBAC the platform team
already set up, and turning someone off is one change in the identity
provider. This is the main thing k9s can't do.

To make that a small step, the terminal app reads data through one Go
interface (`Source`) whose methods return the same shapes as the HTTP API
(`overview.Response`, `resources.Table`, log lines, access checks). Local
mode implements it with the internal packages; server mode will implement it
with an HTTP client.

## RBAC

- Before showing an action, the app asks the API server with a
  `SelfSubjectAccessReview` (verb, group, resource, subresource, namespace,
  name), the same checks as the web: `patch deployments/scale` for scale,
  `patch` for restart, `delete` for delete, `create pods/exec` for shell,
  `get pods/log` for logs.
- Answers are cached per namespace and resource for 30 s; switching
  namespace re-checks.
- A hidden action is gone from the footer, `?` help and the palette, not
  greyed out.
- A 403 on the real call is still shown in plain words with the cluster's
  reason, never as a crash.
- Delete asks you to type the object's name. Scale asks for the number.
  Restart asks once.

## Look

- Background **Bio Black `#06110D`** (set as the terminal's background while
  Glowpod runs, restored on exit). Text `#CCCCCC`, headings white, labels
  `#888888`.
- **Synthetic Lime `#C8FF00`** for OK, healthy arcs, the live dot and the
  selected row's marker. Never for running text.
- **White** for high (70–84 %, pending, waiting). **Rose `#EF4444`** only for
  critical (≥ 85 %, CrashLoopBackOff, OOMKilled, failed).
- Status is always **● + word** (plus `⚠`/`✕` icons in lists), never colour
  alone, so it still reads with `NO_COLOR` or a colour-blind palette.
- Hairline borders in `#2A2A2A`, rounded, no heavy boxes. Pod and node names
  in the terminal's font (it is already mono).
- Lip Gloss picks the best the terminal supports (true colour → 256 → 16),
  and `NO_COLOR` turns colour off. Light theme comes with the web's Phase 6
  settings.

## Build

- Go, **Bubble Tea v2**, **Lip Gloss v2**, **Bubbles v2** (current stable),
  in `backend/internal/tui`. `glowpod term` is a subcommand of the existing
  binary; plain `glowpod` and its flags don't change.
- First milestone: Glance, `:pods` / `:deploy` / any resource with namespace
  switching and `/` filter, pod logs.
- Then: YAML view, RBAC-aware scale, restart and delete (typed name), shell.
- Later: sign-in to an in-cluster Glowpod, light theme, port-forward,
  node cordon/drain, edit YAML with a dry-run diff.
