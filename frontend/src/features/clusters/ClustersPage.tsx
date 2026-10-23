import { IconPlus } from "@tabler/icons-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";

import { Badge, type StatusTone } from "../../components/Badge";
import { Button } from "../../components/Button";
import { HalfGauge, type GaugeState } from "../../components/HalfGauge";
import { ApiError } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { useClusters, useClusterSummary, useSession } from "../../lib/queries";
import { formatAge, useNow } from "../../lib/time";
import type { ClusterInfo, ClusterSummary, Gauge } from "../../lib/types";
import { ImportDialog, RemoveDialog } from "./HubDialogs";
import styles from "./ClustersPage.module.css";

/** Home when there's more than one cluster: every cluster, one row each. */
export function ClustersPage() {
  const { data: clusters, error, isPending } = useClusters();
  const { data: session } = useSession();
  const [search, setSearch] = useState("");
  const admin = session?.hub?.role === "admin";
  // "new" imports a cluster; a cluster gets a fresh command for its agent.
  const [importing, setImporting] = useState<ClusterInfo | "new" | undefined>();
  const [removing, setRemoving] = useState<ClusterInfo | undefined>();

  const words = search.toLowerCase().split(/\s+/).filter(Boolean);
  const shown = (clusters ?? []).filter((c) =>
    words.every((w) => `${c.name} ${c.server}`.toLowerCase().includes(w)),
  );

  return (
    <div className={styles.page}>
      <div className={styles.head}>
        <input
          type="search"
          className={styles.search}
          aria-label="Filter clusters"
          placeholder="Filter by name or server"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
          }}
        />
        {session?.mode === "local" && clusters && (
          <p className={styles.note}>
            {clusters.length} contexts in your kubeconfig. Each opens as the user its context names.
          </p>
        )}
        {session?.hub && (
          <p className={styles.note}>
            {admin
              ? "As a Hub administrator you see every cluster. What you can do in each still depends on its own roles."
              : "The clusters where you have access."}
          </p>
        )}
        {admin && (
          <Button
            variant="primary"
            className={styles.import}
            icon={<IconPlus size={16} stroke={1.75} />}
            onClick={() => {
              setImporting("new");
            }}
          >
            Import
          </Button>
        )}
      </div>
      {error && <p className={styles.note}>{error.message}</p>}
      {isPending && <div className={styles.skeleton} />}
      {clusters && (
        <div className={styles.card}>
          <div className={styles.scroller} tabIndex={0} aria-label="Clusters table">
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">State</th>
                  <th scope="col">Kubernetes</th>
                  <th scope="col">Nodes</th>
                  <th scope="col" className={styles.gaugeHead}>
                    CPU
                  </th>
                  <th scope="col" className={styles.gaugeHead}>
                    Memory
                  </th>
                  <th scope="col" className={styles.gaugeHead}>
                    Pods
                  </th>
                  <th scope="col">Needs attention</th>
                  {admin && <th scope="col">Actions</th>}
                </tr>
              </thead>
              <tbody>
                {shown.map((c) => (
                  <ClusterRow
                    key={c.id}
                    cluster={c}
                    {...(admin && c.state
                      ? {
                          onConnect: () => {
                            setImporting(c);
                          },
                          onRemove: () => {
                            setRemoving(c);
                          },
                        }
                      : {})}
                    admin={admin}
                  />
                ))}
              </tbody>
            </table>
          </div>
          {shown.length === 0 && (
            <p className={styles.empty}>
              {search
                ? `Nothing matches “${search}”.`
                : admin
                  ? "No clusters yet. Import one to get started."
                  : "You don't have access to any cluster yet. Ask an admin to add you."}
            </p>
          )}
        </div>
      )}
      <ImportDialog
        open={importing !== undefined}
        onClose={() => {
          setImporting(undefined);
        }}
        {...(importing && importing !== "new" ? { cluster: importing } : {})}
      />
      <RemoveDialog
        cluster={removing}
        onClose={() => {
          setRemoving(undefined);
        }}
      />
    </div>
  );
}

