import { useState } from "react";

import { usePreferences } from "../../lib/preferences";
import { formatAge, useNow } from "../../lib/time";
import type { ResourceRef } from "../../lib/types";
import { ObjectYaml } from "../yaml/ObjectYaml";
import { useObject } from "../yaml/useObject";
import styles from "./ResourceDetailPage.module.css";

/**
 * Any object, CRDs included: what it is and its YAML. Richer pages for
 * specific kinds come in later phases.
 */
export function ResourceDetailPage({
  resource,
  namespace,
  name,
}: {
  resource: ResourceRef;
  namespace: string;
  name: string;
}) {
  const { mode } = usePreferences();
  const [revealed, setRevealed] = useState(false);
  const { data } = useObject(resource, namespace, name, revealed);
  const now = useNow(30_000);
  const meta = data?.object.metadata;
  const labels = Object.entries(meta?.labels ?? {});

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <p className={styles.kind}>{data?.object.kind ?? resource.resource}</p>
        <h2 className={styles.name}>{name}</h2>
        <dl className={styles.facts}>
          {namespace && (
            <div className={styles.fact}>
              <dt>Namespace</dt>
              <dd>{namespace}</dd>
            </div>
          )}
          {meta?.creationTimestamp && (
            <div className={styles.fact}>
              <dt>Age</dt>
              <dd>{formatAge(now - Date.parse(meta.creationTimestamp))}</dd>
            </div>
          )}
          <div className={styles.fact}>
            <dt>API</dt>
            <dd>{[resource.group || "core", resource.version].join("/")}</dd>
          </div>
        </dl>
        {mode === "deep" && labels.length > 0 && (
          <ul className={styles.labels} aria-label="Labels">
            {labels.map(([k, v]) => (
              <li key={k} className={styles.label}>
                {k}={v}
              </li>
            ))}
          </ul>
        )}
      </header>
      <ObjectYaml
        resource={resource}
        namespace={namespace}
        name={name}
        revealed={revealed}
        onReveal={setRevealed}
      />
    </div>
  );
}
