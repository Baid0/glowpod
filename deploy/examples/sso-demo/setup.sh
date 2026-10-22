#!/usr/bin/env bash
# Sets up the SSO demo in its own namespace: lldap (an LDAP server) with two
# people, Dex (OIDC) signing in against it, their permissions, and the
# secrets Glowpod needs. Safe to run again. Then install Glowpod; see README.md.
#
#   BASE_DOMAIN=192.168.64.240.nip.io ./setup.sh
#
# BASE_DOMAIN: a wildcard DNS name pointing at your ingress controller.
# nip.io does that for any IP: <ingress-ip>.nip.io.
set -euo pipefail

NS=${NS:-glowpod-demo}
BASE_DOMAIN=${BASE_DOMAIN:?set BASE_DOMAIN, e.g. 192.168.64.240.nip.io}
here=$(cd "$(dirname "$0")" && pwd)

render() { sed "s/\${BASE_DOMAIN}/${BASE_DOMAIN}/g" "$1"; }
random() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${1:-24}" || true; }
k() { kubectl -n "$NS" "$@"; }
# Creates a secret unless it exists, so re-running keeps the same passwords.
secret() { k get secret "$1" >/dev/null 2>&1 || k create secret generic "$@" >/dev/null; }
secret_value() { k get secret "$1" -o "jsonpath={.data.$2}" | base64 -d; }
json() { python3 -c 'import json, sys; print(json.dumps(dict(zip(sys.argv[1::2], sys.argv[2::2]))))' "$@"; }

echo "→ namespace $NS"
kubectl get namespace "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS" >/dev/null
kubectl label namespace "$NS" purpose=glowpod-demo --overwrite >/dev/null

echo "→ secrets (random; kept if they exist)"
secret lldap --from-literal=admin-password="$(random)" \
  --from-literal=jwt-secret="$(random 32)" --from-literal=key-seed="$(random 32)"
secret demo-users --from-literal=alice="$(random 16)" --from-literal=bob="$(random 16)"
secret glowpod-oidc --from-literal=client-secret="$(random 32)"
admin=$(secret_value lldap admin-password)
secret glowpod-ldap --from-literal=bind-password="$admin"

echo "→ lldap"
k apply -f "$here/lldap.yaml" >/dev/null
k rollout status deploy/lldap --timeout=180s >/dev/null

echo "→ people and groups: alice (devs), bob (ops)"
k port-forward svc/lldap 17170:17170 >/dev/null 2>&1 &
forward=$!
trap 'kill $forward 2>/dev/null || true' EXIT
for _ in $(seq 30); do curl -sf localhost:17170/health >/dev/null && break; sleep 1; done
token=$(curl -sf localhost:17170/auth/simple/login -H 'Content-Type: application/json' \
  -d "$(json username admin password "$admin")" | python3 -c 'import json, sys; print(json.load(sys.stdin)["token"])')
graphql() {
  curl -sf localhost:17170/api/graphql -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' -d "$(json query "$1")"
}
group_id() {
  graphql '{ groups { id displayName } }' | python3 -c '
import json, sys
groups = json.load(sys.stdin)["data"]["groups"]
print(next((str(g["id"]) for g in groups if g["displayName"] == sys.argv[1]), ""))' "$1"
}
for person in alice:devs bob:ops; do
  user=${person%%:*} group=${person##*:}
  name="$(printf %s "${user:0:1}" | tr '[:lower:]' '[:upper:]')${user:1}"
  graphql "mutation { createUser(user: {id: \"$user\", email: \"$user@example.com\", displayName: \"$name\"}) { id } }" >/dev/null || true
  [ -n "$(group_id "$group")" ] || graphql "mutation { createGroup(name: \"$group\") { id } }" >/dev/null
  graphql "mutation { addUserToGroup(userId: \"$user\", groupId: $(group_id "$group")) { ok } }" >/dev/null || true
  k exec deploy/lldap -- /app/lldap_set_password --base-url http://localhost:17170 \
    --admin-username admin --admin-password "$admin" \
    --username "$user" --password "$(secret_value demo-users "$user")" >/dev/null
done

echo "→ dex"
render "$here/dex-config.yaml.tmpl" |
  GLOWPOD_CLIENT_SECRET=$(secret_value glowpod-oidc client-secret) LLDAP_ADMIN_PASSWORD=$admin \
    python3 -c 'import os, sys; s = sys.stdin.read()
for k in ("GLOWPOD_CLIENT_SECRET", "LLDAP_ADMIN_PASSWORD"):
    s = s.replace("${" + k + "}", os.environ[k])
sys.stdout.write(s)' |
  k create secret generic dex --from-file=config.yaml=/dev/stdin --dry-run=client -o yaml | k apply -f - >/dev/null
render "$here/dex.yaml" | k apply -f - >/dev/null
k rollout restart deploy/dex >/dev/null
k rollout status deploy/dex --timeout=180s >/dev/null

echo "→ permissions: devs view, ops edit (in $NS)"
k apply -f "$here/rbac.yaml" >/dev/null

cat <<NEXT

Done. Install Glowpod into $NS:

  sed "s/\\\${BASE_DOMAIN}/$BASE_DOMAIN/g" $here/values.yaml |
    helm upgrade --install glowpod deploy/helm/glowpod -n $NS -f - --set image.repository=YOUR_REGISTRY/glowpod

Then open http://glowpod.$BASE_DOMAIN and sign in as alice or bob.
Their passwords:  kubectl -n $NS get secret demo-users -o jsonpath='{.data.alice}' | base64 -d
NEXT
