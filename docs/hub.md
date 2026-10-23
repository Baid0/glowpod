# Glowpod Hub: many clusters, one sign-in

Glowpod Hub works the way Rancher does. You install one Hub, import each
cluster with a single `kubectl apply`, and everyone signs in at one address.
After signing in, people see the list of clusters they have access to, open
one, and get everything Glowpod has for it.

Needs Glowpod 0.4.0 or later.

## How it works

```
 browser ──HTTPS──▶  Hub  ◀── WebSocket, opened by the agent (outbound only) ── glowpod-agent ──▶ API server
                      │
                      └──▶ local: the cluster the Hub runs in
```

- **The Hub** is the Glowpod chart with `hub.enabled=true`. Its own cluster
  is listed as **`local`**.
- **Each imported cluster runs `glowpod-agent`**, which connects out to the
  Hub. The cluster needs no inbound access, and the Hub never holds a
  kubeconfig for it.
- **Everything runs as the person signed in.** The Hub sends their name and
  groups, and the agent calls the API server *impersonating* them, so each
  cluster's own RBAC decides what they can see and do.
- **Who sees which cluster** comes from that cluster's role bindings. You see
  a cluster when you, or one of your groups, is bound to anything in it.
  Hub administrators see every cluster, but that gives them no rights
  inside any cluster.

### Where Glowpod is stricter than Rancher

Rancher's agent is cluster-admin. Glowpod's agent may only **impersonate**
users and groups and **read role bindings**. It also refuses any request
that:

- doesn't impersonate a user (the Hub never acts as itself);
- impersonates a `system:` user, or a `system:` group other than
  `system:authenticated`;
- impersonates a name without a Glowpod prefix: `glowpod:`, `oidc:` or
  `ldap:`.

So even a Hub that has been broken into can't become cluster admin on your
clusters. It can act only as people who already have rights there.

## Install the Hub

Install it on a Kubernetes cluster, preferably a small one of its own (one
to three VMs with K3s or RKE2), so trouble in a cluster it manages doesn't
reach it. Serve it over HTTPS at an address that every cluster you'll
import can reach: agents connect to it.

```yaml
# hub-values.yaml
hub:
  enabled: true
  # SSO or LDAP users and groups who are Hub administrators, besides admin.
  admins: ["oidc:platform-team"]
publicURL: https://glowpod.example.com
ingress:
  enabled: true
  className: nginx
  host: glowpod.example.com
  tls:
    - secretName: glowpod-tls
      hosts: [glowpod.example.com]
auth:
  # A token belongs to one cluster, so a Hub doesn't offer token sign-in.
  token:
    enabled: false
```

```bash
helm repo add glowpod https://glowpod.dev/charts
helm install glowpod glowpod/glowpod -n glowpod --create-namespace -f hub-values.yaml
```

The chart adds the following:

| Object | Why |
| --- | --- |
| CRD `clusters.hub.glowpod.dev` | One record per imported cluster (`kubectl get clusters.hub.glowpod.dev`). Kept when you uninstall |
| ClusterRole `<release>-hub` | Keep those records, and read role bindings to know who sees `local` |
| Role `<release>-hub` | Its own Secrets (password hashes, hashed agent credentials) and settings, in its namespace only |
| ClusterRoleBinding `<release>-local-admin` | `glowpod:admin` is cluster-admin on `local`, as in Rancher. Set `hub.localAdmin: false` to leave it out |

The Hub keeps its state as Kubernetes objects in its own namespace. There's
no database, and your cluster backups cover it. Keep one replica: each agent
keeps its tunnel to one Hub pod.

If the Hub's certificate comes from a private CA, put that CA in a Secret
(`ca.crt`) and set `hub.caSecret`, so agents trust it.

## First start

1. Read the bootstrap password. Glowpod never writes it to its log; the log
   only says how to read it:

   ```bash
   kubectl -n glowpod get secret glowpod-bootstrap -o jsonpath='{.data.password}' | base64 -d
   ```

2. Open the Hub's address and choose **Sign in with a Glowpod account**:
   username `admin` and that password.
