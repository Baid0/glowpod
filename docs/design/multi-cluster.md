# Glowpod Hub: many clusters, one sign-in

Today Glowpod shows the contexts in your kubeconfig (local mode) or the one
cluster it is installed in (in-cluster mode). **Glowpod Hub** works the way
Rancher does. You run one Hub server, import each cluster with one
`kubectl apply`, and everyone signs in at one address. After signing in,
people see the list of clusters they are members of, open one, and get
everything Glowpod has for it, plus a shell with `kubectl` in the browser.

## How Rancher does it, and what Glowpod copies

| Rancher | Glowpod Hub |
| --- | --- |
| Rancher server runs on Kubernetes: Helm on a small dedicated cluster (K3s, RKE2) for production | **Same.** `helm install glowpod … --set hub.enabled=true` on any cluster; a small dedicated one is recommended |
| Quick start: `docker run --privileged rancher/rancher` on any server, with K3s running inside the container, for trying out | **Same.** `docker run --privileged ghcr.io/glowpod-dev/glowpod-hub` runs K3s inside and the Hub on it |
| First start: a bootstrap password, then set the admin password and the server URL | **Same** |
| Local users, plus LDAP/AD, SAML, OIDC, GitHub set up in the UI | Local users, plus **OIDC and LDAP**, set up in the UI or in Helm values |
| The Hub's own cluster is listed as `local` | **Same** |
| Import a cluster: `kubectl apply -f https://rancher/v3/import/<token>.yaml` installs `cattle-cluster-agent` | **Same:** `kubectl apply -f https://<hub>/v3/import/<token>.yaml` installs `glowpod-agent` |
| The agent dials out over a WebSocket tunnel; the cluster needs only outbound HTTPS | **Same** |
| Requests go Rancher → tunnel → agent, which **impersonates** the user | **Same** |
| The agent runs as **cluster-admin** | **Different:** the agent may only impersonate and read RBAC bindings (security rule 1) |
| Cluster members (Owner, Member, custom) and **projects** (groups of namespaces) with roles | **Same ideas.** Glowpod writes them as RoleBindings *as the person granting*, never as the agent |
| The person who imports a cluster becomes its owner | **Same** |
| Kubectl shell in a pod, with a Rancher token | **Same** |
| Download a kubeconfig that goes through Rancher | **Same** |
| Creates clusters (RKE2, EKS, AKS, GKE), Fleet, the app catalog, monitoring | **Not now.** Glowpod imports clusters that already exist |

## Four ways to run

| Mode | Clusters | Sign-in | Who it's for |
| --- | --- | --- | --- |
| Local (`glowpod`) | Every context in your kubeconfig | Your kubeconfig | One person on a laptop |
| Terminal (`glowpod term`) | One context | Your kubeconfig | One person in a terminal |
| In-cluster (Helm chart) | The cluster it's installed in | SSO, LDAP, token | One team, one cluster |
| **Hub** (Helm `hub.enabled`, or Docker) | `local` plus every imported cluster | Local users, SSO, LDAP | A company with several clusters |

In-cluster mode stays as it is. The Hub is the same chart with
`hub.enabled=true`, so turning a team install into a Hub is a chart value,
not a migration.

## Installing the Hub

**Production: Helm on a Kubernetes cluster.** It's best on a small cluster
of its own (one to three VMs with K3s or RKE2), so that it isn't affected
when a cluster it manages has trouble.

```bash
helm install glowpod glowpod/glowpod -n glowpod --create-namespace \
  --set hub.enabled=true --set publicURL=https://glowpod.example.com
```

**Quick start: Docker on any server.** This is for trying the Hub, like
Rancher's Docker install. K3s runs inside the container, and the Hub runs on
it.

```bash
docker run -d --restart=unless-stopped --privileged \
  -p 80:80 -p 443:443 -v /opt/glowpod:/var/lib/glowpod \
  ghcr.io/glowpod-dev/glowpod-hub
```

- The image is `glowpod-hub`: the `glowpod` binary plus a pinned K3s.
  `--privileged` is needed to run K3s, as it is for Rancher.
