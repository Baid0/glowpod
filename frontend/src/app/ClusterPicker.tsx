import { IconCheck, IconChevronDown, IconHome } from "@tabler/icons-react";
import { useEffect, useId, useRef, useState } from "react";
import { Link as RouterLink, useLocation, useNavigate } from "react-router";

import { pathInCluster } from "../lib/clusterPaths";
import { useLive } from "../lib/live";
import { useClusterId, useClusters, useSession } from "../lib/queries";
import { samePage } from "./navigation";
import styles from "./ClusterPicker.module.css";

/**
 * The cluster you're on, and every other one. Switching keeps you on the
 * same kind of page: Pods stays Pods; a pod's own page opens the Pods list.
 */
export function ClusterPicker() {
  const { data: clusters = [] } = useClusters();
  const { data: session } = useSession();
  const id = useClusterId();
  const { status } = useLive();
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const current = clusters.find((c) => c.id === id);

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

  const page = samePage(pathInCluster(pathname));

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
          <ul className={styles.list} aria-label="Clusters">
            {clusters.map((c) => {
              const here = c.id === id;
              return (
                <li key={c.id}>
                  <button
                    type="button"
                    className={styles.option}
                    aria-current={here ? "true" : undefined}
                    title={c.server}
                    onClick={() => {
                      setOpen(false);
                      if (!here) void navigate(`/c/${encodeURIComponent(c.id)}${page}`);
                    }}
                  >
                    <span className={styles.name}>{c.name}</span>
                    {here && <IconCheck size={16} stroke={1.75} aria-label="Current cluster" />}
                  </button>
                </li>
              );
            })}
          </ul>
          {current && <p className={styles.note}>{current.server}</p>}
          {clusters.length > 1 ? (
            <RouterLink
              to="/"
              className={styles.all}
              onClick={() => {
                setOpen(false);
              }}
            >
              <IconHome size={16} stroke={1.75} aria-hidden="true" />
              All clusters
            </RouterLink>
          ) : (
            session?.mode === "local" && (
              <p className={styles.note}>Add contexts to your kubeconfig to see more clusters.</p>
            )
          )}
        </div>
      )}
    </div>
  );
}
