import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router";

import { Card } from "../../components/Card";
import { api } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { MapStatus, ResourceMap } from "../../lib/types";
import { ObjectError } from "../yaml/ObjectError";
import { BOX_H, BOX_W, fit, GAP_X, layout, nodePath, type Placed } from "./layout";
import styles from "./MapPage.module.css";

const HEADINGS = ["Comes in", "Services", "Runs", "Uses"];
const KIND_WORDS: Record<string, string> = {
  PersistentVolumeClaim: "Volume claim",
};

const DOT: Record<MapStatus, string | null> = {
  ok: "var(--state-ok)",
  high: "var(--state-high)",
  critical: "var(--state-critical)",
  neutral: null,
};

/** One namespace as a picture: how traffic gets in, what runs, what it uses. */
export function MapPage() {
  const cluster = useClusterId();
  const { namespace } = usePreferences();
  const { data, error, isPending, refetch } = useQuery({
    queryKey: queryKeys.resourceMap(cluster, namespace),
    queryFn: ({ signal }) => api.resourceMap(cluster, namespace, signal),
    enabled: namespace !== "",
    refetchInterval: 30_000,
  });

  if (!namespace) {
    return (
      <Card title="Resource map">
        <p className={styles.muted}>
          Pick a namespace in the top bar. A map of the whole cluster would be too big to read.
        </p>
      </Card>
    );
  }
  return (
    <Card title={`Map of ${namespace}`}>
      {isPending ? (
        <div className={styles.skeleton} aria-label="Loading the map" />
      ) : error ? (
        <ObjectError error={error} onRetry={() => void refetch()} />
      ) : data.nodes.length === 0 ? (
        <p className={styles.muted}>Nothing runs in {namespace} yet.</p>
      ) : (
        <MapView map={data} />
      )}
    </Card>
  );
}

function MapView({ map }: { map: ResourceMap }) {
  const navigate = useNavigate();
  const [focus, setFocus] = useState<string | null>(null);
  const { nodes, width, height, columns } = useMemo(() => layout(map), [map]);
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const linked = useMemo(() => {
    if (!focus) return null;
    const ids = new Set([focus]);
    for (const e of map.edges) {
      if (e.from === focus) ids.add(e.to);
      if (e.to === focus) ids.add(e.from);
    }
    return ids;
  }, [focus, map.edges]);
  const open = (n: Placed) => {
    const path = nodePath(n);
    if (path) void navigate(path);
  };

  return (
    <div className={styles.view}>
      {map.truncated && (
        <p className={styles.muted}>This namespace is big: the map shows the first 400 objects.</p>
      )}
      <div className={styles.scroll}>
        {/* Scales down to fit, to 70% at most; past that it scrolls. */}
        <svg
          viewBox={`0 0 ${String(width)} ${String(height)}`}
          className={styles.svg}
          style={{ maxWidth: width, minWidth: Math.round(width * 0.7) }}
          role="group"
          aria-label={`Map of ${map.namespace}`}
        >
          <g aria-hidden="true">
            {columns.map((c, i) => (
              <text key={c} x={i * (BOX_W + GAP_X)} y={12} className={styles.heading}>
                {HEADINGS[c]}
              </text>
            ))}
          </g>
          <g aria-hidden="true">
            {map.edges.map((e) => {
              const a = byId.get(e.from);
              const b = byId.get(e.to);
              if (!a || !b) return null;
              const x1 = a.x + BOX_W;
              const y1 = a.y + BOX_H / 2;
              const x2 = b.x;
              const y2 = b.y + BOX_H / 2;
              const mid = (x1 + x2) / 2;
              const lit = linked ? linked.has(e.from) && linked.has(e.to) : false;
              return (
                <path
                  key={e.from + ">" + e.to}
                  d={`M${String(x1)},${String(y1)} C${String(mid)},${String(y1)} ${String(mid)},${String(y2)} ${String(x2)},${String(y2)}`}
                  className={styles.edge}
                  data-lit={lit}
                  data-dim={linked !== null && !lit}
                />
              );
            })}
          </g>
          {nodes.map((n) => {
            const path = nodePath(n);
            const dot = DOT[n.status];
            const label = `${KIND_WORDS[n.kind] ?? n.kind} ${n.name}${n.detail ? `, ${n.detail}` : ""}`;
            return (
              <g
                key={n.id}
                transform={`translate(${String(n.x)},${String(n.y)})`}
                className={styles.node}
                data-dim={linked !== null && !linked.has(n.id)}
                data-missing={n.missing ?? false}
                {...(path
                  ? {
                      role: "link",
                      tabIndex: 0,
                      "aria-label": label,
                      onClick: () => {
                        open(n);
                      },
                      onKeyDown: (e: React.KeyboardEvent) => {
                        if (e.key === "Enter") open(n);
                      },
                    }
                  : { role: "img", "aria-label": label })}
                onMouseEnter={() => {
                  setFocus(n.id);
                }}
                onMouseLeave={() => {
                  setFocus(null);
                }}
                onFocus={() => {
                  setFocus(n.id);
                }}
                onBlur={() => {
                  setFocus(null);
                }}
              >
                <title>{label}</title>
                <rect width={BOX_W} height={BOX_H} rx={8} className={styles.box} />
                {dot && <circle cx={16} cy={BOX_H / 2} r={4} fill={dot} className={styles.dot} />}
                <text x={dot ? 30 : 14} y={22} className={styles.kind}>
                  {KIND_WORDS[n.kind] ?? n.kind}
                  {n.detail ? ` · ${fit(n.detail, 20)}` : ""}
                </text>
                <text x={dot ? 30 : 14} y={40} className={styles.name}>
                  {fit(n.name, dot ? 24 : 26)}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}