- All state (K3s data, the Hub's objects) lives in `/opt/glowpod` on the
  server. Moving to a Helm install later is a backup and restore (phase 5).
- It makes a self-signed certificate unless you pass your own with
  `-v /certs:/etc/glowpod/tls`.

**First start**, for both installs:

1. Glowpod creates a one-time **bootstrap password**. Unlike Rancher, it
   never writes the password to the log (security rule 5); the log says
   how to read it:
   - Docker: `docker exec <container> glowpod hub bootstrap-password`
   - Helm: `kubectl -n glowpod get secret glowpod-bootstrap -o jsonpath='{.data.password}' | base64 -d`
2. Open the Hub's address and sign in as `admin` with that password.
3. Set a new admin password and confirm the server URL, the address agents
   will connect to. The bootstrap password then stops working.

## Users and sign-in

- **Local users** are stored in the Hub with Argon2id password hashes. The
  first one is `admin`.
- **SSO (OIDC) and LDAP** are set up under *Hub settings → Authentication*,
  or in Helm values (`hub.auth.*`), which win over the UI. Microsoft Entra
  and Google work as they already do in in-cluster mode.
- Kubernetes sees each person under a stable name with a prefix:
  - local users: `glowpod:<username>`;
  - SSO users: `oidc:<username claim>`, groups `oidc:<group>`;
  - LDAP users: `ldap:<uid>`, groups `ldap:<group>`.
- **Hub roles** (Rancher's global roles):
  - **Administrator:** manages users, authentication and settings, and can
    import and remove clusters;
  - **User:** sees the clusters they're a member of.

  Being a Hub administrator gives **no rights inside a cluster**. Those come
  only from that cluster's own members.

## Importing a cluster

1. *Clusters → Import*, then give the cluster a name and optional labels
   (`env=prod`).
2. The Hub shows one command and a variant for a self-signed Hub certificate:

   ```bash
   kubectl apply -f https://glowpod.example.com/v3/import/<token>.yaml
   curl --insecure -sfL https://glowpod.example.com/v3/import/<token>.yaml | kubectl apply -f -
   ```

   Run it with kubectl as someone allowed to create ClusterRoles in that
   cluster. The token can be used once and expires after one hour.
3. The manifest creates the following in that cluster:
   - the `glowpod-system` namespace;
   - the `glowpod-agent` ServiceAccount, ClusterRole and Deployment, with
     one replica and the same image;
   - a Secret with the one-time token and the Hub's URL and CA;
   - a ClusterRoleBinding that makes **the person who clicked Import an
     owner** (`cluster-admin`), like Rancher. There's a checkbox to leave it
     out.
4. The agent dials `wss://<hub>/v3/connect`. On first connect it trades the
   token for a long-lived agent credential, which it stores in its Secret.
   The Hub keeps only the credential's hash.
5. The page waits live: *Waiting for agent → Connected · v1.34.2 · 5 nodes*.

**Removing** a cluster in the Hub revokes its credential, and the agent
stops. The Hub shows a matching `kubectl delete -f …` to clean up the
cluster.

The Hub's own cluster is listed as **`local`**. Only Hub administrators see
it, unless someone is made a member.

## How requests reach a cluster

```
 browser ──HTTPS──▶  Hub  ◀── WebSocket, opened by the agent (outbound only) ── glowpod-agent ──▶ API server
                      │
                      └──▶ local: its own API server, in-cluster config
```

Over the one WebSocket, the two sides run a stream multiplexer
(`hashicorp/yamux`):
- For each request, the Hub opens a stream and speaks plain HTTP over it.
  client-go on the Hub works unchanged (a custom `Dial` in `rest.Config`).
  Watches, logs and exec work because the agent's `httputil.ReverseProxy`
  passes upgrades through.
- The agent replaces the authentication with its own service-account token
  and keeps the `Impersonate-User` and `Impersonate-Group` headers.

**Where Glowpod is stricter than Rancher.** Rancher's agent is
cluster-admin. Glowpod's agent's ClusterRole is only:
- `impersonate` on users and groups;
- `get/list/watch` on RoleBindings and ClusterRoleBindings.

The agent also refuses any request that:
- has no `Impersonate-User` (the Hub never acts as itself);
- impersonates a `system:` user, or any group starting with `system:`
  except `system:authenticated`;
- impersonates a user or group without a Glowpod prefix (`glowpod:`,
  `oidc:`, `ldap:`).

So even a Hub that has been broken into can't become `system:masters` on
your clusters. It can act only as people who already have rights there.

Clients and caches are kept per **cluster + person** (security rule 2).

## Members and projects

These work as in Rancher, but they're stored as Kubernetes RBAC in each
cluster, not in a second permission system.

**Cluster members** (*cluster → Members*): add a person or group with a role.

| Role | RBAC it creates |
| --- | --- |
| Owner | ClusterRoleBinding to `cluster-admin` |
| Member | ClusterRoleBinding to `view`, plus `edit` in the projects they're added to |
| Read-only | ClusterRoleBinding to `view` |
| Custom | Any ClusterRole in the cluster |

**Projects** (phase 5) are named groups of namespaces, such as "payments" =
`payments-api` + `payments-db`. A project role becomes a RoleBinding in each
of its namespaces, and new namespaces in the project get them too. A
project is stored as the label `hub.glowpod.dev/project=<name>` on its
namespaces, so the cluster itself remembers it.

Glowpod creates these bindings **as the person granting them**,
impersonated, never as the agent. Kubernetes' own `bind` and escalation
rules decide whether they may, so an Owner can add members and a
Read-only member can't.

**The cluster list.** The agent reports which users and groups are subjects
of bindings in its cluster. You see a cluster when you or one of your
groups is bound in it, whether Glowpod made the binding, kubectl did, or
GitOps did. What you can *do* is still decided by the API server on every
call.

## Where the Hub keeps its state

In its own namespace, as Kubernetes objects (in the Docker install, on the
K3s inside the container). There is no separate database.

- **CRDs** in `hub.glowpod.dev`:
  - `Cluster`: name, agent status, last seen, version, subjects;
  - `AuthConfig` (phase 5): OIDC and LDAP settings; their secrets go in Secrets.
- **Secrets:** one per local user (Argon2id password hash and Hub role;
  built as Secrets rather than a `User` CRD, since they hold credentials),
  hashes of import tokens and agent credentials, the bootstrap password,
  kubeconfig token hashes.
- **ConfigMap** `glowpod-hub`: the server URL, and whether first start is done.

`kubectl get clusters.hub.glowpod.dev` works, and backups are the cluster's
own backups.

## Cluster shell and kubeconfig

**Shell.** A **Shell** button in the top bar opens a terminal drawer for the
cluster on screen, with `kubectl`, `helm`, `glowpod term`, `jq` and `yq`.
This is Rancher's kubectl shell.
- It's a short-lived pod in the Hub's namespace, from the image
  `ghcr.io/glowpod-dev/glowpod-shell`:
  - it runs as non-root, with a read-only root filesystem;
  - it has no service-account token;
  - its NetworkPolicy allows egress to the Hub only;
  - it's deleted when the tab closes, or after 30 minutes idle.
- Its kubeconfig points at `https://<hub>/k8s/clusters/<id>` with a token
  for that person and that cluster, valid as long as the shell. The Hub
  proxies those calls and impersonates exactly as the UI does.

**Download kubeconfig.** The account menu has a file with a context per
cluster you can see. Each context points at the Hub with your own token,
valid 8 hours by default, and you can revoke it from the same menu.

## Screens

**Clusters (home after sign-in)** is the page that's already built for local
mode, plus:
- a **Provider** column (K3s, EKS, GKE, AKS, kind… detected from the nodes);
- an **Import** button for Hub administrators.

State is shown in the usual pill + dot + word: Connected · Disconnected
4 min · Waiting for agent · Credentials refused.

**Import cluster.** A name and labels, then the command with a copy button,
waiting live for the agent.

**Cluster → Members** and **Cluster → Projects**: tables with a role picker.
Only the actions your RBAC allows are shown.

**Hub settings** (administrators): users, authentication (Local, OIDC,
LDAP), server URL, and the Hub's version.

**First start:** set the admin password and confirm the server URL.

**Account menu:** change your password (local users), download kubeconfig,
your kubeconfig tokens, sign out.

## What changes in the code

| Area | Change |
| --- | --- |
| `internal/cluster` | The `Registry` from phase 1 gains add and remove at runtime; agent clusters dial through their tunnel |
| `internal/hub` (new) | CRD store, bootstrap, local users, auth config, import manifests and tokens, agent sessions (yamux), subjects → cluster list, members and projects, kubeconfig tokens, shell pods |
| `internal/agent` (new) | Dial, reconnect, the reverse proxy with impersonation guards, the RBAC subject watch |
| `internal/auth` | Local users (Argon2id), and providers loaded from the Hub's AuthConfig as well as flags |
| `internal/api` | `/v3/import/{token}.yaml`, `/v3/connect`, `/k8s/clusters/{id}/*`, `/api/v1/hub/…` (users, auth, clusters), shell WebSocket |
| `cmd/glowpod` | `glowpod hub`, `glowpod hub bootstrap-password`, `glowpod agent` |
| `deploy/` | `hub.*` values in the chart; Dockerfiles for `glowpod-hub` (with K3s) and `glowpod-shell` |
| `frontend` | First-start page, Import, Members, Projects, Hub settings, account menu, shell drawer |

## Build order

Each phase ships on its own and is tested on the lab before the next starts.

1. ✅ **Many clusters in local mode:** the registry, the Clusters page, the
   picker, `/c/<cluster>/` addresses.
2. **Hub and agent:**
   - Helm `hub.enabled`, first start with the bootstrap password, local users;
   - import with `kubectl apply`, the tunnel, the impersonation guards, the
     `local` cluster;
   - the cluster list from RBAC subjects; the importer becomes owner.

   Tested with the lab as the Hub and a throwaway K3s VM as an imported
   cluster.
3. **Docker quick start:** the `glowpod-hub` image with K3s inside.
4. **Shell and kubeconfig download.**
5. **Members, projects and Hub settings:** SSO and LDAP set up in the UI,
   backup and restore.
6. **Across clusters:** search and "Needs attention" over all clusters.
