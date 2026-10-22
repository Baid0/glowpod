import { IconCheck, IconInfoCircle } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useId, useState } from "react";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { useToast } from "../../components/Toast";
import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import type { ApplyResult } from "../../lib/types";
import { ChangeError } from "../actions/dialogs";
import { objectPagePath } from "../resources/cells";
import styles from "./create.module.css";
import { OUTCOME_TONE, outcomeWord, summarize } from "./outcomes";
import { Link } from "../../lib/clusterLinks";

const YamlEditor = lazy(() =>
  import("../yaml/CodeEditor").then((m) => ({ default: m.YamlEditor })),
);

const START = "# Paste one or more objects. Separate them with a line of ---\n";

/**
 * Apply any number of objects of any kind, like kubectl apply: check them
 * all with the cluster first (server-side dry run), then apply. Apply stays
 * off until the text in the editor has passed a check with no failures.
 */
export function ApplyYamlDialog({
  open,
  onClose,
  namespace,
}: {
  open: boolean;
  onClose: () => void;
  /** Where namespaced objects go when their YAML doesn't say. */
  namespace: string;
}) {
  return open ? <ApplyYamlForm onClose={onClose} namespace={namespace} /> : null;
}

function ApplyYamlForm({ onClose, namespace }: { onClose: () => void; namespace: string }) {
  const cluster = useClusterId();
  const client = useQueryClient();
  const toast = useToast();
  const [text, setText] = useState(START);
  // Overwrite fields set another way, like kubectl apply --force-conflicts.
  const [force, setForce] = useState(false);
  const [checked, setChecked] = useState<{
    text: string;
    force: boolean;
    result: ApplyResult;
  } | null>(null);
  const [applied, setApplied] = useState<ApplyResult | null>(null);
  const forceId = useId();

  const check = useMutation({
    mutationFn: (v: { yaml: string; force: boolean }) =>
      api.apply(cluster, namespace, v.yaml, true, v.force),
    onSuccess: (result, v) => {
      setChecked({ text: v.yaml, force: v.force, result });
    },
  });
  const apply = useMutation({
    mutationFn: (yaml: string) => api.apply(cluster, namespace, yaml, false, force),
    onSuccess: async (result) => {
      setApplied(result);
      const failed = result.items.some((it) => it.result === "failed");
      toast({ tone: failed ? "critical" : "ok", message: `Applied: ${summarize(result.items)}.` });
      await client.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== "session" && q.queryKey[0] !== "resourceTypes",
      });
    },
  });

  const current = checked?.text === text && checked.force === force ? checked.result : null;
  const failures = current?.items.filter((it) => it.result === "failed").length ?? 0;
  // Offer to overwrite once a check finds conflicts, and keep the choice visible.
  const conflicts = force || (checked?.result.items.some((it) => it.code === "conflict") ?? false);
  const ready = current !== null && failures === 0;
  const busy = check.isPending || apply.isPending;
  const error = apply.error ?? (current ? null : check.error);

  if (applied) {
    return (
      <Dialog
        open
        size="wide"
        onClose={onClose}
        title="Applied"
        description={<p>{summarize(applied.items)}.</p>}
        footer={
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        }
      >
        <Results result={applied} onOpen={onClose} />
      </Dialog>
    );
  }

  return (
    <Dialog
      open
      size="wide"
      onClose={onClose}
      title="Apply YAML"
      description={
        <p>
          Paste manifests of any kind. Glowpod creates what&apos;s new and updates what exists
          (server-side apply), as you. Objects without a namespace go into{" "}
          {namespace || "the namespace their YAML names"}.
        </p>
      }
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            disabled={busy || current !== null}
            onClick={() => {
              apply.reset();
              check.mutate({ yaml: text, force });
            }}
          >
            {check.isPending ? "Checking…" : "Check"}
          </Button>
          <Button
            variant="primary"
            icon={<IconCheck size={16} stroke={1.75} />}
            disabled={!ready || busy}
            onClick={() => {
              apply.mutate(text);
            }}
          >
            {apply.isPending ? "Applying…" : "Apply"}
          </Button>
        </>
      }
    >
      {error ? (
        <ChangeError error={error} />
      ) : (
        <p className={styles.info} role="status">
          <IconInfoCircle size={16} stroke={1.75} aria-hidden="true" />
          {current
            ? failures > 0
              ? `${String(failures)} of ${String(current.items.length)} can't be applied. Fix them, then Check again.`
              : "The cluster accepted everything (dry run). Nothing changes until you click Apply."
            : "Paste your YAML, then Check. Apply unlocks once every object passes."}
        </p>
      )}
      {current && <Results result={current} />}
      {conflicts && (
        <div className={styles.force}>
          <input
            id={forceId}
            type="checkbox"
            checked={force}
            onChange={(e) => {
              setForce(e.target.checked);
            }}
          />
          <label htmlFor={forceId}>
            Overwrite fields that were set another way, and make Glowpod their owner (like{" "}
            <code>kubectl apply --force-conflicts</code>). Check again after changing this.
          </label>
        </div>
      )}
      <div className={styles.editor}>
        <Suspense fallback={<div className={styles.loading} aria-label="Loading the editor" />}>
          <YamlEditor initial={START} label="YAML to apply" onChange={setText} />
        </Suspense>
      </div>
    </Dialog>
  );
}

/** One row per object: what happens to it, and why not when it fails. */
function Results({ result, onOpen }: { result: ApplyResult; onOpen?: () => void }) {
  return (
    <ul className={styles.results} aria-label={result.dryRun ? "What would happen" : "Results"}>
      {result.items.map((it, i) => {
        const label = `${it.kind ?? "Object"} ${it.name ?? "(no name)"}`;
        const linkable =
          !result.dryRun && it.result !== "failed" && it.group && it.version && it.resource;
        return (
          <li key={i} className={styles.result}>
            <Badge tone={OUTCOME_TONE[it.result]}>{outcomeWord(it.result, result.dryRun)}</Badge>
            <span className={styles.object}>
              {linkable ? (
                <Link
                  to={objectPagePath(
                    {
                      group: it.group ?? "",
                      version: it.version ?? "",
                      resource: it.resource ?? "",
                    },
                    it.namespace ?? "",
                    it.name ?? "",
                  )}
                  onClick={onOpen}
                >
                  {label}
                </Link>
              ) : (
                label
              )}
              {it.namespace && <span className={styles.muted}> in {it.namespace}</span>}
            </span>
            {it.result === "failed" && (
              <span className={styles.why}>
                {it.message}
                {it.detail && <span className={styles.detail}>{it.detail}</span>}
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}
