import { IconFolderPlus, IconUserPlus } from "@tabler/icons-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { useAccess } from "../../lib/access";
import { api, ApiError } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { Project, ProjectMember, ProjectRole, Subject } from "../../lib/types";
import styles from "./Access.module.css";
import { ConfirmDialog, SubjectDialog, type RoleOption } from "./SubjectDialog";

const RBAC = "rbac.authorization.k8s.io";

const PROJECT_ROLES: readonly RoleOption<ProjectRole>[] = [
  {
    value: "owner",
    label: "Owner",
    description: "everything in the project, including who has access (admin)",
  },
  { value: "member", label: "Member", description: "changes workloads and config (edit)" },
  { value: "read-only", label: "Read-only", description: "sees, doesn't change (view)" },
];

const ROLE_WORDS: Record<ProjectRole, string> = {
  owner: "Owner",
  member: "Member",
  "read-only": "Read-only",
};

type Editing =
  | { kind: "new-project" }
  | { kind: "namespaces"; project: Project }
  | { kind: "member"; project: Project; member?: ProjectMember }
  | { kind: "remove-member"; project: Project; member: ProjectMember }
  | { kind: "delete"; project: Project };

/**
 * Projects are named groups of namespaces, such as payments = payments-api
 * + payments-db. A project's members get a role binding in each of its
 * namespaces; namespaces added later get them too.
 */
export function ProjectsPage() {
  const cluster = useClusterId();
  const client = useQueryClient();
  const { data, error, isPending } = useQuery({
    queryKey: queryKeys.projects(cluster),
    queryFn: ({ signal }) => api.projects(cluster, signal),
  });
  const projects = data?.projects ?? [];
  // Projects are labels on namespaces; members are role bindings in each.
  const access = useAccess([
    { verb: "patch", group: "", resource: "namespaces" },
    ...projects.map((p) => ({
      verb: "create" as const,
      group: RBAC,
      resource: "rolebindings",
      namespace: p.namespaces[0] ?? "",
    })),
  ]);
  const canLabel = access[0] ?? false;
  const [editing, setEditing] = useState<Editing | undefined>();
  const close = () => {
    setEditing(undefined);
  };
  const refresh = () => client.invalidateQueries({ queryKey: queryKeys.projects(cluster) });

  return (
    <div className={styles.page}>
      <p className={styles.intro}>
        A project is a named group of namespaces. Its members get the same role in each of them, as
        role bindings in those namespaces.
      </p>
      {canLabel && (
        <div>
          <Button
            icon={<IconFolderPlus size={16} stroke={1.75} />}
            onClick={() => {
              setEditing({ kind: "new-project" });
            }}
          >
            New project
          </Button>
        </div>
      )}
      {isPending ? (
        <p className={styles.empty}>Loading…</p>
      ) : error ? (
        <p className={styles.error} role="alert">
          {error instanceof ApiError ? error.message : "Couldn't load the projects."}
        </p>
      ) : (
        <>
          {projects.length === 0 && (
            <p className={styles.empty}>
              No projects yet.{canLabel && " Make one to give a team its namespaces."}
            </p>
          )}
          {projects.map((p, i) => (
            <ProjectCard
              key={p.name}
              project={p}
              canLabel={canLabel}
              canManage={access[i + 1] ?? false}
              onEdit={setEditing}
              onFix={async (m) => {
                await api.setProjectMember(cluster, p.name, {
                  kind: m.kind,
                  name: m.name,
                  role: m.role,
                });
                await refresh();
              }}
            />
          ))}
          {data.unassigned.length > 0 && (
            <Card title="Not in a project">
              <ul className={styles.chips} aria-label="Namespaces not in a project">
                {data.unassigned.map((ns) => (
                  <li key={ns} className={styles.chip}>
                    {ns}
                  </li>
                ))}
              </ul>
            </Card>
          )}
        </>
      )}

      {(editing?.kind === "new-project" || editing?.kind === "namespaces") && (
        <NamespacesDialog
          project={editing.kind === "namespaces" ? editing.project : undefined}
          unassigned={data?.unassigned ?? []}
          onClose={close}
          save={async (name, namespaces) => {
            if (editing.kind === "namespaces") {
              await api.setProjectNamespaces(cluster, name, namespaces);
            } else {
              await api.createProject(cluster, name, namespaces);
            }
            await refresh();
          }}
        />
      )}
      {editing?.kind === "member" && (
        <SubjectDialog
          key={editing.member ? `${editing.member.kind}/${editing.member.name}` : "new"}
          open
          onClose={close}
          title={editing.member ? "Change the role" : `Add a member to ${editing.project.name}`}
          roles={PROJECT_ROLES}
          {...(editing.member
            ? { editing: editing.member, initialRole: editing.member.role }
            : { initialRole: "member" as const })}
          save={async (s: Subject, role) => {
            await api.setProjectMember(cluster, editing.project.name, { ...s, role });
            await refresh();
          }}
        />
      )}
      {editing?.kind === "remove-member" && (
        <ConfirmDialog
          open
          onClose={close}
          title={`Remove ${editing.member.name} from ${editing.project.name}?`}
          description={`Their access to ${editing.project.namespaces.join(", ")} through this project ends.`}
          action="Remove"
          run={async () => {
            await api.removeProjectMember(cluster, editing.project.name, editing.member);
            await refresh();
          }}
        />
      )}
      {editing?.kind === "delete" && (
        <ConfirmDialog
          open
          onClose={close}
          title={`Delete project ${editing.project.name}?`}
          description={`Its namespaces leave the project and its members lose the access it gave them. The namespaces, and everything in them, stay.`}
          action="Delete project"
          run={async () => {
            await api.deleteProject(cluster, editing.project.name);
            await refresh();
          }}
        />
      )}
    </div>
  );
}

