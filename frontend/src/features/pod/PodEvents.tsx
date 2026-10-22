import { Badge } from "../../components/Badge";
import { Card } from "../../components/Card";
import { formatAgeShort, useNow } from "../../lib/time";
import type { KubeEvent } from "../../lib/types";
import styles from "./PodPage.module.css";

/** The pod's events, newest first. Kubernetes keeps them for about an hour. */
export function PodEvents({ events }: { events: KubeEvent[] }) {
  const now = useNow(15_000);
  if (events.length === 0) {
    return (
      <Card>
        <p className={styles.muted}>No recent events. Kubernetes keeps events for about an hour.</p>
      </Card>
    );
  }
  return (
    <Card>
      <table className={styles.events}>
        <caption className="visually-hidden">Events for this pod</caption>
        <thead>
          <tr>
            <th scope="col">When</th>
            <th scope="col">Type</th>
            <th scope="col">Reason</th>
            <th scope="col">Message</th>
            <th scope="col" className={styles.num}>
              Count
            </th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => (
            <tr key={e.id}>
              <td className={styles.muted}>
                <time dateTime={e.time} title={new Date(e.time).toLocaleString()}>
                  {formatAgeShort(now - Date.parse(e.time))}
                </time>
              </td>
              <td>
                <Badge tone={e.type === "Warning" ? "high" : "neutral"}>{e.type}</Badge>
              </td>
              <td className={styles.mono}>{e.reason}</td>
              <td className={styles.message}>{e.message}</td>
              <td className={styles.num}>{e.count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}
