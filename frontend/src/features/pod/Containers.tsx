import { Link } from "react-router";

import { Badge, type StatusTone } from "../../components/Badge";
import { Card } from "../../components/Card";
import { HalfGauge } from "../../components/HalfGauge";
import { bytes, cores } from "../../lib/format";
import { usePreferences } from "../../lib/preferences";
import type { PodContainer, PodDetail } from "../../lib/types";
import styles from "./PodPage.module.css";

function stateBadge(c: PodContainer): [StatusTone, string] {
  if (c.state === "running")
    return c.ready || c.role === "init" ? ["ok", "Running"] : ["high", "Not ready"];
  if (c.state === "terminated")
    return c.exitCode === 0 ? ["neutral", "Finished"] : ["critical", c.reason ?? "Exited"];
  if (c.state === "waiting") {
    const r = c.reason ?? "Waiting";
    return [/ContainerCreating|PodInitializing/.test(r) ? "high" : "critical", r];
  }
  return ["neutral", "Unknown"];
}

/** Containers: Glance lists their state; Deep adds images, usage vs limits, probes and ports. */
export function Containers({ data }: { data: PodDetail }) {
  const { mode } = usePreferences();
  return (
    <Card title="Containers" actions={`${data.containers.length}`}>
      <ul className={styles.containers}>
        {data.containers.map((c) => {
          const [tone, word] = stateBadge(c);
          return (
            <li key={c.name} className={styles.container}>
              <div className={styles.containerHead}>
                <span className={styles.containerName}>{c.name}</span>
                {c.role !== "app" && <span className={styles.role}>{c.role}</span>}
                <Badge tone={tone}>{word}</Badge>
              </div>
              <p className={styles.containerMeta}>
                {c.restarts > 0 && `Restarted ${c.restarts} ${c.restarts === 1 ? "time" : "times"}`}
                {c.restarts > 0 &&
                  c.lastTermination &&
                  ` · last exit code ${c.lastTermination.exitCode}`}
                {c.restarts === 0 && "No restarts"}
              </p>
              {mode === "deep" && <ContainerDeep c={c} metrics={data.metricsAvailable} />}
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

function ContainerDeep({ c, metrics }: { c: PodContainer; metrics: boolean }) {
  return (
    <>
      <p className={styles.image} title={c.image}>
        {c.image}
      </p>
      {c.state !== "terminated" && (
        <div className={styles.usage}>
          <Usage
            label="CPU"
            used={c.usage?.cpu}
            limit={c.limits.cpu}
            request={c.requests.cpu}
            fmt={cores}
            name={c.name}
            metrics={metrics}
          />
          <Usage
            label="Memory"
            used={c.usage?.memory}
            limit={c.limits.memory}
            request={c.requests.memory}
            fmt={bytes}
            name={c.name}
            metrics={metrics}
          />
        </div>
      )}
      {(c.probes?.length ?? 0) > 0 && (
        <ul className={styles.probes}>
          {c.probes?.map((p) => (
            <li key={p.kind}>
              <span className={styles.probeKind}>{p.kind}</span> {p.action}, every {p.periodSeconds}
              s
            </li>
          ))}
        </ul>
      )}
      {(c.ports?.length ?? 0) > 0 && (
        <p className={styles.containerMeta}>
          Ports:{" "}
          {c.ports?.map((p) => `${p.port}/${p.protocol}${p.name ? ` (${p.name})` : ""}`).join(", ")}
        </p>
      )}
      {c.message && <p className={styles.containerMessage}>{c.message}</p>}
    </>
  );
}

/** Usage against the limit, or the request when there's no limit. */
function Usage({
  label,
  used,
  limit,
  request,
  fmt,
  name,
  metrics,
}: {
  label: string;
  used: number | undefined;
  limit: number | undefined;
  request: number | undefined;
  fmt: (n: number) => string;
  name: string;
  metrics: boolean;
}) {
  const max = limit ?? request;
  const against = limit !== undefined ? "limit" : request !== undefined ? "request" : undefined;
  return (
    <div className={styles.usageItem}>
      {max !== undefined && (
        <HalfGauge
          label={`${name} ${label}`}
          size="S"
          value={used ?? 0}
          max={max}
          state={used === undefined ? (metrics ? "error" : "no-metrics") : "live"}
        />
      )}
      <span className={styles.usageText}>
        <span className={styles.usageLabel}>{label}</span>
        {used === undefined ? "—" : fmt(used)}
        {max !== undefined && against && (
          <span className={styles.muted}>
            {" "}
            of {fmt(max)} {against}
          </span>
        )}
        {max === undefined && <span className={styles.muted}> · no request or limit</span>}
      </span>
    </div>
  );
}

/** Deep-only facts: conditions and volumes. */
export function Facts({ data }: { data: PodDetail }) {
  return (
    <>
      <Card title="Conditions">
        <ul className={styles.list}>
          {data.conditions.map((c) => (
            <li key={c.type} className={styles.listRow}>
              <span>{c.type}</span>
              <Badge tone={c.status === "True" ? "ok" : c.status === "False" ? "high" : "neutral"}>
                {c.status}
              </Badge>
              {c.message && <span className={styles.listNote}>{c.message}</span>}
            </li>
          ))}
        </ul>
        {data.qosClass && <p className={styles.muted}>QoS class: {data.qosClass}</p>}
      </Card>
      <Card title="Volumes" actions={`${data.volumes.length}`}>
        <ul className={styles.list}>
          {data.volumes.map((v) => (
            <li key={v.name} className={styles.listRow}>
              <span className={styles.mono}>{v.name}</span>
              <span className={styles.muted}>{v.kind}</span>
              {v.source && (
                <VolumeSource kind={v.kind} source={v.source} namespace={data.pod.namespace} />
              )}
            </li>
          ))}
        </ul>
      </Card>
    </>
  );
}

const VOLUME_LINKS: Record<string, string> = {
  persistentVolumeClaim: "persistentvolumeclaims",
  configMap: "configmaps",
  secret: "secrets",
};

function VolumeSource({
  kind,
  source,
  namespace,
}: {
  kind: string;
  source: string;
  namespace: string;
}) {
  const resource = VOLUME_LINKS[kind];
  if (!resource) return <span className={styles.listNote}>{source}</span>;
  return (
    <Link
      className={styles.listNote}
      to={`/resources/core/v1/${resource}/${encodeURIComponent(namespace)}/${encodeURIComponent(source)}`}
    >
      {source}
    </Link>
  );
}
