import {
  IconAlertTriangle,
  IconCircleCheck,
  IconCircleX,
  IconInfoCircle,
} from "@tabler/icons-react";
import { Link } from "react-router";

import { Card } from "../../components/Card";
import { useLive } from "../../lib/live";
import { useEvents } from "../../lib/queries";
import { formatAgeShort, useNow } from "../../lib/time";
import type { KubeEvent } from "../../lib/types";
import styles from "./OverviewPage.module.css";
import { shortObject } from "./paths";

const VISIBLE = 6;
const CRITICAL_REASONS = /BackOff|Failed|Kill|Unhealthy|Evict|OOM|Error/i;
const DONE_REASONS = /^(Completed|SuccessfulCreate|SawCompletedJob)$/;

type EventTone = "critical" | "high" | "ok" | "info";

function eventTone(e: KubeEvent): EventTone {
  if (e.type === "Warning") return CRITICAL_REASONS.test(e.reason) ? "critical" : "high";
  if (DONE_REASONS.test(e.reason)) return "ok";
  return "info";
}

const ICONS = {
  critical: IconCircleX,
  high: IconAlertTriangle,
  ok: IconCircleCheck,
  info: IconInfoCircle,
};

const WORDS: Record<EventTone, string> = {
  critical: "Problem",
  high: "Warning",
  ok: "Done",
  info: "Info",
};

/** The newest events, live from the WebSocket stream. */
export function RecentEvents() {
  const { data, isPending, error } = useEvents();
  const { status } = useLive();
  const now = useNow(15_000);
  const events = (data ?? []).slice(0, VISIBLE);

  return (
    <Card
      title="Recent events"
      className={styles.events}
      actions={
        <span className={styles.live} data-live={status === "live"}>
          <span className={styles.liveDot} aria-hidden="true" />
          {status === "live" ? "Live" : status === "reconnecting" ? "Reconnecting" : "Connecting"}
        </span>
      }
    >
      {isPending && <div className={styles.skeletonRows} />}
      {error && <p className={styles.sectionNote}>{error.message}</p>}
      {data?.length === 0 && <p className={styles.sectionNote}>No events yet. Quiet is good.</p>}
      {events.length > 0 && (
        <ul className={styles.rows}>
          {events.map((e) => {
            const tone = eventTone(e);
            const Icon = ICONS[tone];
            return (
              <li key={e.id} className={styles.event}>
                <Icon
                  size={16}
                  stroke={1.75}
                  className={styles.eventIcon}
                  data-tone={tone}
                  aria-hidden="true"
                />
                <span className="visually-hidden">{WORDS[tone]}: </span>
                <span className={styles.eventText}>
                  <span className={styles.eventMessage} title={e.message}>
                    {e.message || e.reason}
                  </span>
                  <span className={styles.eventObject}>
                    {shortObject(e.object.kind, e.object.name)}
                  </span>
                </span>
                <time
                  className={styles.age}
                  dateTime={e.time}
                  title={new Date(e.time).toLocaleString()}
                >
                  {formatAgeShort(now - Date.parse(e.time))}
                </time>
              </li>
            );
          })}
        </ul>
      )}
      {(data?.length ?? 0) > VISIBLE && (
        <Link to="/events" className={styles.moreLink}>
          See all events
        </Link>
      )}
    </Card>
  );
}
