import { IconCheck, IconInfoCircle } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useState } from "react";
import { useNavigate } from "react-router";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { useToast } from "../../components/Toast";
import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import type { ResourceRef, ResourceType } from "../../lib/types";
import { ChangeError } from "../actions/dialogs";
import { objectPagePath } from "../resources/cells";
import styles from "./create.module.css";
import { templateFor } from "./templates";

// CodeMirror loads only when someone creates or edits.
const YamlEditor = lazy(() =>
  import("../yaml/CodeEditor").then((m) => ({ default: m.YamlEditor })),
);

/**
 * Create one object of type from a template: edit it, check it with the
 * cluster (server-side dry run), then create it. Create stays off until the
 * text in the editor has passed a check.
 */
export function NewResourceDialog({
  open,
  onClose,
  resource,
  type,
  namespace,
}: {
  open: boolean;
  onClose: () => void;
  resource: ResourceRef;
  type: ResourceType;
  /** Where a namespaced object goes when its YAML doesn't say. */
  namespace: string;
}) {
  return open ? (
    <NewResourceForm onClose={onClose} resource={resource} type={type} namespace={namespace} />
  ) : null;
}

function NewResourceForm({
  onClose,
  resource,
  type,
  namespace,
}: {
  onClose: () => void;
  resource: ResourceRef;
  type: ResourceType;
  namespace: string;
}) {
  const cluster = useClusterId();
  const client = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();
  const [start] = useState(() => templateFor(type, namespace));
  const [text, setText] = useState(start);
  const [checked, setChecked] = useState<string | null>(null);

  const check = useMutation({
    mutationFn: (yaml: string) => api.createResource(cluster, resource, namespace, yaml, true),
    onSuccess: (_, yaml) => {
      setChecked(yaml);
    },
  });
  const create = useMutation({
    mutationFn: (yaml: string) => api.createResource(cluster, resource, namespace, yaml, false),
    onSuccess: async (result) => {
      const meta = result.object.object.metadata ?? {};
      const name = meta.name ?? "";
      toast({ tone: "ok", message: `Created ${type.kind} ${name}.` });
      onClose();
      await client.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== "session" && q.queryKey[0] !== "resourceTypes",
      });
      if (name) void navigate(objectPagePath(resource, meta.namespace ?? "", name));
    },
  });

  const ready = checked === text;
  const error = create.error ?? (ready ? null : check.error);
  const busy = check.isPending || create.isPending;

  return (
    <Dialog
      open
      size="wide"
      onClose={onClose}
      title={`New ${type.kind}`}
      description={
        <p>
          Start from this template. Glowpod checks it with the cluster before anything is created,
          and creates it as you.
        </p>
      }
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            disabled={busy || ready}
            onClick={() => {
              create.reset();
              check.mutate(text);
            }}
          >
            {check.isPending ? "Checking…" : "Check"}
          </Button>
          <Button
            variant="primary"
            icon={<IconCheck size={16} stroke={1.75} />}
            disabled={!ready || busy}
            onClick={() => {
              create.mutate(text);
            }}
          >
            {create.isPending ? "Creating…" : "Create"}
          </Button>
        </>
      }
    >
      {error ? (
        <ChangeError error={error} />
      ) : (
        <p className={styles.info} role="status">
          <IconInfoCircle size={16} stroke={1.75} aria-hidden="true" />
          {ready
            ? "The cluster accepted this (dry run). Nothing is created until you click Create."
            : "Edit the template, then Check. Create unlocks once the cluster accepts it."}
        </p>
      )}
      <div className={styles.editor}>
        <Suspense fallback={<div className={styles.loading} aria-label="Loading the editor" />}>
          <YamlEditor initial={start} label={`YAML of the new ${type.kind}`} onChange={setText} />
        </Suspense>
      </div>
    </Dialog>
  );
}
