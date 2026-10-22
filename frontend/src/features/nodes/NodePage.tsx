import { useRef, useState, type KeyboardEvent } from "react";
import { useParams, useSearchParams } from "react-router";

import { Badge } from "../../components/Badge";
import { Card } from "../../components/Card";
import { HalfGauge } from "../../components/HalfGauge";
import { ApiError } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { formatAge, useNow } from "../../lib/time";
import type { NodeDetail } from "../../lib/types";
import { ResourceActions } from "../actions/ResourceActions";
import { gaugeView } from "../overview/gauges";
import { PodEvents } from "../pod/PodEvents";
import { PodTable } from "../pod/PodTable";
import { ObjectYaml } from "../yaml/ObjectYaml";
import { NodeActions } from "./NodeActions";
import styles from "./NodePage.module.css";
import { conditionWord } from "./words";
import { useNode } from "./useNode";

const NODES = { group: "", version: "v1", resource: "nodes" };
const TABS = [
  { id: "overview", label: "Overview" },
  { id: "pods", label: "Pods" },
  { id: "yaml", label: "YAML" },
  { id: "events", label: "Events" },
] as const;
type TabId = (typeof TABS)[number]["id"];

/** A node: its load, health, what runs on it, and cordon / drain. */
export function NodePage() {
  const { name = "" } = useParams();
  const { data, error, refetch } = useNode(name);
  const [params, setParams] = useSearchParams();
  const tab: TabId = TABS.some((t) => t.id === params.get("tab"))
    ? (params.get("tab") as TabId)
    : "overview";
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [editing, setEditing] = useState(false);
  const now = useNow(30_000);

  const select = (id: TabId) => {
    setParams(id === "overview" ? {} : { tab: id }, { replace: true });
  };
  const onTabKey = (e: KeyboardEvent, i: number) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const next = (i + (e.key === "ArrowRight" ? 1 : -1) + TABS.length) % TABS.length;
    const t = TABS[next];
    if (!t) return;
    select(t.id);
    tabRefs.current[next]?.focus();
  };

  if (error && !data) {
    const gone = error instanceof ApiError && error.status === 404;
    return (
      <div className={styles.gone} role="alert">
        <h2 className={styles.goneTitle}>
          {gone ? "This node is gone" : "Couldn't load this node"}
        </h2>
        <p>{gone ? "It was removed from the cluster." : error.message}</p>
        {!gone && (
          <button type="button" className={styles.retry} onClick={() => void refetch()}>
            Retry
          </button>
        )}
      </div>
    );
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.titleRow}>
          <h2 className={styles.name}>{name}</h2>
          {data && (
            <Badge tone={data.ready ? "ok" : "critical"}>
              {data.ready ? "Ready" : "Not ready"}
            </Badge>
          )}
          {data?.unschedulable && <Badge tone="high">Cordoned</Badge>}
          <div className={styles.actions}>
            {data && <NodeActions name={name} cordoned={data.unschedulable} />}
            <ResourceActions
              resource={NODES}
              namespace=""
              name={name}
              kind="Node"
              {...(editing
                ? {}
                : {
                    onEdit: () => {
                      select("yaml");
                      setEditing(true);
                    },
                  })}
            />
          </div>
        </div>
        {data && (
          <dl className={styles.facts}>
            <Fact label="Roles">{data.roles.length > 0 ? data.roles.join(", ") : "worker"}</Fact>
            <Fact label="Age">{formatAge(now - Date.parse(data.createdAt))}</Fact>
            <Fact label="Kubelet">{data.info.kubeletVersion}</Fact>
            {data.addresses
              .filter((a) => a.type === "InternalIP")
              .map((a) => (
                <Fact key={a.address} label="IP">
                  {a.address}
                </Fact>
              ))}
          </dl>
        )}
      </header>

      <div className={styles.tabs} role="tablist" aria-label="Node views">
        {TABS.map((t, i) => (
          <button
            key={t.id}
            ref={(el) => {
              tabRefs.current[i] = el;
            }}
            type="button"
            role="tab"
            id={`tab-${t.id}`}
            aria-selected={tab === t.id}
            aria-controls={`panel-${t.id}`}
            tabIndex={tab === t.id ? 0 : -1}
            className={styles.tab}
            onClick={() => {
              select(t.id);
            }}
            onKeyDown={(e) => {
              onTabKey(e, i);
            }}
          >
            {t.label}
            {data && (t.id === "pods" || t.id === "events") && (
              <span className={styles.tabCount}>
                {t.id === "pods" ? data.pods.length : data.events.length}
              </span>
            )}
          </button>
        ))}
      </div>

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "yaml" ? (
          <ObjectYaml
            resource={NODES}
            namespace=""
            name={name}
            editing={editing}
            onEditingChange={setEditing}
          />
        ) : !data ? (
          <div className={styles.skeleton} aria-label="Loading the node" />
        ) : tab === "overview" ? (
          <Overview data={data} />
        ) : tab === "pods" ? (
          <Card title="Pods on this node">
            <PodTable pods={data.pods} now={now} caption="Pods on this node" show="namespace" />
          </Card>
        ) : (
          <PodEvents events={data.events} />
        )}
      </div>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className={styles.fact}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function Overview({ data }: { data: NodeDetail }) {
  const { mode } = usePreferences();
  const now = useNow(30_000);
  const ctx = { loading: false, stale: false };
  const labels = Object.entries(data.labels ?? {});
  return (
    <div className={styles.overview}>
      <section className={styles.gauges} aria-label="Node load">
        <Card padding="gauge">
          <HalfGauge {...gaugeView("cpu", data.gauges.cpu, ctx)} />
        </Card>
        <Card padding="gauge">
          <HalfGauge {...gaugeView("memory", data.gauges.memory, ctx)} />
        </Card>
        <Card padding="gauge">
          <HalfGauge
            {...gaugeView("pods", data.gauges.pods, ctx)}
            note={`of ${String(data.gauges.pods.max)} this node can run`}
          />
        </Card>
      </section>

      <div className={styles.columns}>
        <Card title="Health">
          <ul className={styles.conditions}>
            {data.conditions.map((c) => (
              <li key={c.type} className={styles.condition}>
                <Badge tone={c.problem ? "critical" : "ok"}>
                  {conditionWord(c.type, c.status)}
                </Badge>
                <span className={styles.muted}>
                  {c.problem && c.message ? c.message : ""} since{" "}
                  {formatAge(now - Date.parse(c.since))} ago
                </span>
              </li>
            ))}
          </ul>
        </Card>
        <Card title="Machine">
          <dl className={styles.info}>
            <div>
              <dt>OS</dt>
              <dd>{data.info.osImage}</dd>
            </div>
            <div>
              <dt>Kernel</dt>
              <dd>{data.info.kernelVersion}</dd>
            </div>
            <div>
              <dt>Runtime</dt>
              <dd>{data.info.containerRuntime}</dd>
            </div>
            <div>
              <dt>Architecture</dt>
              <dd>{data.info.architecture}</dd>
            </div>
          </dl>
        </Card>
      </div>

      {(data.taints.length > 0 || mode === "deep") && (
        <Card title="Taints">
          {data.taints.length === 0 ? (
            <p className={styles.muted}>No taints: any pod may be scheduled here.</p>
          ) : (
            <ul className={styles.chips}>
              {data.taints.map((t) => (
                <li key={t.key + t.effect} className={styles.chip}>
                  {t.key}
                  {t.value ? `=${t.value}` : ""}:{t.effect}
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}
      {mode === "deep" && labels.length > 0 && (
        <Card title="Labels">
          <ul className={styles.chips}>
            {labels.map(([k, v]) => (
              <li key={k} className={styles.chip}>
                {k}={v}
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  );
}
