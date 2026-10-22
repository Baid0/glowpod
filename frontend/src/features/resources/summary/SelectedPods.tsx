import { Badge } from "../../../components/Badge";
import { Card } from "../../../components/Card";
import { podPath } from "../../pod/paths";
import { statusTone } from "../cells";
import { useResourceList } from "../useResourceList";
import { str } from "./read";
import styles from "./Summary.module.css";
import { Link } from "../../../lib/clusterLinks";

const PODS = { group: "", version: "v1", resource: "pods" };
const SHOWN = 20;

/** Live list of the pods a selector picks: a Service's backends, a workload's pods. */
export function SelectedPods({
  title,
  namespace,
  selector,
  empty,
}: {
  title: string;
  namespace: string;
  selector: string;
  empty: string;
}) {
  const { query } = useResourceList(PODS, namespace, selector);
  const { data, isPending, error } = query;
  const rows = data?.pages.flatMap((p) => p.rows) ?? [];
  const columns = data?.pages[0]?.columns ?? [];
  const col = (name: string) => columns.findIndex((c) => c.name === name);
  const [status, ready, restarts] = [col("Status"), col("Ready"), col("Restarts")];

  return (
    <Card title={title} actions={<span className={styles.muted}>{selector}</span>}>
      {isPending ? (
        <div className={styles.skeleton} aria-label="Loading pods" />
      ) : error ? (
        <p className={styles.muted}>{error.message}</p>
      ) : rows.length === 0 ? (
        <p className={styles.muted}>{empty}</p>
      ) : (
        <ul className={styles.pods}>
          {rows.slice(0, SHOWN).map((r) => {
            const word = str(r.cells[status]) ?? "";
            return (
              <li key={r.object.uid} className={styles.pod}>
                <Link
                  className={styles.mono}
                  to={podPath({ namespace: r.object.namespace ?? namespace, name: r.object.name })}
                >
                  {r.object.name}
                </Link>
                {word && <Badge tone={statusTone(word)}>{word}</Badge>}
                <span className={styles.muted}>
                  {ready >= 0 ? `${str(r.cells[ready]) ?? ""} ready` : ""}
                  {restarts >= 0 && str(r.cells[restarts]) !== "0"
                    ? ` · ${str(r.cells[restarts]) ?? ""} restarts`
                    : ""}
                </span>
              </li>
            );
          })}
          {rows.length > SHOWN && <li className={styles.muted}>and {rows.length - SHOWN} more</li>}
        </ul>
      )}
    </Card>
  );
}
