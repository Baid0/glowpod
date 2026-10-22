import { IconBulb } from "@tabler/icons-react";
import { Link, useParams, useSearchParams } from "react-router";

import { Badge } from "../../components/Badge";
import { Button, ButtonLink } from "../../components/Button";
import { Card } from "../../components/Card";
import { ApiError } from "../../lib/api";
import { imageTag } from "../../lib/format";
import { formatAge, useNow } from "../../lib/time";
import type { AppDetail, PodSummary } from "../../lib/types";
import { LogViewer } from "../logs/LogViewer";
import { usePod } from "../pod/usePod";
import { statusTone } from "../resources/cells";
import { APP_BADGES } from "./appLinks";
import styles from "./AppPage.module.css";
import { useApp } from "./useApps";

const TABS = [
  { id: "overview", label: "Overview" },
  { id: "logs", label: "Logs" },
] as const;

/** One app: what's wrong in plain words, its workloads and pods, and its logs. */
export function AppPage() {
  const { namespace = "", name = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "logs" ? "logs" : "overview";
  const { data, error, isPending, refetch } = useApp(namespace, name);

  if (error) {
    const gone = error instanceof ApiError && error.status === 404;
    return (
      <div className={styles.gone} role="alert">
        <h2 className={styles.goneTitle}>
          {gone ? "This app isn't here" : "Couldn't load this app"}
        </h2>
        <p>
          {gone
            ? `Nothing in ${namespace} belongs to an app called ${name} right now.`
            : error.message}
        </p>
        <div className={styles.row}>
          <Link to="/apps">See all apps</Link>
          {!gone && (
            <Button size="sm" onClick={() => void refetch()}>
              Retry
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className={styles.page}>
      <Header data={data} name={name} namespace={namespace} />
      <div className={styles.tabs} role="tablist" aria-label="App views">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`app-tab-${t.id}`}
            aria-selected={tab === t.id}
            aria-controls={`app-panel-${t.id}`}
            className={styles.tab}
            onClick={() => {
              setParams(t.id === "overview" ? {} : { tab: t.id }, { replace: true });
            }}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`app-panel-${tab}`} aria-labelledby={`app-tab-${tab}`}>
        {isPending ? (
          <div className={styles.skeleton} aria-label="Loading the app" />
        ) : tab === "logs" ? (
          <AppLogs data={data} />
        ) : (
          <Overview data={data} />
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
  data: AppDetail | undefined;
  name: string;
  namespace: string;
}) {
  const now = useNow(60_000);
  const app = data?.app;
  return (
    <header className={styles.header}>
      <div className={styles.row}>
        <h2 className={styles.name}>{name}</h2>
        {app && <Badge tone={APP_BADGES[app.status][0]}>{APP_BADGES[app.status][1]}</Badge>}
      </div>
      <dl className={styles.facts}>
        <div className={styles.fact}>
          <dt>Namespace</dt>
          <dd>{namespace}</dd>
        </div>
        {app && (
          <div className={styles.fact}>
            <dt>Ready</dt>
            <dd>
              {app.ready} / {app.total}
            </dd>
          </div>
        )}
        {app?.lastDeploy && (
          <div className={styles.fact}>
            <dt>Last deploy</dt>
            <dd>{formatAge(now - Date.parse(app.lastDeploy))} ago</dd>
          </div>
        )}
      </dl>
    </header>
  );
}

function podPath(p: { namespace: string; name: string }, query = "") {
  return `/workloads/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}${query}`;
}

function Overview({ data }: { data: AppDetail }) {
  const now = useNow(30_000);
  return (
    <div className={styles.grid}>
      <div className={styles.main}>
        <Card title="Needs attention">
          {data.problems.length === 0 ? (
            <p className={styles.good}>
              All {data.pods.length} {data.pods.length === 1 ? "pod looks" : "pods look"} healthy.
            </p>
          ) : (
            <ul className={styles.problems}>
              {data.problems.map((p) => (
                <li key={p.name + p.reason} className={styles.problem} data-tone={p.severity}>
                  <p className={styles.problemTitle}>
                    {p.title} <span className={styles.term}>({p.reason})</span>
                  </p>
                  <p className={styles.muted}>
                    <span className={styles.mono}>{p.name}</span> · {p.detail}
                  </p>
                  <p className={styles.hint}>
                    <IconBulb size={16} stroke={1.75} aria-hidden="true" />
                    <span>{p.hint}</span>
                  </p>
                  <div className={styles.row}>
                    <ButtonLink
                      to={podPath(p as { namespace: string; name: string })}
                      size="sm"
                      aria-label={`View ${p.name}`}
                    >
                      View
                    </ButtonLink>
                    {(p.reason === "CrashLoopBackOff" || p.reason === "OOMKilled") && (
                      <Link
                        to={podPath(
                          p as { namespace: string; name: string },
                          "?tab=logs&previous=1",
                        )}
                      >
                        Open the last crash&apos;s log
                      </Link>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card title="Pods" actions={String(data.pods.length)}>
          <PodTable pods={data.pods} now={now} />
        </Card>
      </div>
      <div className={styles.side}>
        <Card title="Workloads" actions={String(data.workloads.length)}>
          <ul className={styles.list}>
            {data.workloads.map((w) => (
              <li key={w.kind + w.name} className={styles.workload}>
                <div className={styles.row}>
                  <span className={styles.kind}>{w.kind}</span>
                  <Badge tone={APP_BADGES[w.status][0]}>
                    {w.ready} / {w.total} ready
                  </Badge>
                </div>
                <Link
                  className={styles.mono}
                  to={`/workloads/${w.resource}/${encodeURIComponent(data.app.namespace)}/${encodeURIComponent(w.name)}`}
                >
                  {w.name}
                </Link>
                <p className={styles.muted}>{w.images.map(imageTag).join(", ")}</p>
              </li>
            ))}
          </ul>
        </Card>
        <Card title="Recent deploys">
          {data.deploys.length === 0 ? (
            <p className={styles.muted}>No deploys recorded.</p>
          ) : (
            <ul className={styles.list}>
              {data.deploys.map((d) => (
                <li key={d.name + d.revision} className={styles.deploy}>
                  <span>
                    <span className={styles.mono}>{d.name}</span> revision {d.revision}
                  </span>
                  <span className={styles.muted}>
                    {imageTag(d.image)} · {formatAge(now - Date.parse(d.at))} ago
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </div>
  );
}

function PodTable({ pods, now }: { pods: PodSummary[]; now: number }) {
  if (pods.length === 0) return <p className={styles.muted}>No pods running.</p>;
  return (
    <table className={styles.table}>
      <caption className="visually-hidden">Pods of this app</caption>
      <thead>
        <tr>
          <th scope="col">Pod</th>
          <th scope="col">Status</th>
          <th scope="col">Ready</th>
          <th scope="col" className={styles.num}>
            Restarts
          </th>
          <th scope="col">Node</th>
          <th scope="col">Age</th>
        </tr>
      </thead>
      <tbody>
        {pods.map((p) => {
          const word = p.problem?.reason ?? p.phase;
          return (
            <tr key={p.name}>
              <td>
                <Link className={styles.mono} to={podPath(p)}>
                  {p.name}
                </Link>
              </td>
              <td>
                <Badge tone={p.problem ? p.problem.severity : statusTone(p.phase)}>{word}</Badge>
              </td>
              <td>{p.ready}</td>
              <td className={styles.num}>{p.restarts}</td>
              <td className={styles.muted}>{p.node ?? "—"}</td>
              <td className={styles.muted}>{formatAge(now - Date.parse(p.createdAt))}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/** Logs for one of the app's pods; opens on the struggling one. */
function AppLogs({ data }: { data: AppDetail }) {
  const [params, setParams] = useSearchParams();
  const struggling = data.pods.find((p) => p.problem) ?? data.pods.find((p) => p.restarts > 0);
  const podName = params.get("pod") ?? struggling?.name ?? data.pods[0]?.name;
  if (!podName) return <p className={styles.muted}>This app has no pods, so there are no logs.</p>;
  return (
    <div className={styles.logs}>
      {data.pods.length > 1 && (
        <label className={styles.podPicker}>
          <span className={styles.muted}>Pod</span>
          <select
            className={styles.select}
            value={podName}
            onChange={(e) => {
              setParams({ tab: "logs", pod: e.target.value }, { replace: true });
            }}
          >
            {data.pods.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
                {p.problem ? ` (${p.problem.reason})` : ""}
              </option>
            ))}
          </select>
        </label>
      )}
      <PodLogs key={podName} namespace={data.app.namespace} pod={podName} />
    </div>
  );
}

function PodLogs({ namespace, pod }: { namespace: string; pod: string }) {
  const { data, error } = usePod(namespace, pod);
  if (error) return <p className={styles.muted}>{error.message}</p>;
  if (!data) return <div className={styles.skeleton} aria-label="Loading the pod" />;
  return <LogViewer namespace={namespace} pod={pod} containers={data.containers} />;
}
