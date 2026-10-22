# Glowpod Hub: many clusters, one sign-in

Today Glowpod shows one cluster: the one in your kubeconfig (local mode) or
the one it is installed in (in-cluster mode). **Glowpod Hub** is a third way
to run the same binary. You install it once, add each cluster with one
command, and everyone signs in at one address. After signing in, people see
the clusters they have access to, open one, and get everything Glowpod has
for it, plus a shell with `kubectl` in the browser.

This is the role Rancher plays. What carries over from Rancher and what
doesn't:

| Rancher | Glowpod Hub |
| --- | --- |
| Rancher server + an agent in each cluster that dials out | Same shape: `glowpod hub` + `glowpod agent`, and the agent dials out |
| Agent runs as cluster-admin | Agent may only **impersonate** and read RBAC bindings. Every call runs as the signed-in person (security rule 1) |
| Its own users, roles and projects next to Kubernetes RBAC | **Kubernetes RBAC is the only permission system.** Who sees a cluster comes from its bindings |
| Stores its state in its own CRDs plus etcd snapshots | State is a few CRDs and Secrets in the hub's namespace. No database |
| Kubectl shell in a pod, using a Rancher token | Same idea: a shell pod whose kubeconfig points back at the hub with a short-lived token for that person |
| Creates clusters (RKE2, cloud providers) | **Not a goal.** Glowpod adds clusters that already exist |
| Fleet (GitOps), monitoring, catalogs | Not a goal. Helm releases stay a read view |

## Three ways to run, and how they relate

| Mode | Clusters | Sign-in | Who it's for |
| --- | --- | --- | --- |
| Local (`glowpod`) | **Every context in your kubeconfig**, not just the current one | Your kubeconfig | One person on a laptop |
| In-cluster (Helm chart) | The cluster it's installed in | SSO, LDAP, token | One team, one cluster |
| **Hub** (Helm chart, `hub.enabled`) | Its own cluster, plus every cluster with an agent | SSO, LDAP | A company with several clusters |

In-cluster mode stays exactly as it is. A hub is an in-cluster install that
can also take agents, so upgrading is a chart value, not a migration.

## How it works

```
 browser ──HTTPS──▶  glowpod hub  ◀──WebSocket (outbound from the cluster)──  glowpod agent ──▶ API server
                     (cluster A)                                              (cluster B)
                        │
                        └──▶ its own API server (cluster A, in-cluster config)
```

### The agent dials out

Each added cluster runs `glowpod agent`: a Deployment, one replica, the
same image. It opens a WebSocket to `https://<hub>/agent/connect`. The
cluster only needs outbound HTTPS to the hub. Nothing has to reach its API
server from outside, and the hub never holds a kubeconfig for it.

Over that one connection the two sides run a stream multiplexer
(`hashicorp/yamux`). For every request, the hub opens a stream and speaks
plain HTTP over it. The agent's side is an `httputil.ReverseProxy` to its
API server. So client-go on the hub works unchanged (a custom `Dial` in
`rest.Config`), and so do watches, logs and exec, since the proxy passes
upgrades through.

**What the agent adds and refuses.** The agent swaps in its own
service-account token and keeps the `Impersonate-User` / `Impersonate-Group`
headers the hub set. It refuses any request that:

- has no `Impersonate-User` (the hub never acts as itself);
- impersonates a `system:` user, or any group starting with `system:` except
  `system:authenticated`;
- impersonates a group without the configured prefix (`oidc:`, `ldap:`).

So even a compromised hub can't become `system:masters` on a cluster. The
agent's ClusterRole is `impersonate` on users and groups, plus
`get/list/watch` on RoleBindings and ClusterRoleBindings. Nothing else.

**Adding a cluster.** A hub admin clicks *Add cluster* and gives it a name.
The hub creates a `Cluster` object and a one-time registration token valid
for 1 hour, then shows one command:

