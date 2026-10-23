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

## Try it with Docker

To try the Hub on any Linux server (or Docker Desktop) without a
Kubernetes cluster, run the `glowpod-hub` image. It runs K3s inside the
container and the Hub on it, like Rancher's Docker install. Needs Glowpod
0.5.0 or later.

```bash
docker run -d --name glowpod --restart=unless-stopped --privileged \
  -p 80:80 -p 443:443 -v glowpod:/var/lib/glowpod \
  ghcr.io/glowpod-dev/glowpod-hub:0.6.0
```

- **`--privileged`** is needed to run Kubernetes inside a container, as for
  Rancher.
- **Everything is in the volume** `/var/lib/glowpod`: K3s, the Hub's
  records, sign-ins and its certificate authority. Keep the volume, and a
  new container (an upgrade, say) carries on where the old one stopped.
- **HTTPS:** the Hub makes its own certificate authority at first start and
  serves certificates from it, for the server URL and any name you open it
  by. Agents trust it automatically (the import manifest carries it);
  browsers show a warning until you bring your own certificate:
  `-v /path/to/certs:/etc/glowpod/tls:ro`, with `tls.crt` and `tls.key`,
  plus `ca.crt` if a private CA signed them. Port 80 redirects to HTTPS.
  Agents keep trusting the CA they were imported with, so after changing
  the certificate, choose **Connect** on each cluster and run its command
  again: the agent gets the new CA and keeps its credential.
- **Settings** go in `GLOWPOD_` variables, as with the binary:
  `-e GLOWPOD_PUBLIC_URL=https://glowpod.example.com`,
  `-e GLOWPOD_HUB_ADMINS=oidc:platform`, or SSO with `GLOWPOD_OIDC_ISSUER`
  and friends (see [Configuration](configuration.md)).
- **First start** takes a few seconds once the image is pulled. Read the
  bootstrap password with:

  ```bash
  docker exec glowpod glowpod hub bootstrap-password
  ```

  Then continue with [First start](#first-start) from step 2. The server URL
  is the address imported clusters reach this server by, such as
  `https://203.0.113.10`.

**Upgrade** by starting the new version on the same volume. Sign-ins and
imported clusters carry over, and agents reconnect by themselves:

```bash
docker pull ghcr.io/glowpod-dev/glowpod-hub:<new version>
docker rm -f glowpod
docker run -d --name glowpod --restart=unless-stopped --privileged \
  -p 80:80 -p 443:443 -v glowpod:/var/lib/glowpod \
  ghcr.io/glowpod-dev/glowpod-hub:<new version>
```

The cluster inside the container is listed as `local`. It's for trying the
Hub, or for a small setup: everything depends on one container. For
production, install the Hub with Helm on a cluster of its own.

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
no database, and your cluster backups cover it. Sign-ins are kept there too,
so restarting or upgrading the Hub doesn't sign anyone out; agents reconnect
by themselves within seconds. Keep one replica: each agent keeps its tunnel
to one Hub pod.

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

Access is the cluster's own RBAC, as with in-cluster Glowpod. The easiest
way is **Cluster → Members** and **Cluster → Projects** in Glowpod (see
[Members and projects](members.md)); kubectl and GitOps work as well. Bind
the names the Hub uses:

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

## Use kubectl

Needs Glowpod 0.6.0 or later. Open the account menu (your initials, top right) and choose **Kubeconfig**,
then **Download kubeconfig**. The file has a context for each cluster you
can see, named by the cluster's ID, and starts in the cluster you had open:

```bash
kubectl --kubeconfig ~/Downloads/glowpod-kubeconfig.yaml get pods --context prod-eu
```

- kubectl goes through the Hub, and through the agent for imported
  clusters, so it works for clusters behind a firewall too. `get`, `apply`,
  `logs -f`, `get -w`, `exec` and `port-forward` all work.
- It acts **as you**, exactly as the UI does: the cluster's own RBAC decides
  what you can do. The Hub ignores `--as` and any other impersonation the
  client asks for, so a kubeconfig can't become someone else.
- It works for 8 hours (`hub.kubeconfigTTL`). The same dialog lists your
  kubeconfigs, with when each was made and last used; **Revoke** stops one
  at once. Each person keeps at most 20; a new one past that replaces the
  oldest.
- The Hub keeps only a hash of each token, in a Secret
  `glowpod-kubeconfig-<id>` in its namespace, and logs when a token is made
  or revoked, never the token.
- If the Hub's certificate comes from a private CA (its own, in the Docker
  install), the file carries that CA, so kubectl trusts it.

## Shell in the browser

Needs Glowpod 0.6.0 or later. On any cluster page, **Shell** in the top bar opens a terminal along the
bottom of the page, with `kubectl`, `helm`, `jq`, `yq` and `glowpod term`
set up for that cluster, as you. kubectl starts in the namespace you picked,
and `k` is short for `kubectl`.

- The shell is a short-lived pod in the Hub's namespace, from the
  `glowpod-shell` image. It runs as non-root with a read-only root
  filesystem and no service-account token, and a NetworkPolicy lets it
  reach only the Hub. (The NetworkPolicy needs a network plugin that
  enforces it, such as Calico or Cilium; K3s's does.)
- Its kubeconfig acts as you on that one cluster only, through the same
  proxy as your downloaded kubeconfig, so the cluster's RBAC decides what
  you can do. You get no rights in the Hub's namespace.
- Closing the drawer, or 30 minutes without typing, deletes the pod and its
  token. Files you make there go with it. A shell lives 12 hours at most.
- The first shell on a node waits for the image to download, about a
  minute.
- Turn shells off with `hub.shell.enabled: false`. For a private registry,
  set `hub.shell.image`.

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
| The agent's pod logs "connect to the Hub: … certificate" | The Hub's certificate isn't trusted: set `hub.caSecret` (or, for Docker, mount `ca.crt`), then choose **Connect** and run the command again |
| The agent's pod logs "certificate has expired or is not yet valid" | That cluster's clock is off: a certificate made just now looks like it's from the future. Turn on time sync on its nodes (`timedatectl set-ntp true`) |
| "Waiting for agent" stays | `kubectl -n glowpod-system logs deploy/glowpod-agent` in that cluster; check it can reach the server URL over HTTPS |
| Someone signed in but sees no clusters | Bind their `oidc:`/`ldap:` name or group in a cluster; it appears within 30 seconds |
| The shell says it "couldn't download the shell image" | The Hub's cluster can't pull `ghcr.io/glowpod-dev/glowpod-shell`: allow it, or mirror it and set `hub.shell.image` |
| kubectl says "This kubeconfig expired or was revoked" | Download a new one: account menu, **Kubeconfig** |
| kubectl says "Glowpod Hub has no cluster … that you can see" | The context's cluster was removed, or you have no binding in it any more |
| `admin` can't sign in with the bootstrap password | Setup is already done; the bootstrap password stopped working then |
| The Docker Hub stops right after starting | `docker logs glowpod`; K3s's own log is in the volume: `docker exec glowpod tail -50 /var/lib/glowpod/k3s.log`. Check the container is `--privileged` |

## Not yet

These come next, in this order:

- More local users, cluster members and projects, and setting up SSO and
  LDAP in the UI. Until then, `admin` is the only local account; everyone
  else signs in with SSO or LDAP.
