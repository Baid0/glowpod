import { useRef, type KeyboardEvent } from "react";
import { Link, useParams, useSearchParams } from "react-router";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { ApiError } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { formatAge, useNow } from "../../lib/time";
import type { PodDetail } from "../../lib/types";
import { statusTone } from "../resources/cells";
import { Containers, Facts } from "./Containers";
import { PodEvents } from "./PodEvents";
import styles from "./PodPage.module.css";
import { Story } from "./Story";
import { usePod } from "./usePod";

const TABS = [
  { id: "overview", label: "Overview" },
  { id: "logs", label: "Logs" },
  { id: "yaml", label: "YAML" },
  { id: "events", label: "Events" },
] as const;
type TabId = (typeof TABS)[number]["id"];

/** Why is this pod unhappy? The story first, then the detail. */
export function PodPage() {
  const { namespace = "", name = "" } = useParams();
  const { data, error, isPending, refetch } = usePod(namespace, name);
  const [params, setParams] = useSearchParams();
  const tab: TabId = TABS.some((t) => t.id === params.get("tab"))
    ? (params.get("tab") as TabId)
    : "overview";
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);

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

  if (error) return <PodError error={error} namespace={namespace} onRetry={() => void refetch()} />;

  return (
    <div className={styles.page}>
      <Header data={data} name={name} namespace={namespace} />

      <div className={styles.tabs} role="tablist" aria-label="Pod views">
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
            {t.id === "events" && data ? (
              <span className={styles.tabCount}>{data.events.length}</span>
            ) : null}
          </button>
        ))}
      </div>

      <div
        role="tabpanel"
        id={`panel-${tab}`}
        aria-labelledby={`tab-${tab}`}
        className={styles.panel}
      >
        {isPending ? (
          <div className={styles.skeleton} aria-label="Loading the pod" />
        ) : tab === "overview" ? (
          <Overview data={data} />
        ) : tab === "events" ? (
          <PodEvents events={data.events} />
        ) : (
          <p className={styles.soon}>
            {tab === "logs" ? "Live logs" : "The YAML view"} arrives in the next build step.
          </p>
        )}
      </div>
    </div>
  );
}

function Header({
  data,
  name,
  namespace,
}: {
  data: PodDetail | undefined;
  name: string;
  namespace: string;
}) {
  const now = useNow(30_000);
  const pod = data?.pod;
  const problem = pod?.problem;
  const owner = data?.owner;
  return (
    <header className={styles.header}>
      <div className={styles.titleRow}>
        <h2 className={styles.name}>{name}</h2>
        {pod &&
          (problem ? (
            <Badge tone={problem.severity}>{problem.reason}</Badge>
          ) : (
            <Badge tone={statusTone(pod.phase)}>{pod.phase}</Badge>
          ))}
      </div>
      <dl className={styles.facts}>
        <Fact label="Namespace">{namespace}</Fact>
        {pod && <Fact label="Ready">{pod.ready}</Fact>}
        {pod && <Fact label="Restarts">{pod.restarts}</Fact>}
        {pod?.node && (
          <Fact label="Node">
            <Link to={`/nodes/${encodeURIComponent(pod.node)}`}>{pod.node}</Link>
          </Fact>
        )}
        {data && <Fact label="Age">{formatAge(now - Date.parse(data.createdAt))}</Fact>}
        {data?.ip && <Fact label="IP">{data.ip}</Fact>}
        {owner && (
          <Fact label="Owned by">
            <Link
              to={`/resources/${encodeURIComponent(owner.group || "core")}/${encodeURIComponent(owner.version)}/${encodeURIComponent(owner.resource)}/${encodeURIComponent(namespace)}/${encodeURIComponent(owner.name)}`}
            >
              {owner.kind} {owner.name}
            </Link>
          </Fact>
        )}
      </dl>
    </header>
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

function Overview({ data }: { data: PodDetail }) {
  const { mode } = usePreferences();
  return (
    <div className={mode === "deep" ? styles.overviewDeep : styles.overviewGlance}>
      <Story data={data} />
      <div className={styles.side}>
        <Containers data={data} />
        {mode === "deep" && <Facts data={data} />}
      </div>
    </div>
  );
}

function PodError({
  error,
  namespace,
  onRetry,
}: {
  error: Error;
  namespace: string;
  onRetry: () => void;
}) {
  const gone = error instanceof ApiError && error.status === 404;
  return (
    <div className={styles.gone} role="alert">
      <h2 className={styles.goneTitle}>{gone ? "This pod is gone" : "Couldn't load this pod"}</h2>
      <p>
        {gone
          ? "It may have been replaced by a new one, which is normal after a rollout or restart."
          : error.message}
      </p>
      <div className={styles.goneActions}>
        <Link to="/workloads/pods">See pods{namespace ? ` in ${namespace}` : ""}</Link>
        {!gone && (
          <Button size="sm" onClick={onRetry}>
            Retry
          </Button>
        )}
      </div>
    </div>
  );
}