3. Choose your own password (at least 12 characters) and confirm the
   **server URL**, the address agents connect to. Saving deletes the
   bootstrap password.

## Import a cluster

1. Go to **Clusters → Import** and give the cluster a name. Leave **Make me
   an owner** on to become cluster-admin there; the command you run creates
   that binding.
2. The Hub shows one command. Run it with `kubectl` pointed at the cluster,
   as someone allowed to create ClusterRoles there:

   ```bash
   kubectl apply -f https://glowpod.example.com/v3/import/<token>.yaml
   ```

   Use the second command shown if the machine running `kubectl` doesn't
   trust the Hub's certificate. The link works once, for one hour; **Connect**
   on the cluster's row makes a new one.
3. The dialog waits, then shows **Connected** when the agent is up.

The manifest creates the following in the cluster:

- the `glowpod-system` namespace;
- the `glowpod-agent` ServiceAccount, ClusterRole, ClusterRoleBinding and
  Deployment (one replica);
- a Role that lets the agent read and update only its own Secret;
- that Secret, holding the Hub's address and the one-time token;
- and, if you left the box on, a `glowpod-owner` ClusterRoleBinding.

On first contact the agent trades the token for its own credential, and
keeps it in the Secret. The Hub keeps only a hash of it.

## Give people access

Access is the cluster's own RBAC, as with in-cluster Glowpod. Bind the
names the Hub uses:

| Signed in with | Kubernetes sees | Example |
| --- | --- | --- |
| A Glowpod account | `glowpod:<username>` | `glowpod:admin` |
| SSO | `oidc:<username claim>`, groups `oidc:<group>` | `oidc:ann@example.com`, `oidc:payments` |
| LDAP | `ldap:<uid>`, groups `ldap:<group>` | `ldap:bob`, `ldap:ops` |

```bash
# In the imported cluster: the payments team may change things in its namespace.
kubectl create rolebinding payments-edit -n payments --clusterrole=edit --group=oidc:payments
```

The cluster then shows up on their Clusters page within half a minute,
because the agent reports its bindings every 30 seconds. SSO and LDAP are
set up as in [Sign-in and access](authentication.md); on a Hub, their
prefixes are always `oidc:` and `ldap:`.

## States on the Clusters page

| State | What it means |
| --- | --- |
| Connected | The agent's tunnel is open |
| Waiting for agent | Imported, but the agent hasn't connected yet. Run the import command, or check the agent's pod |
| Disconnected · 4 min | The agent was connected and its tunnel closed. It reconnects by itself |
| Unreachable, Credentials refused | The tunnel is fine, but the cluster's API server didn't answer, or refused |

## Remove a cluster

On the cluster's row, choose **Remove** and type its name. The Hub forgets
the cluster and refuses its agent from then on. It then shows the command
that removes the agent from the cluster:

```bash
kubectl delete namespace glowpod-system && kubectl delete clusterrole glowpod-agent && kubectl delete clusterrolebinding glowpod-agent glowpod-owner --ignore-not-found
```

## Troubleshooting

| You see | Do this |
| --- | --- |
| The agent's pod logs "the Hub refused this cluster" | The cluster was removed from the Hub, or its first-contact token expired. Import it again, or remove the agent |
| The agent's pod logs "connect to the Hub: … certificate" | The Hub's certificate isn't trusted: set `hub.caSecret`, then make a new import command |
| "Waiting for agent" stays | `kubectl -n glowpod-system logs deploy/glowpod-agent` in that cluster; check it can reach the server URL over HTTPS |
| Someone signed in but sees no clusters | Bind their `oidc:`/`ldap:` name or group in a cluster; it appears within 30 seconds |
| `admin` can't sign in with the bootstrap password | Setup is already done; the bootstrap password stopped working then |

## Not yet

These come next, in this order:

- Running the Hub with `docker run` on a plain server.
- A `kubectl` shell in the browser for each cluster, and one kubeconfig
  download for all of them. Pod shells already work through the Hub.
- More local users, cluster members and projects, and setting up SSO and
  LDAP in the UI. Until then, `admin` is the only local account; everyone
  else signs in with SSO or LDAP.
