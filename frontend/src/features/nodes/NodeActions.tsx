import { IconArrowsShuffle, IconLock, IconLockOpen } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { useAccess } from "../../lib/access";
import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { DrainPod, DrainResult } from "../../lib/types";
import styles from "../actions/actions.module.css";
import { ChangeError } from "../actions/dialogs";
import { useChange } from "../actions/useActions";
import nodeStyles from "./NodePage.module.css";

/** Cordon / Uncordon and Drain, for people allowed to change nodes. */
export function NodeActions({ name, cordoned }: { name: string; cordoned: boolean }) {
  const cluster = useClusterId();
  const [canPatch, canEvict] = useAccess([
    { verb: "patch", group: "", resource: "nodes", name },
    { verb: "create", group: "", resource: "pods", subresource: "eviction" },
  ]);
  const [draining, setDraining] = useState(false);
  const cordon = useChange(
    (on: boolean) => api.cordon(cluster, name, on),
    (on) =>
      on
        ? `Cordoned ${name}. New pods won't be scheduled there.`
        : `Uncordoned ${name}. It takes new pods again.`,
  );

  if (!canPatch) return null;
  return (
    <>
      <Button
        size="sm"
        icon={
          cordoned ? <IconLockOpen size={16} stroke={1.75} /> : <IconLock size={16} stroke={1.75} />
        }
        disabled={cordon.isPending}
        onClick={() => {
          cordon.mutate(!cordoned);
        }}
      >
        {cordoned ? "Uncordon" : "Cordon"}
      </Button>
      {canEvict && (
        <Button
          size="sm"
          icon={<IconArrowsShuffle size={16} stroke={1.75} />}
          onClick={() => {
            setDraining(true);
          }}
        >
          Drain
        </Button>
      )}
      {cordon.error ? <ChangeError error={cordon.error} /> : null}
      {canEvict && (
        <DrainDialog
          name={name}
          open={draining}
          onClose={() => {
            setDraining(false);
          }}
        />
      )}
    </>
  );
}

function DrainDialog({
  name,
  open,
  onClose,
}: {
  name: string;
  open: boolean;
  onClose: () => void;
}) {
  const cluster = useClusterId();
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const plan = useQuery({
    queryKey: queryKeys.drainPlan(cluster, name),
    queryFn: ({ signal }) => api.drainPlan(cluster, name, signal),
    enabled: open,
    staleTime: 0,
    gcTime: 0,
  });
  const drain = useChange(
    () => api.drain(cluster, name),
    () => `Drained ${name}.`,
  );
  const result = drain.data as DrainResult | undefined;
  const close = () => {
    setTyped("");
    drain.reset();
    onClose();
  };
  const evict = plan.data?.evict ?? [];
  const skip = plan.data?.skip ?? [];
  const localData = evict.filter((p) => p.localData).length;

  if (result) {
    return (
      <Dialog
        open={open}
        onClose={close}
        title={`Drained ${name}`}
        description={<p>{resultSummary(result)}</p>}
        footer={
          <Button variant="primary" onClick={close} autoFocus>
            Done
          </Button>
        }
      >
        <PodList title="Blocked for now" pods={result.blocked} />
        <PodList title="Couldn't move" pods={result.failed} />
        <PodList title="Left in place" pods={result.skipped} />
      </Dialog>
    );
  }

  return (
    <Dialog
      open={open}
      onClose={close}
      onSubmit={() => {
        if (typed === name && !drain.isPending) drain.mutate(undefined);
      }}
      title={`Drain ${name}?`}
      description={
        <p>
          Glowpod cordons the node, then asks each pod to move to another node. Disruption budgets
          are respected, so some pods may have to wait.
        </p>
      }
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="danger" disabled={typed !== name || drain.isPending}>
            {drain.isPending ? "Draining…" : `Drain ${String(evict.length)} pods`}
          </Button>
        </>
      }
    >
      {plan.isPending ? (
        <p className={nodeStyles.muted}>Checking which pods would move…</p>
      ) : (
        <>
          <p>
            <strong className={nodeStyles.strong}>{evict.length}</strong>{" "}
            {evict.length === 1 ? "pod moves" : "pods move"}.{" "}
            {skip.length > 0 && (
              <>
                <strong className={nodeStyles.strong}>{skip.length}</strong> stay, listed below.
              </>
            )}
          </p>
          {localData > 0 && (
            <p className={nodeStyles.warning}>
              {localData} {localData === 1 ? "pod keeps" : "pods keep"} data in a temporary volume
              (emptyDir). That data is lost when they move.
            </p>
          )}
          <PodList title="Stay on the node" pods={skip} />
        </>
      )}
      <div className={styles.field}>
        <label htmlFor={inputId} className={styles.label}>
          Type <span className={styles.mono}>{name}</span> to confirm
        </label>
        <input
          id={inputId}
          className={styles.input}
          autoComplete="off"
          spellCheck={false}
          value={typed}
          onChange={(e) => {
            setTyped(e.target.value);
          }}
        />
      </div>
      <ChangeError error={drain.error ?? plan.error} />
    </Dialog>
  );
}

function resultSummary(r: DrainResult): string {
  const parts = [
    `${String(r.evicted.length)} ${r.evicted.length === 1 ? "pod is" : "pods are"} moving.`,
  ];
  if (r.blocked.length > 0)
    parts.push(`${String(r.blocked.length)} blocked by disruption budgets.`);
  if (r.failed.length > 0) parts.push(`${String(r.failed.length)} couldn't be moved.`);
  return parts.join(" ");
}

function PodList({ title, pods }: { title: string; pods: DrainPod[] }) {
  if (pods.length === 0) return null;
  return (
    <section className={nodeStyles.podList} aria-label={title}>
      <h3 className={nodeStyles.listTitle}>{title}</h3>
      <ul>
        {pods.map((p) => (
          <li key={p.namespace + "/" + p.name}>
            <span className={styles.mono}>
              {p.namespace}/{p.name}
            </span>
            {p.reason && <span className={nodeStyles.muted}>{p.reason}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}
