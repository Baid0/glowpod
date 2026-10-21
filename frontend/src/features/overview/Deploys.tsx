import { IconRocket } from "@tabler/icons-react";

import { Card } from "../../components/Card";
import { imageTag } from "../../lib/format";
import { usePreferences } from "../../lib/preferences";
import { formatAgeShort, useNow } from "../../lib/time";
import type { Overview } from "../../lib/types";
import styles from "./OverviewPage.module.css";

/** Glance mode's bottom row: the newest Deployment revisions. */
export function RecentDeploys({ data }: { data: Overview | undefined }) {
  const { setMode } = usePreferences();
  const now = useNow(30_000);
  const deploys = data?.recentDeploys ?? [];

  return (
    <Card
      title="Recent deploys"
      actions={
        <button
          type="button"
          className={styles.textButton}
          onClick={() => {
            setMode("deep");
          }}
        >
          Show in Deep
        </button>
      }
    >
      {!data && <div className={styles.skeletonRows} />}
      {data && deploys.length === 0 && <p className={styles.sectionNote}>No deploys yet.</p>}
      {deploys.length > 0 && (
        <ul className={styles.rows}>
          {deploys.map((d) => (
            <li key={`${d.namespace}/${d.name}/${d.revision}`} className={styles.event}>
              <IconRocket
                size={16}
                stroke={1.75}
                className={styles.eventIcon}
                data-tone="info"
                aria-hidden="true"
              />
              <span className={styles.eventText}>
                <span className={styles.eventMessage}>
                  <span className={styles.mono}>{d.name}</span> rolled out revision {d.revision}
                </span>
                <span className={styles.eventObject}>
                  {d.namespace} · {imageTag(d.image)}
                </span>
              </span>
              <time className={styles.age} dateTime={d.at}>
                {formatAgeShort(now - Date.parse(d.at))}
              </time>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
