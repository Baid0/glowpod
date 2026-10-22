import { IconMinus, IconPlus } from "@tabler/icons-react";
import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import type { ResourceRef } from "../../lib/types";
import styles from "./actions.module.css";
import { failure, useChange } from "./useActions";

interface Target {
  resource: ResourceRef;
  namespace: string;
  name: string;
  /** Singular, lower case: "deployment", "pod". */
  kind: string;
}

interface DialogBase extends Target {
  open: boolean;
  onClose: () => void;
}

/** The cluster's refusal, inside the dialog that asked. */
export function ChangeError({ error }: { error: unknown }) {
  if (!error) return null;
  const { message, detail } = failure(error);
  return (
    <div className={styles.error} role="alert">
      <p>{message}</p>
      {detail && <pre className={styles.detail}>{detail}</pre>}
    </div>
  );
}

export function DeleteDialog({
  open,
  onClose,
  onDeleted,
  consequence,
  ...t
}: DialogBase & { onDeleted?: () => void; consequence?: string }) {
  const cluster = useClusterId();
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const change = useChange(
    () => api.deleteResource(cluster, t.resource, t.namespace, t.name),
    () => `Deleted ${t.kind} ${t.name}.`,
  );
  const close = () => {
    setTyped("");
    change.reset();
    onClose();
  };
  const matches = typed === t.name;
  const submit = () => {
    if (!matches || change.isPending) return;
    change.mutate(undefined, {
      onSuccess: () => {
        close();
        onDeleted?.();
      },
    });
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      onSubmit={submit}
      title={`Delete ${t.kind} ${t.name}?`}
      description={
        <p>
          {consequence ?? "This can't be undone."}
          {t.namespace && <> It's in the {t.namespace} namespace.</>}
        </p>
      }
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="danger" disabled={!matches || change.isPending}>
            {change.isPending ? "Deleting…" : "Delete"}
          </Button>
        </>
      }
    >
      <div className={styles.field}>
        <label htmlFor={inputId} className={styles.label}>
          Type <span className={styles.mono}>{t.name}</span> to confirm
        </label>
        <input
          id={inputId}
          className={styles.input}
          autoComplete="off"
          spellCheck={false}
          autoFocus
          value={typed}
          onChange={(e) => {
            setTyped(e.target.value);
          }}
        />
      </div>
      <ChangeError error={change.error} />
    </Dialog>
  );
}

export function ScaleDialog({ open, onClose, current, ...t }: DialogBase & { current: number }) {
  const cluster = useClusterId();
  const [value, setValue] = useState(String(current));
  const inputId = useId();
  const hintId = useId();
  const change = useChange(
    (n: number) => api.scale(cluster, t.resource, t.namespace, t.name, n),
    (n) => `Scaled ${t.name} to ${String(n)} ${n === 1 ? "replica" : "replicas"}.`,
  );
  const n = Number(value);
  const valid = value.trim() !== "" && Number.isInteger(n) && n >= 0 && n <= 10_000;
  const close = () => {
    setValue(String(current));
    change.reset();
    onClose();
  };
  const submit = () => {
    if (!valid || n === current || change.isPending) return;
    change.mutate(n, { onSuccess: close });
  };
  const step = (by: number) => {
    setValue(String(Math.max(0, (valid ? n : current) + by)));
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      onSubmit={submit}
      title={`Scale ${t.name}`}
      description={
        <p>
          It runs {current} {current === 1 ? "replica" : "replicas"} now.
        </p>
      }
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            type="submit"
            variant="primary"
            disabled={!valid || n === current || change.isPending}
          >
            {change.isPending ? "Scaling…" : valid ? `Scale to ${String(n)}` : "Scale"}
          </Button>
        </>
      }
    >
      <div className={styles.field}>
        <label htmlFor={inputId} className={styles.label}>
          Replicas
        </label>
        <div className={styles.stepper}>
          <button
            type="button"
            className={styles.stepButton}
            aria-label="One fewer"
            disabled={valid && n <= 0}
            onClick={() => {
              step(-1);
            }}
          >
            <IconMinus size={16} stroke={1.75} aria-hidden="true" />
          </button>
          <input
            id={inputId}
            className={`${styles.input} ${styles.number}`}
            inputMode="numeric"
            autoFocus
            aria-describedby={hintId}
            aria-invalid={!valid}
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
            }}
          />
          <button
            type="button"
            className={styles.stepButton}
            aria-label="One more"
            onClick={() => {
              step(1);
            }}
          >
            <IconPlus size={16} stroke={1.75} aria-hidden="true" />
          </button>
        </div>
        <p id={hintId} className={styles.hint}>
          {!valid
            ? "Use a whole number from 0 to 10,000."
            : n === 0
              ? `Scaling to 0 stops every pod. The ${t.kind} stays, so you can scale it back up.`
              : "Pods are added or removed now."}
        </p>
      </div>
      <ChangeError error={change.error} />
    </Dialog>
  );
}

export function RestartDialog({ open, onClose, ...t }: DialogBase) {
  const cluster = useClusterId();
  const change = useChange(
    () => api.restart(cluster, t.resource, t.namespace, t.name),
    () => `Restarting ${t.name}. New pods replace the old ones one by one.`,
  );
  const close = () => {
    change.reset();
    onClose();
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      onSubmit={() => {
        if (!change.isPending) change.mutate(undefined, { onSuccess: close });
      }}
      title={`Restart ${t.name}?`}
      description={
        <p>
          Every pod is replaced with a fresh one, a few at a time, so the app keeps serving while it
          has more than one replica (rolling restart).
        </p>
      }
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={change.isPending} autoFocus>
            {change.isPending ? "Restarting…" : "Restart"}
          </Button>
        </>
      }
    >
      <ChangeError error={change.error} />
    </Dialog>
  );
}
