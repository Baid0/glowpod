import { useRef, type KeyboardEvent } from "react";

import styles from "./Tabs.module.css";

export interface Tab<T extends string> {
  id: T;
  label: string;
  /** A small count after the label, e.g. events. */
  count?: number;
}

/**
 * Text tabs like the Glance / Deep switch: active = ink + 2px underline.
 * Arrow keys move between tabs. Panels use id `panel-{id}` and are
 * labelled by `tab-{id}`.
 */
export function Tabs<T extends string>({
  tabs,
  selected,
  onSelect,
  label,
}: {
  tabs: readonly Tab<T>[];
  selected: T;
  onSelect: (id: T) => void;
  label: string;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: KeyboardEvent, i: number) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const next = (i + (e.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
    const t = tabs[next];
    if (!t) return;
    onSelect(t.id);
    refs.current[next]?.focus();
  };
  return (
    <div className={styles.tabs} role="tablist" aria-label={label}>
      {tabs.map((t, i) => (
        <button
          key={t.id}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="button"
          role="tab"
          id={`tab-${t.id}`}
          aria-selected={selected === t.id}
          aria-controls={`panel-${t.id}`}
          tabIndex={selected === t.id ? 0 : -1}
          className={styles.tab}
          onClick={() => {
            onSelect(t.id);
          }}
          onKeyDown={(e) => {
            onKey(e, i);
          }}
        >
          {t.label}
          {t.count !== undefined && <span className={styles.count}>{t.count}</span>}
        </button>
      ))}
    </div>
  );
}