```bash
helm install glowpod-agent oci://ghcr.io/glowpod-dev/charts/glowpod-agent -n glowpod-system --create-namespace --set hub=https://glowpod.example.com --set token=<one-time>
```

On first connect the agent trades the token for a long-lived agent
credential, which it stores in a Secret in its namespace. The hub keeps only
the credential's hash. Removing the cluster in the hub revokes the
credential; uninstalling the chart removes the agent.

The agent pins the hub's CA when it's private (`--set hubCA=…`). It
reconnects with backoff, and the hub shows the cluster as **Disconnected**
with the time it was last seen.

### Every call runs as you, on every cluster

The hub signs people in with **SSO (OIDC) or LDAP**. On a cluster with an
agent, every call carries `Impersonate-User: <username>` and
`Impersonate-Group: <prefix><group>`, the same identity Glowpod uses in
in-cluster mode today. Service-account token sign-in belongs to one cluster,
so it stays in in-cluster mode and is off in a hub.

Per-identity clients and caches (security rule 2) are keyed by
**cluster + identity**, never by identity alone.

### Who sees which cluster

There is no second permission system. The agent watches the cluster's
RoleBindings and ClusterRoleBindings and reports which users and groups
appear as subjects. On the cluster list, a person sees a cluster when they
or one of their groups is bound to anything in it. A binding to
`system:authenticated` makes the cluster visible to everyone.

This list only decides what to *show*. What you can *do* is still decided
by the API server on every call, since every call is impersonated. A stale
list can show a cluster where you then see "You don't have access", but it
can never let you do something RBAC forbids.

