import { IconCheck, IconChevronDown } from "@tabler/icons-react";
import { useEffect, useId, useRef, useState } from "react";

import { useLive } from "../lib/live";
import { useSession } from "../lib/queries";
import styles from "./ClusterPicker.module.css";

/** Shows the connected cluster. v1 has exactly one; the menu says so. */
export function ClusterPicker() {
  const { data } = useSession();
  const { status } = useLive();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const clusters = data?.clusters ?? [];
  const current = clusters[0];

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent | KeyboardEvent) => {
      if (
        e instanceof KeyboardEvent ? e.key === "Escape" : !root.current?.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", close);
    };
  }, [open]);

  return (
    <div className={styles.root} ref={root}>
      <button
        type="button"
        className={styles.button}
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={`Cluster: ${current?.name ?? "connecting"}${status === "live" ? ", live" : ""}`}
        title={current?.name}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <span className={styles.dot} data-live={status === "live"} aria-hidden="true" />
        <span className={styles.name}>{current?.name ?? "…"}</span>
        <IconChevronDown size={16} stroke={1.75} aria-hidden="true" />
      </button>
      {open && (
        <div id={menuId} className={styles.menu}>
          <ul className={styles.list}>
            {clusters.map((c) => (
              <li key={c.id} className={styles.option}>
                <span className={styles.name}>{c.name}</span>
                <IconCheck size={16} stroke={1.75} aria-label="Current cluster" />
              </li>
            ))}
          </ul>
          {current && <p className={styles.note}>{current.server}</p>}
          <p className={styles.note}>This version connects to one cluster at a time.</p>
        </div>
      )}
    </div>
  );
}
