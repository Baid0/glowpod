import { IconEye, IconEyeOff, IconLock } from "@tabler/icons-react";

import { Button } from "../../components/Button";
import { ApiError } from "../../lib/api";
import type { ResourceRef } from "../../lib/types";
import styles from "./ObjectYaml.module.css";
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
}: {
  resource: ResourceRef;
  namespace: string;
  name: string;
  revealed?: boolean;
  onReveal?: (reveal: boolean) => void;
}) {
  const { data, error, isPending, refetch } = useObject(resource, namespace, name, revealed);
  const secret = isSecret(resource);

  if (error) {
    const msg =
      error instanceof ApiError && error.status === 403
        ? "You can't read this. Ask an admin for access."
        : error instanceof ApiError && error.status === 404
          ? "This object is gone."
          : error.message;
    return (
      <div className={styles.error} role="alert">
        <p>{msg}</p>
        {!(error instanceof ApiError && [403, 404].includes(error.status)) && (
          <Button size="sm" onClick={() => void refetch()}>
            Retry
          </Button>
        )}
      </div>
    );
  }
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
      {secret && revealed && values.length > 0 && (
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
      <YamlView yaml={data.yaml} label={`YAML of ${name}`} />
    </div>
  );
}
