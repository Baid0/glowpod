# Sign-in and access

In-cluster mode offers three ways to sign in. Turn on any mix; the sign-in
page shows the ones you enable.

| Method | Good for | How Glowpod talks to Kubernetes |
| --- | --- | --- |
| [SSO (OpenID Connect)](#sso-with-openid-connect) | Everyone, day to day | Impersonates the user, or passes their ID token |
| [LDAP / Active Directory](#ldap-and-active-directory) | Companies with a directory and no SSO | Impersonates the user |
| [Token](#service-account-token) | First try, automation accounts, break-glass | Sends the token as-is |

Local mode (`glowpod` on your machine) has no sign-in: it uses your
kubeconfig and only listens on `127.0.0.1`.

## How identity works

Every Kubernetes call Glowpod makes runs **as the person who signed in**.
There is no shared admin identity, and no cache that one person's data could
leak from to another.

- **Token:** the browser never holds the token. Glowpod keeps it in memory for
  the session and sends it on each call; the browser gets a random session
  ID in an `HttpOnly`, `Secure`, `SameSite=Strict` cookie.
- **SSO and LDAP:** Glowpod learns the username and groups, then calls the
  API server with
  [impersonation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#user-impersonation)
  (`Impersonate-User`, `Impersonate-Group`). Its service account has one
  right: `impersonate` on users and groups. The API server then checks the
  person's own RBAC on every call.
- **Prefixes.** SSO groups arrive as `oidc:<group>` and LDAP groups as
  `ldap:<group>` by default, so a directory group can never pose as a
  Kubernetes one. Glowpod also refuses to impersonate `system:` users and
  drops `system:` groups. Keep the prefixes.

You can see who you are, and in Deep mode your groups, in the avatar menu
(top right).

## Give people access

Glowpod adds nothing on top of Kubernetes RBAC: bind roles to the names
Glowpod signs people in as.

```bash
# A team that may change things in its own namespace
kubectl create rolebinding payments-edit -n payments --clusterrole=edit --group=oidc:payments

# Read-only for everyone in a directory group, across the cluster
kubectl create clusterrolebinding devs-view --clusterrole=view --group=ldap:developers

# One person, by SSO username (the email by default)
kubectl create rolebinding alice-admin -n shop --clusterrole=admin --user=alice@example.com
```

Good to know:

- **Namespaces list.** The namespace picker lists namespaces when the person
  may `list namespaces` (a cluster-scoped right that `view` and `edit` in a
  RoleBinding don't give). Without it, Glowpod asks them to type a namespace
  and remembers it. To let people see the list without seeing anything
  inside other namespaces:

  ```bash
  kubectl create clusterrole namespace-lister --verb=get,list,watch --resource=namespaces
  kubectl create clusterrolebinding everyone-lists-namespaces --clusterrole=namespace-lister --group=oidc:employees
  ```

- **Nodes and the cluster gauges** need `list nodes` (cluster-scoped). The
  storage gauge reads volume usage from the kubelets (`get nodes/proxy`),
  which usually only admins have. Glowpod shows what each person may see and
  says what it can't.
- **Buttons follow RBAC.** Scale, restart, edit, delete, exec, cordon and drain
  appear only when a `SelfSubjectAccessReview` says the person may do them.
- **Secret values** are hidden until someone clicks to reveal one, and only
  if they may `get` that Secret.

Check what someone can do the same way the API server will:

```bash
kubectl auth can-i --list -n payments --as=alice@example.com --as-group=oidc:payments
```

## Service-account token

On by default. People paste a bearer token and get exactly its rights. Any
token the API server accepts works (a service-account token, or an OIDC ID
token your API server already trusts).

A token for one team:

```bash
kubectl create serviceaccount payments-viewer -n payments
kubectl create rolebinding payments-viewer -n payments --clusterrole=view --serviceaccount=payments:payments-viewer
kubectl create token payments-viewer -n payments --duration=8h
```

A break-glass admin token (keep it short-lived and give it to few people):

```bash
kubectl create serviceaccount glowpod-admin -n kube-system
kubectl create clusterrolebinding glowpod-admin --clusterrole=cluster-admin --serviceaccount=kube-system:glowpod-admin
kubectl create token glowpod-admin -n kube-system --duration=1h
```

Turn tokens off once SSO or LDAP works, if you like:
`auth.token.enabled: false`.

## SSO with OpenID Connect

Works with any OIDC provider: Okta, Microsoft Entra ID, Google, Keycloak,
Dex, Authentik, Auth0, GitLab and others. Glowpod uses the authorization code
flow with PKCE, checks the state and nonce, and verifies the ID token's
signature, issuer, audience and expiry.

### 1. Register Glowpod with your provider

Create a **web application** (confidential client) with:

- Redirect URI: `<publicURL>/auth/oidc/callback`, e.g.
  `https://glowpod.example.com/auth/oidc/callback`
- Grant type: authorization code
- Scopes: `openid email profile`, plus `groups` if your provider needs it to
  send groups

Note the **issuer URL**, **client ID** and **client secret**.

### 2. Store the client secret

```bash
kubectl -n glowpod create secret generic glowpod-oidc --from-literal=client-secret='<the secret>'
```

### 3. Turn it on

```yaml
publicURL: https://glowpod.example.com
auth:
  oidc:
    enabled: true
    name: Okta                       # text on the sign-in button
    issuer: https://example.okta.com
    clientID: 0oa1b2c3d4example
    existingSecret: glowpod-oidc     # key: client-secret
    scopes: [openid, email, profile, groups]
    usernameClaim: email             # becomes the Kubernetes username
    groupsClaim: groups              # becomes Kubernetes groups, with groupsPrefix
    groupsPrefix: "oidc:"
```

```bash
helm upgrade glowpod deploy/helm/glowpod -n glowpod -f glowpod-values.yaml
```

When `usernameClaim` is `email`, Glowpod refuses a token whose
`email_verified` is `false`, as Kubernetes does.

### Try it on your laptop first

Before installing in a cluster, you can check a provider end to end from
your laptop: Glowpod in server mode on `localhost`, against any cluster your
kubeconfig can reach. Your kubeconfig user must be allowed to impersonate
(cluster admins are), because Glowpod acts as whoever signs in.

1. In your provider, use the redirect URI
   `http://localhost:8080/auth/oidc/callback`. Google and Microsoft both
   accept plain `http` for `localhost`.
2. Put the client secret in your shell without it showing or landing in
   your history. Paste it, then press Enter:

   ```bash
   read -rs GLOWPOD_OIDC_CLIENT_SECRET && export GLOWPOD_OIDC_CLIENT_SECRET
   ```

3. Start Glowpod in the same shell (the provider's own values are below):

   ```bash
   glowpod -mode server -addr 127.0.0.1:8080 \
     -public-url http://localhost:8080 -insecure-cookies -auth-token=false \
     -oidc-name Google -oidc-issuer https://accounts.google.com \
     -oidc-client-id <client id> -oidc-groups-prefix oidc:
   ```

   `-insecure-cookies` is only for this plain-HTTP test. Add
   `-kubeconfig` or `-context` to pick the cluster.
4. Open **http://localhost:8080** (exactly `localhost`, matching the redirect
   URI) and sign in. You arrive with no rights at all: "You don't have
   access to this". That's right, and it shows Glowpod acts as you, not as
   the kubeconfig's user.
5. Give your provider identity some rights and refresh. Read-only, for
   example:

   ```bash
   kubectl create clusterrolebinding sso-test-view --clusterrole=view --user=alice@example.com
   kubectl create clusterrole sso-test-cluster-read --verb=get,list,watch --resource=namespaces,nodes
   kubectl create clusterrolebinding sso-test-cluster-read --clusterrole=sso-test-cluster-read --user=alice@example.com
   ```

   Glowpod now shows the cluster, but no Scale, Restart, Edit or Delete
   buttons. Delete the test bindings when you're done, and the test client
   in your provider if you won't use it again.

### Provider examples

**Okta**

```yaml
auth:
  oidc:
    enabled: true
    name: Okta
    issuer: https://example.okta.com/oauth2/default   # or https://example.okta.com
    clientID: <client id>
    existingSecret: glowpod-oidc
    scopes: [openid, email, profile, groups]
```

In the app's *Sign On* tab, add a **groups claim** to the ID token (filter,
e.g. "Starts with: k8s-"). Groups arrive as `oidc:k8s-payments`.

**Microsoft Entra ID (Azure AD)**

```yaml
auth:
  oidc:
    enabled: true
    name: Microsoft
    issuer: https://login.microsoftonline.com/<tenant-id>/v2.0
    clientID: <client id>
    existingSecret: glowpod-oidc
    scopes: [openid, email, profile]
    usernameClaim: preferred_username   # many tenants don't send email
```

Use your **tenant ID** in the issuer: the *Directory (tenant) ID* on your app
registration's overview page. Microsoft's shared addresses (`…/common/v2.0`,
`…/organizations/v2.0`, `…/consumers/v2.0`) don't work: they publish
`{tenantid}` as their issuer, which no token ever matches, so Glowpod refuses
to start with them and says why. Register the app as *single tenant*.

Register an app (*Web*, redirect URI as above), create a client secret, and
in *Token configuration* add a **groups claim** (security groups). Entra
sends group **object IDs**, so bind RBAC to them:
`--group=oidc:3f2a…`. People in more than about 200 groups get no `groups`
claim; assign groups to the application to stay under the limit.

**Google**

```yaml
auth:
  oidc:
    enabled: true
    name: Google
    issuer: https://accounts.google.com
    clientID: <client id>
    existingSecret: glowpod-oidc
    scopes: [openid, email, profile]
```

Tested with Glowpod 0.3.0 and a personal Google account. Set it up in
[Google Cloud Console](https://console.cloud.google.com):

1. Create (or pick) a project.
2. **Google Auth Platform → Get started** (older consoles: *APIs & Services
   → OAuth consent screen*): an app name, a support email, and the
   **Audience**: *Internal* for your Google Workspace organization only, or
   *External*.
3. With *External* in testing mode, add the people who may sign in under
   **Audience → Test users**. Publish the app when everyone should be able
   to.
4. **Clients → Create client**: type **Web application**, and under
   **Authorized redirect URIs** add `<publicURL>/auth/oidc/callback`.
   Copy the **Client ID**; keep the **Client secret** in a Secret.

Glowpod signs people in by their Google email, so bind RBAC to users:
`--user=alice@example.com`. Google doesn't send groups; to use Google
Workspace groups, put Dex in front of Google.

**Keycloak**

```yaml
auth:
  oidc:
    enabled: true
    name: Keycloak
    issuer: https://keycloak.example.com/realms/platform
    clientID: <client id>
    existingSecret: glowpod-oidc
    scopes: [openid, email, profile]
```

Create an OpenID Connect client with *Client authentication* on. In its
dedicated scope, add a **Group Membership** mapper, token claim name
`groups`, *Full group path* off, *Add to ID token* on.

**Dex**

```yaml
auth:
  oidc:
    enabled: true
    name: Dex
    issuer: https://dex.example.com
    clientID: <client id>
    existingSecret: glowpod-oidc
    scopes: [openid, email, profile, groups]
```

Add Glowpod as a `staticClients` entry with the redirect URI. Dex can in turn
sign people in with LDAP, GitHub, GitLab, SAML and more; the
[SSO demo](../deploy/examples/sso-demo/README.md) is a working Dex + LDAP
setup.

**GitLab**

```yaml
auth:
  oidc:
    enabled: true
    name: GitLab
    issuer: https://gitlab.example.com
    clientID: <client id>
    existingSecret: glowpod-oidc
    scopes: [openid, email, profile]
    groupsClaim: groups_direct
```

**Authentik / Auth0 / others:** use the issuer from the provider's
`/.well-known/openid-configuration` (without that suffix).

### Impersonation or passing the token

By default Glowpod **impersonates** the user, which works on every cluster,
including managed ones (EKS, GKE, AKS) whose API servers you can't configure.

If your API server already trusts the same issuer and client ID
(`--oidc-issuer-url`, `--oidc-client-id`, or a structured
`AuthenticationConfiguration`), set:

```yaml
auth:
  oidc:
    passToken: true
```

Glowpod then sends the ID token itself and refreshes it as it expires, the API
server maps claims to the user and groups by its own rules (so bind RBAC to
*its* prefixes), and Glowpod's service account needs no rights at all.

## LDAP and Active Directory

People type their directory username and password into Glowpod. Glowpod finds
the user with a search, checks the password by **binding as that user**, then
looks up their groups.

### 1. A search account

Create a read-only directory account for Glowpod (or leave `bindDN` empty if
your directory allows anonymous search) and store its password:

```bash
kubectl -n glowpod create secret generic glowpod-ldap --from-literal=bind-password='<password>'
```

### 2. Turn it on

OpenLDAP, FreeIPA, lldap and similar:

```yaml
auth:
  ldap:
    enabled: true
    url: ldaps://ldap.example.com:636
    bindDN: cn=glowpod,ou=services,dc=example,dc=com
    existingSecret: glowpod-ldap         # key: bind-password
    userBase: ou=people,dc=example,dc=com
    userFilter: (uid={username})
    usernameAttribute: uid
    groupBase: ou=groups,dc=example,dc=com
    groupFilter: (member={dn})           # groupOfNames; posixGroup: (memberUid={username})
    groupAttribute: cn
    groupsPrefix: "ldap:"
```

Active Directory:

```yaml
auth:
  ldap:
    enabled: true
    url: ldaps://dc1.corp.example.com:636
    bindDN: CN=svc-glowpod,OU=Service Accounts,DC=corp,DC=example,DC=com
    existingSecret: glowpod-ldap
    userBase: OU=Staff,DC=corp,DC=example,DC=com
    userFilter: (&(objectClass=user)(sAMAccountName={username}))
    usernameAttribute: sAMAccountName
    groupBase: OU=Groups,DC=corp,DC=example,DC=com
    # Nested groups too (AD's "in chain" matching rule):
    groupFilter: (member:1.2.840.113556.1.4.1941:={dn})
    groupAttribute: cn
    groupsPrefix: "ldap:"
```

`{username}` and `{dn}` are escaped before they go into a filter.

### TLS

- `ldaps://` (port 636) is TLS from the start; `ldap://` with
  `startTLS: true` upgrades the connection. Plain `ldap://` sends passwords
  unencrypted: only use it inside the cluster for testing.
- For a private CA, put it in a Secret and point the chart at it:

  ```bash
  kubectl -n glowpod create secret generic ldap-ca --from-file=ca.crt=corp-root-ca.pem
  ```

  ```yaml
  auth:
    ldap:
      caSecret: ldap-ca
  ```

- `insecureSkipVerify: true` turns off certificate checks. Testing only.

### What Glowpod does to keep this safe

- Refuses empty passwords (many directories treat them as an anonymous bind).
- Gives the same answer for an unknown user and a wrong password.
- Limits sign-in attempts per client address and per username.
- Never logs passwords; the bind password comes from a Secret through the
  environment, not the command line.

## Troubleshooting sign-in

The sign-in page shows a short message; Glowpod's log says why (search for
the `msg` values below).

| Message on the sign-in page | Log | Usual cause |
| --- | --- | --- |
| Your sign-in provider can't be reached right now. | `SSO provider unavailable` | Glowpod's pod can't reach the issuer URL: DNS, egress NetworkPolicy, proxy, or a private CA. If the log says the issuer "did not match", `issuer` isn't exactly what the provider publishes (a trailing slash, or Microsoft's `common` address instead of your tenant ID) |
| Sign-in was cancelled or refused by your provider. | `SSO refused` | The person isn't assigned to the app, or cancelled |
| That sign-in took too long, or was finished in another browser. | — | More than 10 minutes on the provider's page, or cookies blocked |
| Couldn't finish signing in with your provider. | `SSO code exchange failed` / `SSO ID token rejected` | Wrong client secret, redirect URI mismatch, or the issuer in the token differs from `issuer` (watch trailing slashes) |
| Your account didn't come with a verified email… | `SSO user not accepted` | No `usernameClaim` in the ID token, an unverified email, or a `system:` username. Try `usernameClaim: preferred_username` or `sub` |
| That username and password didn't work. | — | Wrong password, user not under `userBase`, or `userFilter` matches none or several entries |
| Couldn't reach the directory. | `LDAP sign-in failed` | Glowpod can't connect or bind to the LDAP server: URL, port, TLS/CA, or the search account's DN and password |
| Too many sign-in attempts. | — | Rate limit: 10 tries per 5 minutes per username or token, 30 per client address. Behind a proxy, set `trustedProxies` so everyone isn't counted as the proxy's address |
| The cluster stopped accepting your sign-in. | — | A token expired or was revoked, or the impersonation ClusterRole is missing |

Signed in but see nothing? That's RBAC: check with
`kubectl auth can-i --list --as=<user> --as-group=<prefixed group>`. The
groups Glowpod used are in the avatar menu in Deep mode.
