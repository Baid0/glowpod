# Members and projects

Give people access to a cluster from Glowpod, the way Rancher's cluster
members and projects work, but stored only as the cluster's own RBAC. There
is no second permission system to keep in step: what Glowpod shows is what
Kubernetes enforces, and bindings made with kubectl or GitOps count too.

Needs Glowpod 0.7.0 or later. Works on a Hub, in the cluster and on your
own machine.

## Cluster members

**Cluster → Members** lists who has access to the whole cluster.

| Role | What it makes |
| --- | --- |
| Owner | A ClusterRoleBinding to `cluster-admin` |
| Read-only | A ClusterRoleBinding to `view`: sees everything but Secrets |
| Custom | A ClusterRoleBinding to any ClusterRole you name |

Add a person or a group under the name Kubernetes sees, prefix included:
`oidc:ann@example.com` or `oidc:payments` for SSO, `ldap:bob` or `ldap:ops`
for LDAP, `glowpod:admin` for a Glowpod account on a Hub (see [Sign-in and
access](authentication.md)).

**Other access** lists bindings Glowpod didn't make, from kubectl, Helm or
GitOps. They're shown so you see everyone with access; change them where
they were made.

## Projects

**Cluster → Projects**: a project is a named group of namespaces, such as
`payments` = `payments-api` + `payments-db`. Its members get a role in each
of those namespaces:

| Role | What it makes in each namespace |
| --- | --- |
| Owner | A RoleBinding to `admin`: everything, including who has access |
| Member | A RoleBinding to `edit`: changes workloads and config |
| Read-only | A RoleBinding to `view` |

- A namespace is in a project when it has the label
  `hub.glowpod.dev/project=<project>`, so the cluster itself remembers it.
  A namespace is in one project at a time.
- Adding a namespace to a project gives it the project's members. Taking it
  out removes them; the namespace and everything in it stay.
- If a member's binding is missing in one of the project's namespaces (it
  was deleted with kubectl, or the namespace was labelled by hand), the
  project shows "Not in …" with **Apply there too**.
- Deleting a project takes its namespaces out of it and removes its
  members' access there. Nothing else is deleted.

## Who may change access

Every change is made **as you**, through the cluster's own API, so
Kubernetes' rules decide: you can only give access you have yourself (or
have the `bind` right for). An Owner can add members; a Read-only member
can't. Glowpod hides the buttons you can't use, and if the cluster still
refuses, says so.

## What Glowpod makes

- Cluster members: ClusterRoleBindings named `glowpod-member-<hash>`.
- Project members: RoleBindings named `glowpod-project-<project>-<hash>` in
  each of the project's namespaces.
- Both are labelled `app.kubernetes.io/managed-by=glowpod` and
  `hub.glowpod.dev/member-role=<role>`, with the person or group in the
  `hub.glowpod.dev/subject-kind` and `hub.glowpod.dev/subject-name`
  annotations. The name is a hash because names like `oidc:ann@example.com`
  can't be part of a binding's name.
- Glowpod only changes bindings it made. A binding with one of these names
  that something else made is left alone.
- Each change is logged ("changed access") with who made it.

```bash
kubectl get clusterrolebindings,rolebindings -A -l app.kubernetes.io/managed-by=glowpod
```

On a Hub, the cluster shows up on a new member's Clusters page within half
a minute, when the agent next reports its bindings.
