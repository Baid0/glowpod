import { IconInfoCircle } from "@tabler/icons-react";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { HalfGauge } from "../../components/HalfGauge";
import { ApiError } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { OVERVIEW_REFRESH_MS, useOverview, useThresholds } from "../../lib/queries";
import { formatAge, useNow } from "../../lib/time";
import type { Overview } from "../../lib/types";
import { NeedsAttention } from "./Attention";
import { RecentDeploys } from "./Deploys";
import { RecentEvents } from "./Events";
import { gaugeView, type GaugeKind } from "./gauges";
import { NodeLoad, TopConsumers } from "./Nodes";
import styles from "./OverviewPage.module.css";
import { AppCards, WorkloadTiles } from "./Workloads";

const METRICS_SERVER_DOCS = "https://github.com/kubernetes-sigs/metrics-server#installation";

/** Is the cluster okay, and if not, where? Gauges first, problems second, detail last. */
export function OverviewPage() {
  const { mode } = usePreferences();
  const query = useOverview();
  const { data, isPending, isError, refetch, dataUpdatedAt } = query;
  const thresholds = useThresholds(data?.thresholds);
  const now = useNow();
  const stale = isError && data !== undefined;
  const updated = data ? `updated ${formatAge(now - dataUpdatedAt)} ago` : undefined;

  // People who may see some namespaces only, none of them known yet.
  if (!data && query.error instanceof ApiError && query.error.code === "namespace_needed") {
    return (
      <div className={styles.page}>
        <Card title="Pick your namespace">
          <p className={styles.pickNamespace}>
            You can see some namespaces of this cluster, not all, and Glowpod doesn&apos;t know
            which ones are yours. Type yours in the namespace menu at the top, and the Overview
            shows what&apos;s in it.
          </p>
        </Card>
      </div>
    );
  }

  // Network only renders when Prometheus was found.
  const kinds: GaugeKind[] = ["cpu", "memory", "pods", "storage"];
  if (data?.gauges.network) kinds.push("network");

  return (
    <div className={styles.page}>
      {isError && (
        <ClusterBanner
          message={query.error.message}
          lastContact={data ? now - dataUpdatedAt : undefined}
          nextTry={Math.max(0, query.errorUpdatedAt + OVERVIEW_REFRESH_MS - now)}
          onRetry={() => void refetch()}
        />
      )}

      <ClusterLine data={data} />

      {data?.gauges.cpu.state === "no-metrics" && (
        <p className={styles.notice}>
          <IconInfoCircle size={18} stroke={1.75} aria-hidden="true" />
          <span>
            metrics-server isn't installed, so CPU and memory usage can't be shown yet.{" "}
            <a href={METRICS_SERVER_DOCS} target="_blank" rel="noreferrer">
              How to install it
            </a>
          </span>
        </p>
      )}

      <section className={styles.gauges} aria-label="Cluster load">
        {kinds.map((kind) => (
          <Card key={kind} padding="gauge" className={styles.gaugeCard}>
            <HalfGauge
              {...gaugeView(kind, data?.gauges[kind], {
                loading: isPending,
                stale,
                ...(updated ? { updated } : {}),
                ...(thresholds ? { thresholds } : {}),
              })}
              onRetry={() => void refetch()}
            />
          </Card>
        ))}
      </section>

      {mode === "deep" ? <WorkloadTiles data={data} /> : <AppCards data={data} />}

      <div className={styles.split}>
        <NeedsAttention data={data} />
        <RecentEvents />
      </div>

      {mode === "deep" ? (
        <>
          <NodeLoad data={data} />
          <TopConsumers data={data} />
        </>
      ) : (
        <RecentDeploys data={data} />
      )}
    </div>
  );
}

function ClusterLine({ data }: { data: Overview | undefined }) {
  if (!data) return <div className={styles.clusterLine} aria-hidden="true" />;
  const c = data.cluster;
  const version = c.version.replace(/^(v\d+\.\d+).*/, "$1");
  const n = data.problemCount;
  const critical = data.problems.some((p) => p.severity === "critical");
  // People who can't see nodes or namespaces get no count, rather than 0.
  const facts = [
    version && `Kubernetes ${version}`,
    data.gauges.pods.state !== "no-access" &&
      `${String(c.nodes)} ${c.nodes === 1 ? "node" : "nodes"}`,
    !data.unavailable?.some((u) => u.section === "namespaces") &&
      `${String(c.namespaces)} namespaces`,
  ].filter(Boolean);

  return (
    <div className={styles.clusterLine}>
      <p className={styles.clusterFacts}>
        <span className={styles.mono}>{c.name}</span>
        {facts.map((f) => ` · ${String(f)}`)}
        {data.namespace && (
          <>
            {" "}
            · showing <span className={styles.mono}>{data.namespace.split(",").join(", ")}</span>
          </>
        )}
      </p>
      {n > 0 ? (
        <Badge tone={critical ? "critical" : "high"}>
          {n} {n === 1 ? "thing needs" : "things need"} attention
        </Badge>
      ) : (
        <Badge tone="ok">Nothing needs attention</Badge>
      )}
    </div>
  );
}

function ClusterBanner({
  message,
  lastContact,
  nextTry,
  onRetry,
}: {
  message: string;
  lastContact: number | undefined;
  nextTry: number;
  onRetry: () => void;
}) {
  return (
    <div className={styles.banner} role="alert">
      <span className={styles.bannerDot} aria-hidden="true" />
      <div className={styles.bannerText}>
        <strong>{message}</strong>
        <span>
          {lastContact !== undefined && <>Last contact {formatAge(lastContact)} ago. </>}
          Trying again in {Math.ceil(nextTry / 1000)} s.
        </span>
      </div>
      <Button size="sm" onClick={onRetry}>
        Retry now
      </Button>
    </div>
  );
}