function ClusterRow({
  cluster,
  admin,
  onConnect,
  onRemove,
}: {
  cluster: ClusterInfo;
  admin: boolean;
  /** Imported clusters on a Hub: a new command for the agent, or remove it. */
  onConnect?: () => void;
  onRemove?: () => void;
}) {
  const navigate = useNavigate();
  // A Hub's imported cluster without its agent has nothing to ask.
  const agentDown = cluster.state === "pending" || cluster.state === "disconnected";
  const summary = useClusterSummary(cluster.id, !agentDown);
  const { data, error } = summary;
  const isPending = summary.isPending && !agentDown;
  const { thresholdsFor } = usePreferences();
  const now = useNow(30_000);
  const href = `/c/${encodeURIComponent(cluster.id)}/`;
  const thresholds = thresholdsFor(cluster.id) ?? data?.thresholds;
  const state = agentDown ? agentState(cluster, now) : rowState(data, error, isPending);

  return (
    <tr
      className={styles.row}
      onClick={(e) => {
        // The name is the link; the rest of the row (but its buttons) is a bigger target for a mouse.
        if (!(e.target instanceof Element && e.target.closest("a, button"))) void navigate(href);
      }}
    >
      <td className={styles.nameCell}>
        <Link to={href} className={styles.name}>
          {cluster.name}
        </Link>
        <span className={styles.server}>
          {cluster.server}
          {cluster.current && <span className={styles.current}> · current</span>}
        </span>
      </td>
      <td>
        <span title={state.detail}>
          <Badge tone={state.tone}>{state.word}</Badge>
        </span>
      </td>
      <td className={styles.num}>{data?.cluster.version ?? dash(isPending)}</td>
      <td className={styles.num}>{data ? nodes(data) : dash(isPending)}</td>
      {(["cpu", "memory", "pods"] as const).map((kind) => (
        <td key={kind} className={styles.gaugeCell}>
          {/* A cluster that didn't answer has no load to show; the state says why. */}
          {(error || agentDown) && !data ? (
            dash(false)
          ) : (
            <MiniGauge
              label={LABELS[kind]}
              gauge={data?.gauges[kind]}
              state={gaugeState(data?.gauges[kind], isPending)}
              {...(thresholds ? { thresholds } : {})}
            />
          )}
        </td>
      ))}
      <td className={styles.num}>{data ? attention(data.problemCount) : dash(isPending)}</td>
      {admin && (
        <td className={styles.actions}>
          {onConnect && agentDown && (
            <Button size="sm" onClick={onConnect}>
              Connect
            </Button>
          )}
          {onRemove && (
            <Button size="sm" variant="ghost" onClick={onRemove}>
              Remove
            </Button>
          )}
        </td>
      )}
    </tr>
  );
}

/** An imported cluster whose agent isn't connected. */
function agentState(cluster: ClusterInfo, now: number): RowState {
  if (cluster.state === "pending") {
    return {
      tone: "neutral",
      word: "Waiting for agent",
      detail: "Run the import command in the cluster.",
    };
  }
  const since = cluster.since ? Date.parse(cluster.since) : NaN;
  return {
    tone: "high",
    word: Number.isNaN(since) ? "Disconnected" : `Disconnected · ${formatAge(now - since)}`,
    detail: "The cluster's agent isn't connected to the Hub.",
  };
}

const LABELS = { cpu: "CPU", memory: "Memory", pods: "Pods" } as const;

function MiniGauge({
  label,
  gauge,
  state,
  thresholds,
}: {
  label: string;
  gauge: Gauge | undefined;
  state: GaugeState;
  thresholds?: [number, number];
}) {
  return (
    <div className={styles.mini}>
      <HalfGauge
        label={label}
        value={gauge?.value ?? 0}
        max={gauge?.max ?? 0}
        size="S"
        valueInside
        state={state}
        {...(thresholds ? { thresholds } : {})}
      />
    </div>
  );
}

function gaugeState(g: Gauge | undefined, pending: boolean): GaugeState {
  if (pending) return "loading";
  if (!g) return "error";
  return g.state === "no-access" ? "no-metrics" : g.state;
}

interface RowState {
  tone: StatusTone;
  word: string;
  /** The reason, on hover. */
  detail?: string;
}

/** What a row's summary says about the connection. */
function rowState(
  data: ClusterSummary | undefined,
  error: Error | null,
  pending: boolean,
): RowState {
  if (data) return { tone: "ok", word: "Connected" };
  if (pending) return { tone: "neutral", word: "Checking" };
  if (error instanceof ApiError) {
    if (error.code === "cluster_unauthorized")
      return { tone: "high", word: "Credentials refused", detail: error.message };
    if (error.status === 403) return { tone: "neutral", word: "No access", detail: error.message };
    return { tone: "high", word: "Unreachable", detail: error.message };
  }
  return { tone: "high", word: "Unreachable", ...(error ? { detail: error.message } : {}) };
}

function nodes(s: ClusterSummary): string {
  const { nodes, nodesReady } = s.cluster;
  return nodesReady === nodes
    ? `${String(nodes)} ready`
    : `${String(nodesReady)} of ${String(nodes)} ready`;
}

function attention(n: number) {
  if (n === 0) return <span className={styles.muted}>None</span>;
  return (
    <span className={styles.problems}>
      <span className={styles.problemDot} aria-hidden="true" />
      {n}
    </span>
  );
}

function dash(pending: boolean) {
  return <span className={styles.muted}>{pending ? "…" : "—"}</span>;
}
