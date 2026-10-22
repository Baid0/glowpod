import { IconChevronLeft } from "@tabler/icons-react";
import { NavLink, useLocation } from "react-router";

import { cx } from "../lib/cx";
import { usePreferences } from "../lib/preferences";
import { useEvents, useOverview } from "../lib/queries";
import { useNow } from "../lib/time";
import type { Overview } from "../lib/types";
import { ClusterHealthCard } from "./ClusterHealthCard";
import { ClusterPicker } from "./ClusterPicker";
import { countFor } from "./counts";
import { usePanel } from "./panel";
import { sectionFor, sectionsFor } from "./navigation";
import styles from "./SectionPanel.module.css";

export function SectionPanel() {
  const { mode } = usePreferences();
  const panel = usePanel();
  const { pathname } = useLocation();
  const section = sectionFor(pathname, mode) ?? sectionsFor(mode)[0];
  const overview = useOverview().data;
  const events = useEvents().data;
  const now = useNow(60_000);
  const title = section?.label ?? "";

  return (
    <aside className={styles.panel} aria-label={`${title} panel`}>
      <div className={styles.head}>
        <h2 className={styles.title}>{title}</h2>
        <button
          type="button"
          className={styles.collapse}
          aria-label="Collapse panel"
          title="Collapse panel"
          onClick={() => {
            panel.setOpen(false);
          }}
        >
          <IconChevronLeft size={16} stroke={1.75} aria-hidden="true" />
        </button>
      </div>

      <ClusterPicker />

      <div className={styles.scroll}>
        {mode === "deep" && section && section.id !== "settings" && (
          <nav aria-label={title}>
            <ul className={styles.list}>
              {section.items.map((item) => {
                const count = item.count && countFor(item.count, overview, events, now);
                return (
                  <li key={item.path}>
                    <NavLink
                      to={item.path}
                      end={item.path === "/"}
                      className={({ isActive }) => cx(styles.item, isActive && styles.active)}
                      onClick={() => {
                        if (panel.overlay) panel.setOpen(false);
                      }}
                    >
                      <span className={styles.label}>{item.label}</span>
                      {count && (
                        <span className={styles.count} aria-label={count.label} title={count.label}>
                          {count.problem && (
                            <span className={styles.problemDot} aria-hidden="true" />
                          )}
                          <span aria-hidden="true">{count.text}</span>
                        </span>
                      )}
                    </NavLink>
                  </li>
                );
              })}
            </ul>
          </nav>
        )}

        {mode === "deep" ? <Pinned /> : <Apps apps={overview?.apps} />}
      </div>

      <div className={styles.footer}>
        <ClusterHealthCard />
      </div>
    </aside>
  );
}

function Pinned() {
  return (
    <section className={styles.group} aria-labelledby="pinned-title">
      <h3 id="pinned-title" className={styles.groupTitle}>
        Pinned
      </h3>
      <p className={styles.empty}>Pin pods and apps from their pages to keep them here.</p>
    </section>
  );
}

function Apps({ apps }: { apps: Overview["apps"] | undefined }) {
  return (
    <section className={styles.group} aria-labelledby="apps-title">
      <h3 id="apps-title" className={styles.groupTitle}>
        Your apps
      </h3>
      {apps?.length === 0 && <p className={styles.empty}>Nothing growing here yet.</p>}
      <ul className={styles.list}>
        {apps?.map((a) => (
          <li key={a.namespace + "/" + a.name}>
            <NavLink
              to={`/apps/${encodeURIComponent(a.namespace)}/${encodeURIComponent(a.name)}`}
              className={({ isActive }) => cx(styles.item, styles.app, isActive && styles.active)}
            >
              <span className={styles.statusDot} data-status={a.status} aria-hidden="true" />
              <span className={styles.label}>{a.name}</span>
              <span className="visually-hidden">
                {a.status === "ok"
                  ? ", healthy"
                  : a.status === "high"
                    ? ", rolling out"
                    : ", failing"}
              </span>
            </NavLink>
          </li>
        ))}
      </ul>
    </section>
  );
}
