import { Link } from "react-router";

import { Badge } from "../../components/Badge";
import { Card } from "../../components/Card";
import { formatAge, useNow } from "../../lib/time";
import type { AppSummary } from "../../lib/types";
import { APP_BADGES, appPath } from "./appLinks";
import styles from "./Apps.module.css";

/** One app: status, ready replicas, last deploy. */
export function AppCard({ app }: { app: AppSummary }) {
  const now = useNow(60_000);
  const [tone, word] = APP_BADGES[app.status];
  return (
    <Card
      title={
        <Link className={styles.cardLink} title={app.name} to={appPath(app)}>
          {app.name}
        </Link>
      }
      headingLevel={3}
      actions={<Badge tone={tone}>{word}</Badge>}
    >
      <p className={styles.meta}>
        {app.namespace} · {app.workloads} {app.workloads === 1 ? "workload" : "workloads"}
      </p>
      <p className={styles.ready}>
        <span className={styles.big}>{app.ready}</span>{" "}
        <span className={styles.rest}>/ {app.total} ready</span>
      </p>
      {app.lastDeploy && (
        <p className={styles.meta}>Deployed {formatAge(now - Date.parse(app.lastDeploy))} ago</p>
      )}
    </Card>
  );
}
