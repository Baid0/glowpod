import { IconUserPlus } from "@tabler/icons-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { useAccess } from "../../lib/access";
import { api, ApiError } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { ClusterRoleChoice, Member, Subject } from "../../lib/types";
import styles from "./Access.module.css";
import { ConfirmDialog, SubjectDialog, type RoleOption } from "./SubjectDialog";

const RBAC = "rbac.authorization.k8s.io";

const CLUSTER_ROLES: readonly RoleOption<ClusterRoleChoice>[] = [
  { value: "owner", label: "Owner", description: "everything in the cluster (cluster-admin)" },
  { value: "read-only", label: "Read-only", description: "sees everything but Secrets (view)" },
  { value: "custom", label: "Custom", description: "any ClusterRole in the cluster" },
];

const ROLE_WORDS: Record<ClusterRoleChoice, string> = {
  owner: "Owner",
  "read-only": "Read-only",
  custom: "Custom",
};

/**
 * Who has access to the whole cluster. Glowpod's members are bindings it
 * made, as you; others came from kubectl, Helm or GitOps and are shown as
 * they are.
 */
export function MembersPage() {
  const cluster = useClusterId();
  const client = useQueryClient();
  const { data, error, isPending } = useQuery({
    queryKey: queryKeys.members(cluster),
    queryFn: ({ signal }) => api.members(cluster, signal),
  });
  const [canAdd = false, canRemove = false] = useAccess([
    { verb: "create", group: RBAC, resource: "clusterrolebindings" },
    { verb: "delete", group: RBAC, resource: "clusterrolebindings" },
  ]);
  const [editing, setEditing] = useState<Member | "new" | undefined>();
  const [removing, setRemoving] = useState<Member | undefined>();
  const refresh = () => client.invalidateQueries({ queryKey: queryKeys.members(cluster) });

  return (
    <div className={styles.page}>
      <p className={styles.intro}>
        Access to the whole cluster, stored as its own role bindings. You can only give what you
        have yourself; the cluster checks.
      </p>
      <Card
        title="Cluster members"
        actions={
          canAdd && (
            <Button
              size="sm"
              icon={<IconUserPlus size={16} stroke={1.75} />}
              onClick={() => {
                setEditing("new");
              }}
            >
              Add member
            </Button>
          )
        }
      >
        {isPending ? (
          <p className={styles.empty}>Loading…</p>
        ) : error ? (
          <p className={styles.error} role="alert">
            {error instanceof ApiError ? error.message : "Couldn't load the members."}
          </p>
        ) : data.members.length === 0 ? (
          <p className={styles.empty}>
            No one has been added here yet.{canAdd && " Add a person or a group."}
          </p>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Type</th>
                <th scope="col">Role</th>
                {(canAdd || canRemove) && <th scope="col">Actions</th>}
              </tr>
            </thead>
            <tbody>
              {data.members.map((m) => (
                <tr key={`${m.kind}/${m.name}`}>
                  <td className={styles.mono}>{m.name}</td>
                  <td>{m.kind === "Group" ? "Group" : "Person"}</td>
                  <td>
                    {m.role ? ROLE_WORDS[m.role] : "Custom"}
                    {(m.role === "custom" || !m.role) && (
                      <span className={styles.mono}> ({m.clusterRole})</span>
                    )}
                  </td>
                  {(canAdd || canRemove) && (
                    <td>
                      <div className={styles.actions}>
                        {canAdd && (
                          <Button
                            size="sm"
                            aria-label={`Change ${m.name}'s role`}
                            onClick={() => {
                              setEditing(m);
                            }}
                          >
                            Change
                          </Button>
                        )}
                        {canRemove && (
                          <Button
                            size="sm"
                            aria-label={`Remove ${m.name}`}
                            onClick={() => {
                              setRemoving(m);
                            }}
                          >
                            Remove
                          </Button>
                        )}
                      </div>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {data && data.others.length > 0 && (
        <Card title="Other access">
          <p className={styles.intro}>
            Made with kubectl, Helm or GitOps. Change these where they were made.
          </p>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Type</th>
                <th scope="col">ClusterRole</th>
                <th scope="col">Binding</th>
              </tr>
            </thead>
            <tbody>
              {data.others.map((m) => (
                <tr key={`${m.binding}/${m.kind}/${m.name}`}>
                  <td className={styles.mono}>{m.name}</td>
                  <td>{m.kind === "Group" ? "Group" : "Person"}</td>
                  <td className={styles.mono}>{m.clusterRole}</td>
                  <td className={styles.mono}>{m.binding}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {editing && (
        <SubjectDialog
          key={editing === "new" ? "new" : `${editing.kind}/${editing.name}`}
          open
          onClose={() => {
            setEditing(undefined);
          }}
          title={editing === "new" ? "Add a cluster member" : "Change the role"}
          roles={CLUSTER_ROLES}
          custom="custom"
          {...(editing === "new"
            ? { initialRole: "read-only" as const }
            : {
                editing,
                initialRole: editing.role ?? "custom",
                initialClusterRole: editing.role === "custom" ? editing.clusterRole : "",
              })}
          save={async (s: Subject, role, clusterRole) => {
            await api.setMember(cluster, {
              ...s,
              role,
              ...(role === "custom" ? { clusterRole } : {}),
            });
            await refresh();
          }}
        />
      )}
      {removing && (
        <ConfirmDialog
          open
          onClose={() => {
            setRemoving(undefined);
          }}
          title={`Remove ${removing.name}?`}
          description={`Their ${removing.role ? ROLE_WORDS[removing.role].toLowerCase() : ""} access to the whole cluster ends. Access they have from projects or other bindings stays.`}
          action="Remove"
          run={async () => {
            await api.removeMember(cluster, removing);
            await refresh();
          }}
        />
      )}
    </div>
  );
}
