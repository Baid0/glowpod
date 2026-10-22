import { Link } from "react-router";

import { Badge } from "../../components/Badge";
import { formatAge } from "../../lib/time";
import type { PodSummary } from "../../lib/types";
import { statusTone } from "../resources/cells";
import { podPath } from "./paths";
import styles from "./PodTable.module.css";

/** Pods with their status pill, readiness and restarts. */
export function PodTable({
  pods,
  now,
  caption,
  show = "node",
}: {
  pods: PodSummary[];
  now: number;
  caption: string;
  /** The column that tells pods apart here: their node, or their namespace. */
  show?: "node" | "namespace";
}) {
  if (pods.length === 0) return <p className={styles.muted}>No pods running.</p>;
  return (
    <table className={styles.table}>
      <caption className="visually-hidden">{caption}</caption>
      <thead>
        <tr>
          <th scope="col">Pod</th>
          {show === "namespace" && <th scope="col">Namespace</th>}
          <th scope="col">Status</th>
          <th scope="col">Ready</th>
          <th scope="col" className={styles.num}>
            Restarts
          </th>
          {show === "node" && <th scope="col">Node</th>}
          <th scope="col">Age</th>
        </tr>
      </thead>
      <tbody>
        {pods.map((p) => {
          const word = p.problem?.reason ?? p.phase;
          return (
            <tr key={p.namespace + "/" + p.name}>
              <td>
                <Link className={styles.mono} to={podPath(p)}>
                  {p.name}
                </Link>
              </td>
              {show === "namespace" && <td className={styles.muted}>{p.namespace}</td>}
              <td>
                <Badge tone={p.problem ? p.problem.severity : statusTone(p.phase)}>{word}</Badge>
              </td>
              <td>{p.ready}</td>
              <td className={styles.num}>{p.restarts}</td>
              {show === "node" && <td className={styles.muted}>{p.node ?? "—"}</td>}
              <td className={styles.muted}>{formatAge(now - Date.parse(p.createdAt))}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
