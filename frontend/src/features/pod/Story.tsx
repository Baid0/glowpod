import { IconBulb } from "@tabler/icons-react";

import { Card } from "../../components/Card";
import { formatAge, useNow } from "../../lib/time";
import type { PodDetail } from "../../lib/types";
import styles from "./PodPage.module.css";
import { Link } from "../../lib/clusterLinks";

const CRASH_REASONS = new Set(["CrashLoopBackOff", "OOMKilled", "Error"]);

const TONE_WORDS = { ok: "Good", info: "Info", high: "Warning", critical: "Problem" } as const;

/** What happened to the pod, oldest first, ending with where it stands. */
export function Story({ data }: { data: PodDetail }) {
  const now = useNow(30_000);
  const problem = data.pod.problem;
  // A container that crashed has a previous run whose log usually says why.
  const crashed =
    problem && CRASH_REASONS.has(problem.reason)
      ? data.containers.find((c) => c.lastTermination && c.restarts > 0)?.name
      : undefined;
  return (
    <Card title="What happened" className={styles.story}>
      {problem && (
        <div className={styles.fix} data-tone={problem.severity}>
          <p className={styles.fixTitle}>
            {problem.title} <span className={styles.term}>({problem.reason})</span>
          </p>
          {problem.detail && <p className={styles.fixDetail}>{problem.detail}</p>}
          <p className={styles.fixHint}>
            <IconBulb size={16} stroke={1.75} aria-hidden="true" />
            <span>
              <strong>Suggested fix:</strong> {problem.hint}
              {crashed && (
                <>
                  {" "}
                  <Link to={`?tab=logs&container=${encodeURIComponent(crashed)}&previous=1`}>
                    Open the last crash&apos;s log
                  </Link>
                </>
              )}
            </span>
          </p>
        </div>
      )}
      {data.story.length === 0 ? (
        <p className={styles.muted}>Nothing to tell yet.</p>
      ) : (
        <ol className={styles.timeline}>
          {data.story.map((s, i) => (
            <li key={`${s.time}-${String(i)}`} className={styles.step} data-tone={s.tone}>
              <span className={styles.stepDot} aria-hidden="true" />
              <div className={styles.stepBody}>
                <p className={styles.stepTitle}>
                  <span className="visually-hidden">{TONE_WORDS[s.tone]}: </span>
                  {s.title}
                  {s.count > 1 && <span className={styles.count}> ×{s.count}</span>}
                </p>
                {s.detail && <p className={styles.stepDetail}>{s.detail}</p>}
              </div>
              <time
                className={styles.stepTime}
                dateTime={s.time}
                title={new Date(s.time).toLocaleString()}
              >
                {formatAge(now - Date.parse(s.time))} ago
              </time>
            </li>
          ))}
        </ol>
      )}
    </Card>
  );
}