Giving access stays where it is today: `kubectl create rolebinding … --group=oidc:…`,
GitOps, or (phase 4) a **Members** page in Glowpod that creates those
RoleBindings *as you*. You can only grant what you're allowed to grant
(Kubernetes' `bind` and escalation rules), so the page needs no special
rights.

**Hub admins** are the members of the groups in `hub.admins.groups`. They
add, rename and remove clusters. Being a hub admin gives **no access inside
any cluster**.

### Where the hub keeps things

In its own namespace on its own cluster, as Kubernetes objects:

- `clusters.hub.glowpod.dev` (CRD): name, labels, agent status, last seen,
  Kubernetes version, the subject list from the agent.
- Secrets: registration-token hashes, agent-credential hashes, the session
  signing key.

`kubectl get clusters.hub.glowpod.dev` works, and backups are the cluster's
own backups.

## Cluster shell

A **Shell** button in the top bar opens a terminal drawer at the bottom of
the page. It is already connected to the cluster you're looking at, with
`kubectl`, `helm`, `glowpod term`, `jq` and `yq`. You can have several tabs,
one per cluster.

- **Where it runs:** a short-lived pod in the hub's namespace
  (`glowpod-shell-<random>`). The image is `ghcr.io/glowpod-dev/glowpod-shell`,
  signed like the main image. The pod runs as non-root with a read-only root
  filesystem, and its NetworkPolicy allows egress to the hub only. It is
  deleted when the tab closes, or after 30 minutes idle.
- **As whom:** its kubeconfig points at the hub's Kubernetes API proxy,
  `https://<hub>/k8s/clusters/<id>`, with a token the hub issues for **that
  person and that cluster only**. The token lasts as long as the shell, and
  the hub keeps only its hash. The proxy impersonates exactly as the UI does.
  The pod's own service account has no rights (`automountServiceAccountToken: false`).
- **Local mode:** no shell, since you already have a terminal with your
  kubeconfig.

The same API proxy gives a second feature almost for free: **Download
kubeconfig.** It is one file with a context per cluster you can see, each
pointing at the hub with your own token, which lasts 8 hours by default and
can be revoked from your account menu. Use `kubectl`, k9s or `glowpod term`
on your laptop without a cloud login per cluster.

## Screens

Everything uses the existing tokens and components. Only these screens are
new or changed.

**Clusters (home after sign-in).** This page replaces "go straight to the
Overview" when you can see more than one cluster. It is a table, one row per
cluster:

| Column | Shows |
| --- | --- |
| Name | Name + labels (`env=prod`), JetBrains Mono |
| State | Pill + dot + word: **Connected** (lime), **Disconnected · 4 min** (white), **Waiting for agent**, **No access** (muted) |
| Kubernetes | `v1.34.1` |
| Nodes | `5 ready` |
| CPU · Memory · Pods | Three **S** HalfGauges (64px) |
| Needs attention | Count with a rose dot when > 0 |

Above the table there's a filter (name, label) and, for hub admins only, a
primary **Add cluster** button. Clicking a row opens that cluster's Overview.
With exactly one cluster, you go straight to its Overview, as today.

**Rail and section panel.** The rail gets a **Home** icon at the top that
leads back to Clusters. The cluster picker that's already in the section
panel becomes real. It lists your clusters with their state dots, filters as
you type, and switching keeps you on the same page (Workloads → Workloads).
URLs carry the cluster: `/c/<cluster>/workloads/…`.

**Add cluster** (hub admins). A name and optional labels, then the agent
command with a copy button. The page waits live for the agent: *Waiting for
agent → Connected* with the Kubernetes version.

**Hub settings** (hub admins). Clusters (rename, labels, remove, rotate the
agent credential), sign-in providers (read-only, from Helm values) and the
hub's version.

**Account menu.** Download kubeconfig, active kubeconfig tokens (revoke),
sign out.

## What changes in the code

| Area | Change |
| --- | --- |
| `internal/cluster` | `Provider` becomes `Registry`: `List(ctx, id)`, `Get(clusterID)` returning today's `Provider`. Local mode builds one per kubeconfig context; in-cluster mode has one; the hub has its own plus one per agent. |
| `internal/api` | `requireCluster` looks the id up in the registry instead of comparing with `"default"`. New: `GET /api/v1/clusters` (visible to you, with gauges), hub admin routes under `/api/v1/hub/…`, `/k8s/clusters/{id}/*` proxy, `/agent/connect`, shell WebSocket. |
| `internal/hub` (new) | Cluster CRD store, registration, agent sessions (yamux), visibility from subjects, kubeconfig tokens, shell pods. |
| `internal/agent` (new) | Dial, reconnect, the reverse proxy with impersonation guards, RBAC subject watch. |
| `cmd/glowpod` | `glowpod hub` (or `-hub` in server mode) and `glowpod agent`. |
| `frontend/src/app` | Routes under `/c/:cluster`, Home icon, real cluster picker, shell drawer. |
| `frontend/src/features/clusters` (new) | Clusters page, Add cluster, hub settings. |
| `deploy/helm` | `hub.*` values in the glowpod chart; a new `glowpod-agent` chart; the shell image. |
| `glowpod term` | `:ctx` switches kubeconfig context (local), or hub clusters when signed in to a hub (later). |

## Build order

Each phase ships on its own and is tested on the lab before the next starts.

1. **Many clusters in local mode.** Registry, `/api/v1/clusters`, the
   Clusters page, the real picker, `/c/<cluster>/…` URLs, every kubeconfig
   context listed. No hub yet, but everyone with more than one context gets
   it right away.
2. **Hub and agent.** `glowpod agent`, the tunnel, registration, Add cluster,
   connection states, impersonation guards, visibility from RBAC subjects.
   Tested with the lab as the hub and a second throwaway cluster (kind) as an
   agent.
3. **Cluster shell and kubeconfig download.** The API proxy, per-person
   tokens, shell pods and image, the drawer.
4. **Access.** The Members page (RoleBindings created as you), hub settings,
   credential rotation.
5. **Across clusters.** Search and "Needs attention" across all your
   clusters.

CLAUDE.md says v1 is one cluster. Once this design is approved, it changes
to describe the three modes, and these phases follow the six already done.
