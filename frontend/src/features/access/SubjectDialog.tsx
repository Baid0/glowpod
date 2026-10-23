import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { Segmented } from "../../components/Segmented";
import { ApiError } from "../../lib/api";
import type { Subject } from "../../lib/types";
import styles from "./Access.module.css";

export interface RoleOption<R extends string> {
  value: R;
  label: string;
  description: string;
}

const KINDS = [
  { value: "User", label: "Person" },
  { value: "Group", label: "Group" },
] as const;

/**
 * Add someone, or change their role: a person or group as Kubernetes sees
 * them, and one of roles. custom, when given, is the role that asks for a
 * ClusterRole's name.
 */
export function SubjectDialog<R extends string>({
  open,
  onClose,
  title,
  roles,
  editing,
  initialRole,
  initialClusterRole = "",
  custom,
  save,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  roles: readonly RoleOption<R>[];
  /** Changing an existing member: their name and kind stay. */
  editing?: Subject;
  initialRole: R;
  initialClusterRole?: string;
  custom?: R;
  save: (s: Subject, role: R, clusterRole: string) => Promise<void>;
}) {
  const [kind, setKind] = useState<Subject["kind"]>(editing?.kind ?? "User");
  const [name, setName] = useState(editing?.name ?? "");
  const [role, setRole] = useState<R>(initialRole);
  const [clusterRole, setClusterRole] = useState(initialClusterRole);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const nameId = useId();
  const hintId = useId();
  const crId = useId();

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      await save({ kind, name: name.trim() }, role, clusterRole.trim());
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      <div className={styles.form}>
        {editing ? (
          <p className={styles.mono}>
            {editing.kind === "Group" ? "Group " : ""}
            {editing.name}
          </p>
        ) : (
          <>
            <Segmented label="Person or group" options={KINDS} value={kind} onChange={setKind} />
            <div className={styles.field}>
              <label htmlFor={nameId} className={styles.label}>
                {kind === "Group" ? "Group" : "Name"}
              </label>
              <input
                id={nameId}
                className={styles.input}
                aria-describedby={hintId}
                autoComplete="off"
                spellCheck={false}
                required
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                }}
              />
              <p id={hintId} className={styles.hint}>
                As Kubernetes sees {kind === "Group" ? "it" : "them"}:{" "}
                {kind === "Group"
                  ? "oidc:payments or ldap:ops"
                  : "oidc:ann@example.com, ldap:bob or glowpod:admin"}
                .
              </p>
            </div>
          </>
        )}
        <fieldset className={styles.roles}>
          <legend className={styles.label}>Role</legend>
          {roles.map((r) => (
            <label key={r.value} className={styles.role}>
              <input
                type="radio"
                name="role"
                value={r.value}
                checked={role === r.value}
                onChange={() => {
                  setRole(r.value);
                }}
              />
              <span>
                <span className={styles.roleName}>{r.label}</span> · {r.description}
              </span>
            </label>
          ))}
        </fieldset>
        {custom !== undefined && role === custom && (
          <div className={styles.field}>
            <label htmlFor={crId} className={styles.label}>
              ClusterRole
            </label>
            <input
              id={crId}
              className={styles.input}
              autoComplete="off"
              spellCheck={false}
              required
              placeholder="edit"
              value={clusterRole}
              onChange={(e) => {
                setClusterRole(e.target.value);
              }}
            />
          </div>
        )}
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

/** Asks before something is taken away. */
export function ConfirmDialog({
  open,
  onClose,
  title,
  description,
  action,
  run,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  action: string;
  run: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const go = async () => {
    setBusy(true);
    setError("");
    try {
      await run();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      description={<p>{description}</p>}
      onSubmit={() => void go()}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Working…" : action}
          </Button>
        </>
      }
    >
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
    </Dialog>
  );
}
