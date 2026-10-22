import { IconArrowsVertical, IconPencil, IconRefresh, IconTrash } from "@tabler/icons-react";
import { useState } from "react";
import { useNavigate } from "react-router";

import { listPathFor } from "../../app/navigation";
import { Button } from "../../components/Button";
import type { ResourceRef } from "../../lib/types";
import styles from "./actions.module.css";
import { deleteConsequence } from "./consequence";
import { DeleteDialog, RestartDialog, ScaleDialog } from "./dialogs";
import { useAllowedActions } from "./useActions";

type Open = "scale" | "restart" | "delete" | null;

/**
 * The change buttons for one object. Each shows only when the API supports
 * it and the cluster says this user may do it.
 */
export function ResourceActions({
  resource,
  namespace,
  name,
  kind,
  replicas,
  ownerKind,
  onEdit,
  editLabel = "Edit YAML",
}: {
  resource: ResourceRef;
  namespace: string;
  name: string;
  /** The object's Kind, e.g. "Deployment". */
  kind: string;
  /** spec.replicas, for Scale. */
  replicas?: number;
  /** The controller that would replace a deleted pod. */
  ownerKind?: string;
  /** Opens the YAML editor; omitted where editing lives elsewhere. */
  onEdit?: () => void;
  /** "Reveal and edit" for a hidden Secret. */
  editLabel?: string;
}) {
  const allowed = useAllowedActions(resource, namespace, name);
  const navigate = useNavigate();
  const [open, setOpen] = useState<Open>(null);
  const close = () => {
    setOpen(null);
  };
  const target = { resource, namespace, name, kind: kind.toLowerCase() };

  if (!allowed.edit && !allowed.scale && !allowed.restart && !allowed.delete) return null;
  return (
    <div className={styles.bar} role="group" aria-label="Actions">
      {allowed.edit && onEdit && (
        <Button size="sm" icon={<IconPencil size={16} stroke={1.75} />} onClick={onEdit}>
          {editLabel}
        </Button>
      )}
      {allowed.scale && (
        <Button
          size="sm"
          icon={<IconArrowsVertical size={16} stroke={1.75} />}
          onClick={() => {
            setOpen("scale");
          }}
        >
          Scale
        </Button>
      )}
      {allowed.restart && (
        <Button
          size="sm"
          icon={<IconRefresh size={16} stroke={1.75} />}
          onClick={() => {
            setOpen("restart");
          }}
        >
          Restart
        </Button>
      )}
      {allowed.delete && (
        <Button
          size="sm"
          icon={<IconTrash size={16} stroke={1.75} />}
          onClick={() => {
            setOpen("delete");
          }}
        >
          Delete
        </Button>
      )}

      {allowed.scale && (
        <ScaleDialog {...target} open={open === "scale"} onClose={close} current={replicas ?? 0} />
      )}
      {allowed.restart && <RestartDialog {...target} open={open === "restart"} onClose={close} />}
      {allowed.delete && (
        <DeleteDialog
          {...target}
          open={open === "delete"}
          onClose={close}
          consequence={deleteConsequence(resource.resource, ownerKind)}
          onDeleted={() => void navigate(listPathFor(resource))}
        />
      )}
    </div>
  );
}
