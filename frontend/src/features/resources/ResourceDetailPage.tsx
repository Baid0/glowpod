import { useState } from "react";
import { useSearchParams } from "react-router";

import { Tabs } from "../../components/Tabs";
import { usePreferences } from "../../lib/preferences";
import { formatAge, useNow } from "../../lib/time";
import type { ResourceRef } from "../../lib/types";
import { ResourceActions } from "../actions/ResourceActions";
import { ObjectError } from "../yaml/ObjectError";
import { ObjectYaml } from "../yaml/ObjectYaml";
import { isSecret, useObject } from "../yaml/useObject";
import styles from "./ResourceDetailPage.module.css";
import { Summary } from "./summary/Summary";

const TABS = [
  { id: "overview", label: "Overview" },
  { id: "yaml", label: "YAML" },
] as const;
type TabId = (typeof TABS)[number]["id"];

/**
 * Any object, CRDs included: a plain summary (richer for kinds Glowpod
 * knows), its conditions and events, and its YAML.
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
  const [editing, setEditing] = useState(false);
  const secret = isSecret(resource);
  const { data, error, refetch } = useObject(resource, namespace, name, revealed);
  const now = useNow(30_000);
  const meta = data?.object.metadata;
  const labels = Object.entries(meta?.labels ?? {});
  const [params, setParams] = useSearchParams();
  const tab: TabId = params.get("tab") === "yaml" ? "yaml" : "overview";
  const select = (id: TabId) => {
    setParams(id === "overview" ? {} : { tab: id }, { replace: true });
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.titleRow}>
          <div className={styles.title}>
            <p className={styles.kind}>{data?.object.kind ?? resource.resource}</p>
            <h2 className={styles.name}>{name}</h2>
          </div>
          {data?.object.kind && (
            <ResourceActions
              resource={resource}
              namespace={namespace}
              name={name}
              kind={data.object.kind}
              {...replicasOf(data.object)}
              {...(editing
                ? {}
                : {
                    onEdit: () => {
                      // Editing a Secret shows its values: the click is the reveal.
                      if (secret) setRevealed(true);
                      select("yaml");
                      setEditing(true);
                    },
                    editLabel: secret && !revealed ? "Reveal and edit" : "Edit YAML",
                  })}
            />
          )}
        </div>
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
      <Tabs tabs={TABS} selected={tab} onSelect={select} label="Views" />
      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "yaml" ? (
          <ObjectYaml
            resource={resource}
            namespace={namespace}
            name={name}
            revealed={revealed}
            onReveal={(r) => {
              setRevealed(r);
              if (!r) setEditing(false);
            }}
            editing={editing}
            onEditingChange={setEditing}
          />
        ) : data ? (
          <Summary resource={resource} object={data.object} namespace={namespace} name={name} />
        ) : error ? (
          <ObjectError error={error} onRetry={() => void refetch()} />
        ) : (
          <div className={styles.skeleton} aria-label="Loading" />
        )}
      </div>
    </div>
  );
}

/** spec.replicas, for objects that have it. */
function replicasOf(obj: Record<string, unknown>): { replicas?: number } {
  const spec = obj.spec;
  if (spec && typeof spec === "object" && "replicas" in spec && typeof spec.replicas === "number") {
    return { replicas: spec.replicas };
  }
  return {};
}
