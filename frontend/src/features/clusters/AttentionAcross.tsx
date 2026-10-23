import { IconCircleCheck } from "@tabler/icons-react";
import { useState } from "react";

import { Card } from "../../components/Card";
import { usePreferences } from "../../lib/preferences";
import { useClusterSummaries } from "../../lib/queries";
import { formatAge, useNow } from "../../lib/time";
import type { ClusterInfo, Problem } from "../../lib/types";
import { ProblemRow } from "../overview/Attention";
import overview from "../overview/OverviewPage.module.css";
import { agentDown } from "./agent";

const VISIBLE = 8;

interface Row {
  cluster: ClusterInfo;
  p: Problem;
}

/**
 * "Needs attention" over every cluster in the list: the same rows as each
 * Overview, most severe and newest first, each saying which cluster it's on.
 * It reads the summaries the table's rows already load.
 */
export function AttentionAcross({ clusters }: { clusters: ClusterInfo[] }) {
  const { mode } = usePreferences();
  const deep = mode === "deep";
  const now = useNow(30_000);
  const [expanded, setExpanded] = useState(false);
  const results = useClusterSummaries(clusters.map((c) => ({ id: c.id, enabled: !agentDown(c) })));

  const rows: Row[] = [];
  let total = 0;
  let checked = 0;
  let unchecked = 0;
  let loading = false;
  for (const [i, cluster] of clusters.entries()) {
    const r = results[i];
    if (r?.data === undefined) {
      if (r?.isPending && !agentDown(cluster)) loading = true;
      else unchecked++;
      continue;
    }
    checked++;
    // Glance lists the pods only, like the Overview.
    const problems = r.data.problems.filter((p) => deep || p.kind === "Pod");
    total += deep ? r.data.problemCount : problems.length;
    for (const p of problems) rows.push({ cluster, p });
  }
  rows.sort(
    (a, b) =>
      severityRank(a.p) - severityRank(b.p) || Date.parse(b.p.since) - Date.parse(a.p.since),
  );
  const shown = expanded ? rows : rows.slice(0, VISIBLE);

  return (
    <Card
      title="Needs attention"
      actions={
        rows.length > VISIBLE && (
          <button
            type="button"
            className={overview.textButton}
            aria-expanded={expanded}
            onClick={() => {
              setExpanded((e) => !e);
            }}
          >
            {expanded ? "Show fewer" : `See all ${String(total)}`}
          </button>
        )
      }
    >
      {loading && rows.length === 0 && <div className={overview.skeletonRows} />}
      {!loading && rows.length === 0 && checked > 0 && (
        <p className={overview.allGood}>
          <IconCircleCheck size={20} stroke={1.75} aria-hidden="true" className={overview.okIcon} />
          {checked === 1
            ? "Nothing needs attention in this cluster."
            : `Nothing needs attention in ${String(checked)} clusters.`}
        </p>
      )}
      {rows.length > 0 && (
        <ul className={overview.rows}>
          {shown.map(({ cluster, p }) => (
            <li
              key={`${cluster.id}/${p.kind}/${p.namespace ?? ""}/${p.name}/${p.reason}`}
              className={overview.problem}
            >
              <ProblemRow
                p={p}
                deep={deep}
                age={formatAge(now - Date.parse(p.since))}
                cluster={cluster}
              />
            </li>
          ))}
        </ul>
      )}
      {total > rows.length && (
        <p className={overview.sectionNote}>
          Showing the first {rows.length} of {total}. Open a cluster to see the rest.
        </p>
      )}
      {unchecked > 0 && (
        <p className={overview.sectionNote}>
          {unchecked === 1
            ? "1 cluster couldn't be checked; its state above says why."
            : `${String(unchecked)} clusters couldn't be checked; their state above says why.`}
        </p>
      )}
    </Card>
  );
}

const severityRank = (p: Problem) => (p.severity === "critical" ? 0 : 1);
