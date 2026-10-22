import { IconInfoCircle } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";

import { Badge } from "../../components/Badge";
import { Card } from "../../components/Card";
import { api } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import { formatAge, useNow } from "../../lib/time";
import { ObjectError } from "../yaml/ObjectError";
import styles from "./Helm.module.css";
import { helmTone, helmWord, objectPath } from "./status";

const releasePath = (ns: string, name: string) =>
  `/extensions/helm/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`;

/** Every Helm release the user can see, newest revision of each. */
export function HelmReleasesPage() {
  const cluster = useClusterId();
  const { namespace } = usePreferences();
  const now = useNow(30_000);
  const { data, error, isPending, refetch } = useQuery({
    queryKey: queryKeys.helmReleases(cluster, namespace),
    queryFn: ({ signal }) => api.helmReleases(cluster, namespace, signal),
    staleTime: 30_000,
  });

  return (
    <Card title={namespace ? `Helm releases in ${namespace}` : "Helm releases in all namespaces"}>
      {isPending ? (
        <div className={styles.skeleton} aria-label="Loading releases" />
      ) : error ? (
        <ObjectError error={error} onRetry={() => void refetch()} />
      ) : data.length === 0 ? (
        <p className={styles.muted}>
          No Helm releases here. Charts installed with <code>helm install</code> show up on their
          own.
        </p>
      ) : (
        <table className={styles.table}>
          <caption className="visually-hidden">Helm releases</caption>
          <thead>
            <tr>
              <th scope="col">Release</th>
              {!namespace && <th scope="col">Namespace</th>}
              <th scope="col">Status</th>
              <th scope="col">Chart</th>
              <th scope="col">App version</th>
              <th scope="col" className={styles.num}>
                Revision
              </th>
              <th scope="col">Updated</th>
            </tr>
          </thead>
          <tbody>
            {data.map((r) => (
              <tr key={r.namespace + "/" + r.name}>
                <td>
                  <Link className={styles.mono} to={releasePath(r.namespace, r.name)}>
                    {r.name}
                  </Link>
                </td>
                {!namespace && <td className={styles.muted}>{r.namespace}</td>}
                <td>
                  <Badge tone={helmTone(r.status)}>{helmWord(r.status)}</Badge>
                </td>
                <td className={styles.mono}>
                  {r.chart}-{r.chartVersion}
                </td>
                <td>{r.appVersion ?? "—"}</td>
                <td className={styles.num}>{r.revision}</td>
                <td className={styles.muted}>{formatAge(now - Date.parse(r.updated))} ago</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

/** One release: what it is, its history and what it installed. Never its values. */
export function HelmReleasePage() {
  const { namespace = "", name = "" } = useParams();
  const cluster = useClusterId();
  const now = useNow(30_000);
  const { data, error, isPending, refetch } = useQuery({
    queryKey: queryKeys.helmRelease(cluster, namespace, name),
    queryFn: ({ signal }) => api.helmRelease(cluster, namespace, name, signal),
    staleTime: 30_000,
  });

  if (error) return <ObjectError error={error} onRetry={() => void refetch()} />;
  if (isPending) return <div className={styles.skeleton} aria-label="Loading the release" />;

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <p className={styles.kind}>Helm release</p>
        <div className={styles.titleRow}>
          <h2 className={styles.name}>{data.name}</h2>
          <Badge tone={helmTone(data.status)}>{helmWord(data.status)}</Badge>
        </div>
        <dl className={styles.facts}>
          <div>
            <dt>Namespace</dt>
            <dd>{data.namespace}</dd>
          </div>
          <div>
            <dt>Chart</dt>
            <dd>
              {data.chart}-{data.chartVersion}
            </dd>
          </div>
          {data.appVersion && (
            <div>
              <dt>App version</dt>
              <dd>{data.appVersion}</dd>
            </div>
          )}
          <div>
            <dt>Revision</dt>
            <dd>{data.revision}</dd>
          </div>
          <div>
            <dt>Updated</dt>
            <dd>{formatAge(now - Date.parse(data.updated))} ago</dd>
          </div>
        </dl>
        {data.description && <p className={styles.description}>{data.description}</p>}
      </header>

      <p className={styles.note}>
        <IconInfoCircle size={16} stroke={1.75} aria-hidden="true" />
        Values aren&apos;t shown: they often hold passwords. Use <code>helm get values</code> if you
        need them.
      </p>

      <div className={styles.columns}>
        <Card title={`Installed ${String(data.objects.length)} objects`}>
          <ul className={styles.objects}>
            {data.objects.map((o) => (
              <li key={`${o.kind}/${o.namespace ?? ""}/${o.name}`}>
                <span className={styles.muted}>{o.kind}</span>
                <Link className={styles.mono} to={objectPath(o)}>
                  {o.name}
                </Link>
              </li>
            ))}
          </ul>
        </Card>
        <Card title="History">
          <ol className={styles.history}>
            {data.history.map((h) => (
              <li key={h.revision}>
                <span className={styles.revision}>{h.revision}</span>
                <Badge tone={helmTone(h.status)}>{helmWord(h.status)}</Badge>
                <span className={styles.mono}>{h.chartVersion}</span>
                <span className={styles.muted}>
                  {h.description} · {formatAge(now - Date.parse(h.updated))} ago
                </span>
              </li>
            ))}
          </ol>
        </Card>
      </div>
    </div>
  );
}
