import { IconArrowLeft, IconCheck, IconInfoCircle } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useState } from "react";
import { useBlocker } from "react-router";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { useToast } from "../../components/Toast";
import { api, ApiError } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import type { ResourceRef, UpdateResult } from "../../lib/types";
import { ChangeError } from "../actions/dialogs";
import styles from "./EditYaml.module.css";

// CodeMirror loads only when someone edits.
const YamlEditor = lazy(() => import("./CodeEditor").then((m) => ({ default: m.YamlEditor })));
const YamlDiff = lazy(() => import("./CodeEditor").then((m) => ({ default: m.YamlDiff })));

type Stage = { at: "edit" } | { at: "review"; result: UpdateResult };

/**
 * Edit an object's YAML: change it, review what the cluster would save
 * (server-side dry run), then apply. Nothing is saved before Apply.
 */
export function EditYaml({
  resource,
  namespace,
  name,
  initial,
  onDone,
  onReload,
}: {
  resource: ResourceRef;
  namespace: string;
  name: string;
  /** The YAML editing starts from, read once. */
  initial: string;
  onDone: () => void;
  /** Fetches the latest version and restarts editing from it. */
  onReload: () => void;
}) {
  const cluster = useClusterId();
  const client = useQueryClient();
  const toast = useToast();
  // Live updates may refresh `initial`; editing keeps the version it started from.
  const [start] = useState(initial);
  const [text, setText] = useState(start);
  const [stage, setStage] = useState<Stage>({ at: "edit" });
  const dirty = text !== start;

  // Ask before leaving the page with unsaved edits.
  const [leaving, setLeaving] = useState(false);
  const blocker = useBlocker(dirty && !leaving);
  const finish = () => {
    setLeaving(true);
    onDone();
  };

  const review = useMutation({
    mutationFn: () => api.updateResource(cluster, resource, namespace, name, text, true),
    onSuccess: (result) => {
      setStage({ at: "review", result });
    },
  });
  const apply = useMutation({
    mutationFn: () => api.updateResource(cluster, resource, namespace, name, text, false),
    onSuccess: async () => {
      toast({ tone: "ok", message: `Saved ${name}.` });
      finish();
      await client.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== "session" && q.queryKey[0] !== "resourceTypes",
      });
    },
  });

  const error = review.error ?? apply.error;
  const conflict = error instanceof ApiError && error.code === "conflict";
  const unchanged = stage.at === "review" && stage.result.before === stage.result.after;

  return (
    <div className={styles.box}>
      <div className={styles.toolbar}>
        <span className={styles.meta}>
          {stage.at === "edit" ? (dirty ? "Editing · unsaved changes" : "Editing") : "Review"}
        </span>
        <div className={styles.buttons}>
          {stage.at === "edit" ? (
            <>
              <Button size="sm" onClick={finish}>
                Cancel
              </Button>
              <Button
                size="sm"
                variant="primary"
                disabled={!dirty || review.isPending}
                onClick={() => {
                  apply.reset();
                  review.mutate();
                }}
              >
                {review.isPending ? "Checking…" : "Review changes"}
              </Button>
            </>
          ) : (
            <>
              <Button
                size="sm"
                icon={<IconArrowLeft size={16} stroke={1.75} />}
                onClick={() => {
                  apply.reset();
                  setStage({ at: "edit" });
                }}
              >
                Back to editing
              </Button>
              <Button
                size="sm"
                variant="primary"
                icon={<IconCheck size={16} stroke={1.75} />}
                disabled={unchanged || apply.isPending}
                onClick={() => {
                  apply.mutate();
                }}
              >
                {apply.isPending ? "Applying…" : "Apply"}
              </Button>
            </>
          )}
        </div>
      </div>

      {error ? (
        <div className={styles.notice}>
          <ChangeError error={error} />
          {conflict && (
            <Button size="sm" onClick={onReload}>
              Start over from the latest version
            </Button>
          )}
        </div>
      ) : null}

      {stage.at === "review" && (
        <p className={styles.info}>
          <IconInfoCircle size={16} stroke={1.75} aria-hidden="true" />
          {unchanged
            ? "The cluster would save this exactly as it is now. There's nothing to apply."
            : "The cluster checked this change (server-side dry run). Nothing is saved until you apply."}
        </p>
      )}

      <Suspense fallback={<div className={styles.loading} aria-label="Loading the editor" />}>
        {stage.at === "edit" ? (
          <YamlEditor initial={text} label={`Edit YAML of ${name}`} onChange={setText} />
        ) : (
          <YamlDiff
            before={stage.result.before ?? ""}
            after={stage.result.after ?? ""}
            label={`Changes to ${name}`}
          />
        )}
      </Suspense>

      <Dialog
        open={blocker.state === "blocked"}
        onClose={() => {
          blocker.reset?.();
        }}
        title="Leave without saving?"
        description={<p>Your changes to {name} aren't saved yet.</p>}
        footer={
          <>
            <Button
              onClick={() => {
                blocker.reset?.();
              }}
            >
              Keep editing
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                blocker.proceed?.();
              }}
            >
              Discard changes
            </Button>
          </>
        }
      />
    </div>
  );
}
