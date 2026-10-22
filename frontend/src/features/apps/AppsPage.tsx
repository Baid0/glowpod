import { useMemo, useState } from "react";
import { Link } from "react-router";

import { IconFileText } from "@tabler/icons-react";
import { AppCard } from "./AppCard";
import { appPath } from "./appLinks";
import styles from "./Apps.module.css";
import { useApps } from "./useApps";

function useFiltered(search: string) {
  const { data, error, isPending } = useApps();
  const apps = useMemo(() => {
    const q = search.toLowerCase().trim();
    return (data ?? []).filter((a) => !q || `${a.name} ${a.namespace}`.toLowerCase().includes(q));
  }, [data, search]);
  return { all: data, apps, error, isPending };
}

function Search({
  value,
  onChange,
  label,
}: {
  value: string;
  onChange: (v: string) => void;
  label: string;
}) {
  return (
    <input
      type="search"
      className={styles.search}
      aria-label={label}
      placeholder="Search apps…"
      value={value}
      onChange={(e) => {
        onChange(e.target.value);
      }}
    />
  );
}

/** Glance's Apps: every app, troubled first. */
export function AppsPage() {
  const [search, setSearch] = useState("");
  const { all, apps, error, isPending } = useFiltered(search);
  return (
    <div className={styles.page}>
      <div className={styles.toolbar}>
        <Search value={search} onChange={setSearch} label="Search apps" />
        {all && (
          <span className={styles.count} aria-live="polite">
            {search ? `${apps.length} of ${all.length}` : all.length} apps
          </span>
        )}
      </div>
      {error && <p className={styles.note}>{error.message}</p>}
      {all?.length === 0 && (
        <p className={styles.note}>Nothing growing here yet. Deploy something?</p>
      )}
      {all && all.length > 0 && apps.length === 0 && (
        <p className={styles.note}>No app matches “{search}”.</p>
      )}
      <div className={styles.grid}>
        {isPending && [0, 1, 2, 3].map((i) => <div key={i} className={styles.skeleton} />)}
        {apps.map((a) => (
          <AppCard key={a.namespace + "/" + a.name} app={a} />
        ))}
      </div>
    </div>
  );
}

/** Glance's Logs: pick an app, read what it says. */
export function LogsPage() {
  const [search, setSearch] = useState("");
  const { all, apps, error } = useFiltered(search);
  return (
    <div className={styles.page}>
      <div className={styles.toolbar}>
        <Search value={search} onChange={setSearch} label="Search apps" />
        <span className={styles.count}>Pick an app to read its logs.</span>
      </div>
      {error && <p className={styles.note}>{error.message}</p>}
      {all?.length === 0 && <p className={styles.note}>No apps yet, so no logs.</p>}
      <ul className={styles.logList}>
        {apps.map((a) => (
          <li key={a.namespace + "/" + a.name}>
            <Link className={styles.logItem} to={appPath(a, "logs")}>
              <IconFileText size={18} stroke={1.75} aria-hidden="true" />
              <span className={styles.logName}>{a.name}</span>
              <span className={styles.logNs}>{a.namespace}</span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
