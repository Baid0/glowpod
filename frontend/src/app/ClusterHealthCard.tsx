import { HalfGauge, type GaugeState } from "../components/HalfGauge";
import { useOverview, useThresholds } from "../lib/queries";
import type { Gauge } from "../lib/types";
import styles from "./ClusterHealthCard.module.css";

/** Panel footer: CPU, memory and pod mini gauges plus running · pending · failing. */
export function ClusterHealthCard() {
  const { data, isPending, isError, refetch } = useOverview();

  const state = (g: Gauge | undefined): GaugeState => {
    if (isPending) return "loading";
    if (!g || isError) return "error";
    return g.state === "no-access" ? "no-metrics" : g.state;
  };
  const gauges: { label: string; gauge: Gauge | undefined }[] = [
    { label: "CPU", gauge: data?.gauges.cpu },
    { label: "Memory", gauge: data?.gauges.memory },
    { label: "Pods", gauge: data?.gauges.pods },
  ];
  const thresholds = useThresholds(data?.thresholds);

  return (
    <section className={styles.card} aria-labelledby="cluster-health-title">
      <h2 id="cluster-health-title" className={styles.title}>
        Cluster health
      </h2>
      <div className={styles.gauges}>
        {gauges.map(({ label, gauge }) => (
          <figure key={label} className={styles.mini}>
            <HalfGauge
              label={label}
              value={gauge?.value ?? 0}
              max={gauge?.max ?? 0}
              size="S"
              valueInside
              state={state(gauge)}
              {...(thresholds ? { thresholds } : {})}
              onRetry={() => void refetch()}
            />
            <figcaption className={styles.caption}>{label}</figcaption>
          </figure>
        ))}
      </div>
      {data && (
        <p className={styles.counts}>
          <Count tone="ok" n={data.health.running} word="running" />
          <Count tone="high" n={data.health.pending} word="pending" />
          <Count tone="critical" n={data.health.failing} word="failing" />
        </p>
      )}
    </section>
  );
}

function Count({ tone, n, word }: { tone: "ok" | "high" | "critical"; n: number; word: string }) {
  return (
    <span className={styles.count}>
      <span className={styles.dot} data-tone={tone} aria-hidden="true" />
      <span className={styles.n}>{n}</span> {word}
    </span>
  );
}
