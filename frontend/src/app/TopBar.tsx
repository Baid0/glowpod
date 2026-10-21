import { IconChevronDown, IconSearch } from "@tabler/icons-react";

import { initials } from "../lib/format";
import { useLive } from "../lib/live";
import { usePreferences, type Mode } from "../lib/preferences";
import { useNamespaces, useOverview, useSession } from "../lib/queries";
import { formatAge, useNow } from "../lib/time";
import styles from "./TopBar.module.css";

/** How old data may get before the live dot goes grey. */
const STALE_MS = 60_000;

export function TopBar({ title, onSearch }: { title: string; onSearch: () => void }) {
  return (
    <header className={styles.bar}>
      <div className={styles.left}>
        <h1 className={styles.title}>{title}</h1>
        <Updated />
      </div>
      <div className={styles.right}>
        <NamespacePicker />
        <span className={styles.divider} aria-hidden="true" />
        <ModeTabs />
        <span className={styles.divider} aria-hidden="true" />
        <button
          type="button"
          className={styles.search}
          aria-label="Search and commands"
          aria-keyshortcuts="Meta+K Control+K"
          onClick={onSearch}
        >
          <IconSearch size={18} stroke={1.75} aria-hidden="true" />
          <kbd className={styles.kbd} aria-hidden="true">
            ⌘K
          </kbd>
        </button>
        <Avatar />
      </div>
    </header>
  );
}

function Updated() {
  const { status, lastMessageAt } = useLive();
  const { dataUpdatedAt, isError, error } = useOverview();
  const now = useNow();
  const last = Math.max(dataUpdatedAt, lastMessageAt ?? 0);

  if (isError && last === 0) {
    return (
      <span className={styles.updated}>
        <span className={styles.dot} data-tone="critical" aria-hidden="true" />
        {error.message}
      </span>
    );
  }
  if (last === 0) return <span className={styles.updated}>Loading…</span>;

  const age = now - last;
  const fresh = status === "live" && age < STALE_MS && !isError;
  return (
    <span className={styles.updated} title={new Date(last).toLocaleString()}>
      <span className={styles.dot} data-tone={fresh ? "live" : "stale"} aria-hidden="true" />
      {status === "reconnecting" ? "Reconnecting… " : ""}updated {formatAge(age)} ago
    </span>
  );
}

function NamespacePicker() {
  const { namespace, setNamespace } = usePreferences();
  const { data } = useNamespaces();
  const names = data ?? [];
  // Keep a saved namespace selectable even if it's gone or not loaded yet.
  const options = namespace && !names.includes(namespace) ? [namespace, ...names] : names;

  return (
    <label className={styles.namespace}>
      <span className="visually-hidden">Namespace</span>
      <select
        className={styles.select}
        value={namespace}
        onChange={(e) => {
          setNamespace(e.target.value);
        }}
      >
        <option value="">All namespaces</option>
        {options.map((ns) => (
          <option key={ns} value={ns}>
            {ns}
          </option>
        ))}
      </select>
      <IconChevronDown className={styles.chevron} size={16} stroke={1.75} aria-hidden="true" />
    </label>
  );
}

const modes: { id: Mode; label: string }[] = [
  { id: "glance", label: "Glance" },
  { id: "deep", label: "Deep" },
];

function ModeTabs() {
  const { mode, setMode } = usePreferences();
  return (
    <div className={styles.modes} role="group" aria-label="View mode">
      {modes.map((m) => (
        <button
          key={m.id}
          type="button"
          className={styles.mode}
          aria-pressed={mode === m.id}
          onClick={() => {
            setMode(m.id);
          }}
        >
          {m.label}
        </button>
      ))}
    </div>
  );
}

function Avatar() {
  const { data } = useSession();
  const name = data?.user.username ?? "";
  return (
    <span
      className={styles.avatar}
      role="img"
      aria-label={name ? `Signed in as ${name}` : "Signed in"}
      title={name}
    >
      {initials(name)}
    </span>
  );
}
