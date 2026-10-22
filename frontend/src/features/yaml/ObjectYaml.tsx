import { IconEye, IconEyeOff, IconLock } from "@tabler/icons-react";
import { useState } from "react";

import { Button } from "../../components/Button";
import type { ResourceRef } from "../../lib/types";
import styles from "./ObjectYaml.module.css";
import { EditYaml } from "./EditYaml";
import { ObjectError } from "./ObjectError";
import { decodeSecretValue } from "./secret";
import { isSecret, useObject } from "./useObject";
import { YamlView } from "./YamlView";

/**
 * An object's YAML. Secret values stay hidden until someone clicks Reveal;
 * revealed values also show decoded, and Hide puts them away again.
 */
export function ObjectYaml({
  resource,
  namespace,
  name,
  revealed = false,
  onReveal,
  editing = false,
  onEditingChange,
}: {
  resource: ResourceRef;
  namespace: string;
  name: string;
  revealed?: boolean;
  onReveal?: (reveal: boolean) => void;
  /** Shows the editor instead of the read-only view. */
  editing?: boolean;
  onEditingChange?: (editing: boolean) => void;
}) {
  const { data, error, isPending, refetch } = useObject(resource, namespace, name, revealed);
  const secret = isSecret(resource);
  // Bumped to restart editing from a freshly loaded version.
  const [generation, setGeneration] = useState(0);
  // A hidden Secret can't be edited: saving would blank its values.
  const canEdit = editing && (!secret || revealed);

  if (error) return <ObjectError error={error} onRetry={() => void refetch()} />;
  if (isPending) return <div className={styles.skeleton} aria-label="Loading YAML" />;

  const values = Object.entries(data.object.data ?? {});
  return (
    <div className={styles.stack}>
      {secret && onReveal && (
        <div className={styles.secret} data-revealed={revealed}>
          {revealed ? (
            <IconEye size={18} stroke={1.75} aria-hidden="true" />
          ) : (
            <IconLock size={18} stroke={1.75} aria-hidden="true" />
          )}
          <p className={styles.secretText}>
            {revealed
              ? "Secret values are showing. Hide them when you're done."
              : `Secret values are hidden. ${values.length} ${values.length === 1 ? "key" : "keys"}.`}
          </p>
          <Button
            size="sm"
            variant={revealed ? "secondary" : "primary"}
            icon={
              revealed ? (
                <IconEyeOff size={16} stroke={1.75} />
              ) : (
                <IconEye size={16} stroke={1.75} />
              )
            }
            onClick={() => {
              onReveal(!revealed);
            }}
          >
            {revealed ? "Hide values" : "Reveal values"}
          </Button>
        </div>
      )}
      {secret && revealed && !canEdit && values.length > 0 && (
        <section className={styles.decoded} aria-label="Decoded values">
          <h3 className={styles.decodedTitle}>Decoded values</h3>
          <dl className={styles.pairs}>
            {values.map(([key, b64]) => {
              const v = decodeSecretValue(b64);
              return (
                <div key={key} className={styles.pair}>
                  <dt>{key}</dt>
                  <dd>{v.text ?? `(binary, ${String(v.bytes)} bytes)`}</dd>
                </div>
              );
            })}
          </dl>
        </section>
      )}
      {canEdit ? (
        <EditYaml
          key={generation}
          resource={resource}
          namespace={namespace}
          name={name}
          initial={data.yaml}
          onDone={() => onEditingChange?.(false)}
          onReload={() => {
            void refetch().then(() => {
              setGeneration((g) => g + 1);
            });
          }}
        />
      ) : (
        <YamlView yaml={data.yaml} label={`YAML of ${name}`} />
      )}
    </div>
  );
}
