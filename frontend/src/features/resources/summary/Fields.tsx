import type { ReactNode } from "react";

import styles from "./Summary.module.css";

/** A two-column list of facts; rows with no value are left out. */
export function Fields({ rows }: { rows: [string, ReactNode][] }) {
  const shown = rows.filter(([, v]) => v !== undefined && v !== null && v !== "");
  if (shown.length === 0) return null;
  return (
    <dl className={styles.fields}>
      {shown.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Chips({ items, empty }: { items: string[]; empty?: string }) {
  if (items.length === 0) return empty ? <p className={styles.muted}>{empty}</p> : null;
  return (
    <ul className={styles.chips}>
      {items.map((i) => (
        <li key={i} className={styles.chip}>
          {i}
        </li>
      ))}
    </ul>
  );
}
