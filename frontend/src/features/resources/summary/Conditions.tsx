import { Badge } from "../../../components/Badge";
import { Card } from "../../../components/Card";
import { formatAge, useNow } from "../../../lib/time";
import { conditionTone } from "./logic";
import { arr, at, str } from "./read";
import styles from "./Summary.module.css";

/** status.conditions, which most resources and many CRDs report. */
export function Conditions({ object }: { object: unknown }) {
  const now = useNow(30_000);
  const conditions = arr(at(object, "status", "conditions"))
    .map((c) => ({
      type: str(at(c, "type")) ?? "",
      status: str(at(c, "status")) ?? "Unknown",
      reason: str(at(c, "reason")),
      message: str(at(c, "message")),
      since: str(at(c, "lastTransitionTime")),
    }))
    .filter((c) => c.type);
  if (conditions.length === 0) return null;
  return (
    <Card title="Conditions">
      <ul className={styles.conditions}>
        {conditions.map((c) => (
          <li key={c.type} className={styles.condition}>
            <Badge tone={conditionTone(c.type, c.status)}>
              {c.type}: {c.status}
            </Badge>
            <span className={styles.muted}>
              {[c.reason, c.message].filter(Boolean).join(" · ")}
              {c.since ? ` · since ${formatAge(now - Date.parse(c.since))} ago` : ""}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}
