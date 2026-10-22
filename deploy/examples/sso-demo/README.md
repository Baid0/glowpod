# SSO and LDAP demo

A complete sign-in setup in one throwaway namespace, to see how Glowpod's SSO
and LDAP sign-in work before wiring up your real identity provider:

- **lldap**, a small LDAP server, with two people:
  - `alice` in group `devs`
  - `bob` in group `ops`
- **Dex**, an OIDC provider that signs people in against lldap.
- **Glowpod**, offering three ways in: SSO through Dex, LDAP straight to
  lldap, and tokens.
- **RBAC** in the namespace: `devs` may look (`view`), `ops` may change
  things (`edit`).

```
browser ──► Glowpod ──SSO──► Dex ──► lldap
              │  └──LDAP────────────► lldap
              └──impersonates alice / bob──► Kubernetes API (their RBAC)
```

It runs over plain HTTP with `-insecure-cookies` and stores Dex state in
memory: fine for a demo, **not for real use**. See
[docs/install.md](../../../docs/install.md) for that.

## You need

- A cluster with an ingress controller that has an IP address you can reach,
  and that the cluster's pods can reach too (Glowpod talks to Dex through it).
- `kubectl`, `helm`, `curl` and `python3`.
- A Glowpod image in a registry the cluster can pull from
  (`make image-push IMAGE=registry.example.com/you/glowpod`).

Hostnames come from [nip.io](https://nip.io), which resolves
`anything.<ip>.nip.io` to `<ip>`, so there's no DNS to set up.

## Run it

From the repository root:

```bash
INGRESS_IP=192.168.64.240          # your ingress controller's address
export BASE_DOMAIN=$INGRESS_IP.nip.io

deploy/examples/sso-demo/setup.sh

sed "s/\${BASE_DOMAIN}/$BASE_DOMAIN/g" deploy/examples/sso-demo/values.yaml |
  helm upgrade --install glowpod deploy/helm/glowpod -n glowpod-demo -f - \
    --set image.repository=registry.example.com/you/glowpod --set image.tag=0.1.0

helm test glowpod -n glowpod-demo
```

`setup.sh` creates the namespace `glowpod-demo` (override with `NS=...`),
random passwords in Secrets, the two people, Dex's config and the
RoleBindings. Running it again keeps the same passwords.

## Sign in

Open `http://glowpod.<ingress-ip>.nip.io`. Read the passwords from the
cluster:

```bash
kubectl -n glowpod-demo get secret demo-users -o jsonpath='{.data.alice}' | base64 -d; echo
kubectl -n glowpod-demo get secret demo-users -o jsonpath='{.data.bob}' | base64 -d; echo
```

| Sign in as | With | Glowpod sees | In `glowpod-demo` |
| --- | --- | --- | --- |
| alice | **Sign in with Dex** | `alice@example.com`, group `oidc:devs` | Can look, no change buttons |
| bob | **Sign in with Dex** | `bob@example.com`, group `oidc:ops` | Can scale, restart, edit, delete |
| alice | **LDAP** form | `alice`, group `ldap:devs` | Can look |
| bob | **LDAP** form | `bob`, group `ldap:ops` | Can change things |

Neither can see other namespaces: they only have RoleBindings here, so type
`glowpod-demo` in the namespace picker. Switch to Deep mode and open the
avatar menu to see the groups Glowpod signed you in with.

Try a token too:

```bash
kubectl -n glowpod-demo create serviceaccount demo-viewer
kubectl -n glowpod-demo create rolebinding demo-viewer --clusterrole=view --serviceaccount=glowpod-demo:demo-viewer
kubectl -n glowpod-demo create token demo-viewer --duration=1h
```

## How the pieces map to a real setup

| Demo | Real install |
| --- | --- |
| `auth.oidc.issuer: http://dex.<domain>` | Your provider's issuer (Okta, Entra ID, Keycloak, …) over HTTPS |
| Dex static client `glowpod`, secret in `glowpod-oidc` | An app registered with your provider |
| `auth.ldap.url: ldap://lldap:3890` | `ldaps://` to your directory or AD |
| Bind as lldap's admin | A read-only search account |
| `rbac.yaml` | RoleBindings for your teams' groups |
| `-insecure-cookies`, `http://` | HTTPS with a real certificate |

[docs/authentication.md](../../../docs/authentication.md) has the settings for
each provider.

## Clean up

```bash
helm uninstall glowpod -n glowpod-demo
kubectl delete namespace glowpod-demo
```
