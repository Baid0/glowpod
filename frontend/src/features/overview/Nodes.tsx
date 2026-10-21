import { Link } from "react-router";

import { Badge } from "../../components/Badge";
import { Card } from "../../components/Card";
import { HalfGauge, zoneFor } from "../../components/HalfGauge";
import { bytes, cores, percent } from "../../lib/format";
import type { Overview } from "../../lib/types";
import styles from "./OverviewPage.module.css";

/** One tile per node: arc = CPU, bar = memory. */
export function NodeLoad({ data }: { data: Overview | undefined }) {
  const thresholds = data?.thresholds ?? [0.7, 0.85];
  return (
    <Card title="Node load" actions="arc = CPU · bar = memory">
      {!data && <div className={styles.skeletonRows} />}
      <ul className={styles.nodes}>
        {data?.nodes.map((n) => {
          const cpu = n.cpu;
          const mem = n.memory;
          return (
            <li key={n.name} className={styles.nodeTile}>
              <Link to={`/nodes/${encodeURIComponent(n.name)}`} className={styles.nodeName}>
                {n.name}
              </Link>
              {!n.ready && <Badge tone="critical">Not ready</Badge>}
              <div className={styles.nodeCpu}>
                <HalfGauge
                  label={`${n.name} CPU`}
                  value={cpu ?? 0}
                  max={1}
                  size="S"
                  showValue={false}
                  thresholds={thresholds}
                  state={cpu === undefined ? "no-metrics" : "live"}
                />
                <span className={styles.nodeValue}>
                  <span className={styles.nodePercent}>
                    {cpu === undefined ? "–" : percent(cpu)}
                  </span>
                  <span className={styles.nodeUnit}>CPU</span>
                </span>
              </div>
              <div className={styles.memRow}>
                <span className={styles.memBar} aria-hidden="true">
                  {mem !== undefined && (
                    <span
                      className={styles.memFill}
                      data-zone={zoneFor(mem, thresholds)}
                      style={{ width: percent(Math.min(1, mem)) }}
                    />
                  )}
                </span>
                <span className={styles.memText}>Mem {mem === undefined ? "–" : percent(mem)}</span>
              </div>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

/** Pods using the most CPU right now. */
export function TopConsumers({ data }: { data: Overview | undefined }) {
  const missing = data?.unavailable?.find((u) => u.section === "topConsumers");
  const rows = data?.topConsumers ?? [];
  return (
    <Card title="Top consumers" actions="by CPU, right now">
      {!data && <div className={styles.skeletonRows} />}
      {missing && <p className={styles.sectionNote}>{missing.message}</p>}
      {data && !missing && rows.length === 0 && (
        <p className={styles.sectionNote}>No pods are running.</p>
      )}
      {rows.length > 0 && (
        <table className={styles.table}>
          <caption className="visually-hidden">Pods using the most CPU</caption>
          <thead>
            <tr>
              <th scope="col">Pod</th>
              <th scope="col">Namespace</th>
              <th scope="col" className={styles.num}>
                CPU
              </th>
              <th scope="col" className={styles.num}>
                Memory
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.namespace + "/" + r.name}>
                <td className={styles.mono}>
                  <Link
                    to={`/workloads/pods/${encodeURIComponent(r.namespace)}/${encodeURIComponent(r.name)}`}
                  >
                    {r.name}
                  </Link>
                </td>
                <td>{r.namespace}</td>
                <td className={styles.num}>{cores(r.cpu)}</td>
                <td className={styles.num}>{bytes(r.memory)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}
