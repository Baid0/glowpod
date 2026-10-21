import { Link } from "react-router";

import { Badge, type StatusTone } from "../../components/Badge";
import { Card } from "../../components/Card";
import { usePreferences } from "../../lib/preferences";
import { formatAge, useNow } from "../../lib/time";
import type { AppStatus, Overview, WorkloadCount } from "../../lib/types";
import styles from "./OverviewPage.module.css";

interface Segment {
  value: number;
  tone: "ok" | "high" | "critical";
}

/** Thin bar split by state; decorative, the numbers are in the text. */
function SegmentBar({ segments }: { segments: Segment[] }) {
  const shown = segments.filter((s) => s.value > 0);
  return (
    <div className={styles.segments} aria-hidden="true">
      {shown.length === 0 ? (
        <span className={styles.segment} data-tone="empty" style={{ flexGrow: 1 }} />
      ) : (
        shown.map((s) => (
          <span
            key={s.tone}
            className={styles.segment}
            data-tone={s.tone}
            style={{ flexGrow: s.value }}
          />
        ))
      )}
    </div>
  );
}

function workloadBadge(w: WorkloadCount): [StatusTone, string] {
  if (w.total === 0) return ["neutral", "None yet"];
  if (w.failing > 0) return ["critical", `${w.failing} failing`];
  if (w.pending > 0) return ["high", `${w.pending} pending`];
  return ["ok", "All ready"];
}

function Tile({
  title,
  badge,
  big,
  rest,
  segments,
}: {
  title: string;
  badge: [StatusTone, string];
  big: number;
  rest: string;
  segments: Segment[];
}) {
  return (
    <Card title={title} headingLevel={3} actions={<Badge tone={badge[0]}>{badge[1]}</Badge>}>
      <p className={styles.tileNumber}>
        <span className={styles.big}>{big}</span> <span className={styles.tileRest}>{rest}</span>
      </p>
      <SegmentBar segments={segments} />
    </Card>
  );
}

/** Deep mode: counts by kind, ready vs total. */
export function WorkloadTiles({ data }: { data: Overview | undefined }) {
  const missing = data?.unavailable?.find((u) => u.section === "workloads");
  if (!data || missing) {
    return (
      <section className={styles.tiles} aria-label="Workloads">
        {missing ? (
          <p className={styles.sectionNote}>Workloads: {missing.message}</p>
        ) : (
          [0, 1, 2, 3].map((i) => <div key={i} className={styles.skeletonTile} />)
        )}
      </section>
    );
  }
  const w = data.workloads;
  const tile = (title: string, c: WorkloadCount) => (
    <Tile
      title={title}
      badge={workloadBadge(c)}
      big={c.ready}
      rest={`/ ${c.total} ready`}
      segments={[
        { value: c.ready, tone: "ok" },
        { value: c.pending, tone: "high" },
        { value: c.failing, tone: "critical" },
      ]}
    />
  );
  const j = w.jobs;
  return (
    <section className={styles.tiles} aria-label="Workloads">
      {tile("Deployments", w.deployments)}
      {tile("StatefulSets", w.statefulSets)}
      {tile("DaemonSets", w.daemonSets)}
      <Tile
        title="Jobs (24h)"
        badge={j.failed > 0 ? ["critical", `${j.failed} failed`] : ["ok", "0 failed"]}
        big={j.succeeded}
        rest="succeeded"
        segments={[
          { value: j.succeeded, tone: "ok" },
          { value: j.running, tone: "high" },
          { value: j.failed, tone: "critical" },
        ]}
      />
    </section>
  );
}

const APP_LIMIT = 8;

const appBadge: Record<AppStatus, [StatusTone, string]> = {
  ok: ["ok", "Healthy"],
  high: ["high", "Rolling out"],
  critical: ["critical", "Failing"],
};

/** Glance mode: one card per app, problems first. */
export function AppCards({ data }: { data: Overview | undefined }) {
  const { setMode } = usePreferences();
  const now = useNow(60_000);
  const apps = data?.apps ?? [];

  return (
    <section className={styles.appsSection} aria-labelledby="apps-heading">
      <div className={styles.sectionHead}>
        <h2 id="apps-heading" className={styles.sectionTitle}>
          Your apps
        </h2>
        <button
          type="button"
          className={styles.textButton}
          onClick={() => {
            setMode("deep");
          }}
        >
          Show in Deep
        </button>
      </div>
      {data && apps.length === 0 && (
        <p className={styles.sectionNote}>Nothing growing here yet. Deploy something?</p>
      )}
      <div className={styles.tiles}>
        {!data && [0, 1, 2, 3].map((i) => <div key={i} className={styles.skeletonTile} />)}
        {apps.slice(0, APP_LIMIT).map((a) => {
          const [tone, word] = appBadge[a.status];
          return (
            <Card
              key={a.namespace + "/" + a.name}
              title={
                <Link
                  className={styles.cardLink}
                  title={a.name}
                  to={`/apps/${encodeURIComponent(a.namespace)}/${encodeURIComponent(a.name)}`}
                >
                  {a.name}
                </Link>
              }
              headingLevel={3}
              actions={<Badge tone={tone}>{word}</Badge>}
              className={styles.appCard}
            >
              <p className={styles.appMeta}>{a.namespace}</p>
              <p className={styles.tileNumber}>
                <span className={styles.big}>{a.ready}</span>{" "}
                <span className={styles.tileRest}>/ {a.total} ready</span>
              </p>
              {a.lastDeploy && (
                <p className={styles.appMeta}>
                  Deployed {formatAge(now - Date.parse(a.lastDeploy))} ago
                </p>
              )}
            </Card>
          );
        })}
      </div>
      {apps.length > APP_LIMIT && (
        <Link to="/apps" className={styles.moreLink}>
          See all {apps.length} apps
        </Link>
      )}
    </section>
  );
}
