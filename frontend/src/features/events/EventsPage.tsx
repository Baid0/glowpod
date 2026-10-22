import {
  IconAlertTriangle,
  IconCircleCheck,
  IconCircleX,
  IconInfoCircle,
  IconSearch,
} from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { Badge, type StatusTone } from "../../components/Badge";
import { Card } from "../../components/Card";
import { api } from "../../lib/api";
import { cx } from "../../lib/cx";
import { PAGE_EVENTS, useLive } from "../../lib/live";
import { usePreferences } from "../../lib/preferences";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import { formatAgeShort, useNow } from "../../lib/time";
import type { KubeEvent } from "../../lib/types";
import { shortObject } from "../overview/paths";
import styles from "./EventsPage.module.css";
import { EVENT_WORDS, eventObjectPath, eventTone, type EventTone } from "./tone";

const FILTERS = [
  { id: "all", label: "All" },
  { id: "problems", label: "Problems" },
  { id: "warnings", label: "Warnings" },
  { id: "normal", label: "Normal" },
] as const;
type Filter = (typeof FILTERS)[number]["id"];

const BADGE: Record<EventTone, StatusTone> = {
  critical: "critical",
  high: "high",
  ok: "ok",
  info: "neutral",
};
const ICONS = {
  critical: IconCircleX,
  high: IconAlertTriangle,
  ok: IconCircleCheck,
  info: IconInfoCircle,
};

function matches(e: KubeEvent, filter: Filter, q: string): boolean {
  const tone = eventTone(e);
  if (filter === "problems" && tone !== "critical") return false;
  if (filter === "warnings" && e.type !== "Warning") return false;
  if (filter === "normal" && e.type === "Warning") return false;
  if (!q) return true;
  return [e.reason, e.message, e.object.kind, e.object.name, e.object.namespace ?? ""].some((s) =>
    s.toLowerCase().includes(q),
  );
}

/** Everything the cluster reported lately, newest first, live. */
export function EventsPage() {
  const cluster = useClusterId();
  const { namespace, mode } = usePreferences();
  const { status } = useLive();
  const now = useNow(15_000);
  const [params, setParams] = useSearchParams();
  const filter: Filter = FILTERS.some((f) => f.id === params.get("show"))
    ? (params.get("show") as Filter)
    : "all";
  const [search, setSearch] = useState("");
  const { data, isPending, error } = useQuery({
    queryKey: queryKeys.eventsPage(cluster, namespace),
    queryFn: ({ signal }) => api.events(cluster, namespace, PAGE_EVENTS, signal),
    staleTime: Infinity,
  });

  const counts = useMemo(() => {
    const all = data ?? [];
    return {
      all: all.length,
      problems: all.filter((e) => eventTone(e) === "critical").length,
      warnings: all.filter((e) => e.type === "Warning").length,
      normal: all.filter((e) => e.type !== "Warning").length,
    };
  }, [data]);
  const q = search.trim().toLowerCase();
  const shown = (data ?? []).filter((e) => matches(e, filter, q));

  return (
    <Card
      title={namespace ? `Events in ${namespace}` : "Events in all namespaces"}
      actions={
        <span className={styles.live} data-live={status === "live"}>
          <span className={styles.liveDot} aria-hidden="true" />
          {status === "live" ? "Live" : status === "reconnecting" ? "Reconnecting" : "Connecting"}
        </span>
      }
    >
      <div className={styles.toolbar}>
        <div className={styles.segments} role="radiogroup" aria-label="Show">
          {FILTERS.map((f) => (
            <button
              key={f.id}
              type="button"
              role="radio"
              aria-checked={filter === f.id}
              className={cx(styles.segment, filter === f.id && styles.active)}
              onClick={() => {
                setParams(f.id === "all" ? {} : { show: f.id }, { replace: true });
              }}
            >
              {f.label}
              <span className={styles.count}>{counts[f.id]}</span>
            </button>
          ))}
        </div>
        <label className={styles.search}>
          <IconSearch size={16} stroke={1.75} aria-hidden="true" />
          <input
            type="search"
            aria-label="Search events"
            placeholder="Search reason, object or message…"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
            }}
          />
        </label>
      </div>

      {isPending && <div className={styles.skeleton} aria-label="Loading events" />}
      {error && <p className={styles.note}>{error.message}</p>}
      {data && shown.length === 0 && (
        <p className={styles.note}>
          {data.length === 0
            ? "No events yet. Quiet is good."
            : "No events match. Try another filter or search."}
        </p>
      )}
      {shown.length > 0 && (
        <ol className={styles.list} aria-label="Events, newest first">
          {shown.map((e) => {
            const tone = eventTone(e);
            const Icon = ICONS[tone];
            const path = eventObjectPath(e.object);
            const object = shortObject(e.object.kind, e.object.name);
            return (
              <li key={e.id} className={styles.row}>
                <Badge tone={BADGE[tone]} icon={<Icon size={14} stroke={1.75} />}>
                  {EVENT_WORDS[tone]}
                </Badge>
                <div className={styles.text}>
                  <p className={styles.message}>
                    {mode === "deep" && <span className={styles.reason}>{e.reason}</span>}
                    {e.message || e.reason}
                  </p>
                  <p className={styles.meta}>
                    {path ? (
                      <Link to={path} className={styles.object}>
                        {object}
                      </Link>
                    ) : (
                      <span className={styles.object}>{object}</span>
                    )}
                    {!namespace && e.object.namespace && <span>{e.object.namespace}</span>}
                    {e.count > 1 && <span>{e.count} times</span>}
                  </p>
                </div>
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
        </ol>
      )}
    </Card>
  );
}
