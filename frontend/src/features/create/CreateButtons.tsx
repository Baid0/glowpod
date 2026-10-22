import { IconFileImport, IconPlus } from "@tabler/icons-react";
import { useState } from "react";

import { Button } from "../../components/Button";
import { useAccess } from "../../lib/access";
import type { ResourceRef, ResourceType } from "../../lib/types";
import { ApplyYamlDialog } from "./ApplyYamlDialog";
import styles from "./create.module.css";
import { targetNamespace } from "./namespace";
import { NewResourceDialog } from "./NewResourceDialog";

/**
 * "New <Kind>" on a resource list, shown only when the API can create this
 * kind and RBAC lets the person create it there; "Apply YAML" in Deep mode,
 * where the cluster checks each object on its own.
 */
export function CreateButtons({
  resource,
  type,
  namespaces,
  deep,
}: {
  resource: ResourceRef;
  type: ResourceType | undefined;
  namespaces: readonly string[];
  deep: boolean;
}) {
  const [open, setOpen] = useState<"new" | "apply" | null>(null);
  const namespace = targetNamespace(namespaces);
  const creatable = type?.verbs.includes("create") ?? false;
  const [allowed] = useAccess(
    type && creatable
      ? [
          {
            verb: "create",
            group: type.group,
            resource: type.resource,
            ...(type.namespaced ? { namespace } : {}),
          },
        ]
      : [],
  );
  const close = () => {
    setOpen(null);
  };

  return (
    <div className={styles.actions}>
      {deep && (
        <Button
          size="sm"
          icon={<IconFileImport size={16} stroke={1.75} />}
          onClick={() => {
            setOpen("apply");
          }}
        >
          Apply YAML
        </Button>
      )}
      {type && allowed && (
        <Button
          size="sm"
          variant="primary"
          icon={<IconPlus size={16} stroke={1.75} />}
          onClick={() => {
            setOpen("new");
          }}
        >
          New {type.kind}
        </Button>
      )}
      {type && (
        <NewResourceDialog
          open={open === "new"}
          onClose={close}
          resource={resource}
          type={type}
          namespace={type.namespaced ? namespace : ""}
        />
      )}
      <ApplyYamlDialog open={open === "apply"} onClose={close} namespace={namespace} />
    </div>
  );
}