function ProjectCard({
  project: p,
  canLabel,
  canManage,
  onEdit,
  onFix,
}: {
  project: Project;
  canLabel: boolean;
  canManage: boolean;
  onEdit: (e: Editing) => void;
  onFix: (m: ProjectMember) => Promise<void>;
}) {
  const [fixing, setFixing] = useState("");
  return (
    <Card
      title={p.name}
      actions={
        <div className={styles.actions}>
          {canManage && (
            <Button
              size="sm"
              icon={<IconUserPlus size={16} stroke={1.75} />}
              onClick={() => {
                onEdit({ kind: "member", project: p });
              }}
            >
              Add member
            </Button>
          )}
          {canLabel && (
            <>
              <Button
                size="sm"
                onClick={() => {
                  onEdit({ kind: "namespaces", project: p });
                }}
              >
                Namespaces
              </Button>
              <Button
                size="sm"
                aria-label={`Delete project ${p.name}`}
                onClick={() => {
                  onEdit({ kind: "delete", project: p });
                }}
              >
                Delete
              </Button>
            </>
          )}
        </div>
      }
    >
      <ul className={styles.chips} aria-label={`Namespaces in ${p.name}`}>
        {p.namespaces.map((ns) => (
          <li key={ns} className={styles.chip}>
            {ns}
          </li>
        ))}
      </ul>
      {p.members.length === 0 ? (
        <p className={styles.empty}>No members yet.</p>
      ) : (
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Type</th>
              <th scope="col">Role</th>
              {canManage && <th scope="col">Actions</th>}
            </tr>
          </thead>
          <tbody>
            {p.members.map((m) => (
              <tr key={`${m.kind}/${m.name}/${m.role}`}>
                <td>
                  <span className={styles.mono}>{m.name}</span>
                  {m.missing && m.missing.length > 0 && (
                    <span className={styles.missing}>
                      Not in {m.missing.join(", ")}
                      {canManage && (
                        <Button
                          size="sm"
                          disabled={fixing === m.name}
                          onClick={() => {
                            setFixing(m.name);
                            void onFix(m).finally(() => {
                              setFixing("");
                            });
                          }}
                        >
                          Apply there too
                        </Button>
                      )}
                    </span>
                  )}
                </td>
                <td>{m.kind === "Group" ? "Group" : "Person"}</td>
                <td>{ROLE_WORDS[m.role]}</td>
                {canManage && (
                  <td>
                    <div className={styles.actions}>
                      <Button
                        size="sm"
                        aria-label={`Change ${m.name}'s role in ${p.name}`}
                        onClick={() => {
                          onEdit({ kind: "member", project: p, member: m });
                        }}
                      >
                        Change
                      </Button>
                      <Button
                        size="sm"
                        aria-label={`Remove ${m.name} from ${p.name}`}
                        onClick={() => {
                          onEdit({ kind: "remove-member", project: p, member: m });
                        }}
                      >
                        Remove
                      </Button>
                    </div>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

/** A new project's name and namespaces, or an existing one's namespaces. */
function NamespacesDialog({
  project,
  unassigned,
  onClose,
  save,
}: {
  project?: Project;
  unassigned: string[];
  onClose: () => void;
  save: (name: string, namespaces: string[]) => Promise<void>;
}) {
  const [name, setName] = useState(project?.name ?? "");
  const [picked, setPicked] = useState<string[]>(project?.namespaces ?? []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const nameId = useId();
  const choices = [...(project?.namespaces ?? []), ...unassigned].sort();

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      await save(name.trim(), picked);
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title={project ? `Namespaces in ${project.name}` : "New project"}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            variant="primary"
            disabled={busy || picked.length === 0 || !name.trim()}
          >
            {busy ? "Saving…" : project ? "Save" : "Make project"}
          </Button>
        </>
      }
    >
      <div className={styles.form}>
        {!project && (
          <div className={styles.field}>
            <label htmlFor={nameId} className={styles.label}>
              Name
            </label>
            <input
              id={nameId}
              className={styles.input}
              autoComplete="off"
              spellCheck={false}
              required
              placeholder="payments"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
              }}
            />
          </div>
        )}
        <fieldset className={styles.roles}>
          <legend className={styles.label}>Namespaces</legend>
          {project && (
            <p className={styles.hint}>
              Namespaces you add get the project&apos;s members; ones you take out lose them.
            </p>
          )}
          {choices.length === 0 ? (
            <p className={styles.empty}>Every namespace is already in a project.</p>
          ) : (
            <div className={styles.checks}>
              {choices.map((ns) => (
                <label key={ns} className={styles.check}>
                  <input
                    type="checkbox"
                    checked={picked.includes(ns)}
                    onChange={(e) => {
                      setPicked((cur) =>
                        e.target.checked ? [...cur, ns] : cur.filter((x) => x !== ns),
                      );
                    }}
                  />
                  {ns}
                </label>
              ))}
            </div>
          )}
        </fieldset>
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}
