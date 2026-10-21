import { IconCircleCheck } from "@tabler/icons-react";
import { useState } from "react";

import { Badge } from "../../components/Badge";
import { ButtonLink } from "../../components/Button";
import { Card } from "../../components/Card";
import { usePreferences } from "../../lib/preferences";
import { formatAge, useNow } from "../../lib/time";
import type { Overview, Problem } from "../../lib/types";
import styles from "./OverviewPage.module.css";
import { problemPath } from "./paths";

const VISIBLE = 6;

/**
 * Problems in plain language. Deep shows reason codes, node pressure and
 * failed jobs; Glance shows the pods, in words, with a suggested fix.
 */
export function NeedsAttention({ data }: { data: Overview | undefined }) {
  const { mode } = usePreferences();
  const now = useNow(30_000);
  const [expanded, setExpanded] = useState(false);
  const problems = (data?.problems ?? []).filter((p) => mode === "deep" || p.kind === "Pod");
  const total = mode === "deep" ? (data?.problemCount ?? 0) : problems.length;
  const shown = expanded ? problems : problems.slice(0, VISIBLE);

  return (
    <Card
      title="Needs attention"
      className={styles.attention}
      actions={
        problems.length > VISIBLE && (
          <button
            type="button"
            className={styles.textButton}
            aria-expanded={expanded}
            onClick={() => {
              setExpanded((e) => !e);
            }}
          >
            {expanded ? "Show fewer" : `See all ${total}`}
          </button>
        )
      }
    >
      {!data && <div className={styles.skeletonRows} />}
      {data && problems.length === 0 && (
        <p className={styles.allGood}>
          <IconCircleCheck size={20} stroke={1.75} aria-hidden="true" className={styles.okIcon} />
          {healthyLine(data)}
        </p>
      )}
      {problems.length > 0 && (
        <ul className={styles.rows}>
          {shown.map((p) => (
            <li
              key={`${p.kind}/${p.namespace ?? ""}/${p.name}/${p.reason}`}
              className={styles.problem}
            >
              {mode === "deep" ? (
                <DeepRow p={p} age={formatAge(now - Date.parse(p.since))} />
              ) : (
                <GlanceRow p={p} age={formatAge(now - Date.parse(p.since))} />
              )}
            </li>
          ))}
        </ul>
      )}
      {total > problems.length && mode === "deep" && (
        <p className={styles.sectionNote}>
          Showing the first {problems.length} of {total}.
        </p>
      )}
    </Card>
  );
}

function healthyLine(data: Overview): string {
  const pods = data.health.running + data.health.pending;
  if (pods === 0) return "Nothing growing here yet.";
  return `All ${pods} ${pods === 1 ? "pod looks" : "pods look"} healthy.`;
}

function DeepRow({ p, age }: { p: Problem; age: string }) {
  return (
    <>
      <span className={styles.problemBadge}>
        <Badge tone={p.severity}>{p.reason}</Badge>
      </span>
      <span className={styles.problemText}>
        <span className={styles.problemName}>{p.name}</span>
        <span className={styles.problemDetail}>
          {[p.namespace ?? p.kind.toLowerCase(), p.detail].filter(Boolean).join(" · ")}
        </span>
      </span>
      <span className={styles.age}>{age}</span>
      <ButtonLink to={problemPath(p)} size="sm" aria-label={`View ${p.name}`}>
        View
      </ButtonLink>
    </>
  );
}

function GlanceRow({ p, age }: { p: Problem; age: string }) {
  return (
    <>
      <span className={styles.problemDot} data-tone={p.severity} aria-hidden="true" />
      <span className={styles.problemText}>
        <span className={styles.problemTitle}>
          {p.title} <span className={styles.term}>({p.reason})</span>
        </span>
        <span className={styles.problemDetail}>
          <span className={styles.mono}>{p.name}</span> in {p.namespace}. {p.hint}
        </span>
      </span>
      <span className={styles.age}>{age}</span>
      <ButtonLink to={problemPath(p)} size="sm" aria-label={`View ${p.name}`}>
        View
      </ButtonLink>
    </>
  );
}
