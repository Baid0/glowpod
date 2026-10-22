import { IconHome, IconLayoutSidebarLeftExpand } from "@tabler/icons-react";
import { Link as RouterLink, useLocation } from "react-router";

import { cx } from "../lib/cx";
import { usePreferences } from "../lib/preferences";
import { useClusters } from "../lib/queries";
import { Logo } from "./Logo";
import { usePanel } from "./panel";
import { sectionFor, sectionHome, sectionsFor, settingsSection, type Section } from "./navigation";
import styles from "./Rail.module.css";
import { Link } from "../lib/clusterLinks";
import { pathInCluster } from "../lib/clusterPaths";

const ICON = { size: 18, stroke: 1.75 } as const;

/** home: the Clusters page, where only Home is there to show. */
export function Rail({ home = false }: { home?: boolean }) {
  const { mode } = usePreferences();
  const panel = usePanel();
  const { pathname } = useLocation();
  const active = sectionFor(pathInCluster(pathname), mode);
  const { data: clusters } = useClusters();
  const many = (clusters?.length ?? 0) > 1;

  if (home) {
    return (
      <nav className={styles.rail} aria-label="Sections">
        <RouterLink to="/" className={styles.logo} aria-label="Glowpod: all clusters">
          <Logo />
        </RouterLink>
        <ul className={styles.list}>
          <li>
            <RouterLink
              to="/"
              className={cx(styles.item, styles.active)}
              aria-label="All clusters"
              aria-current="page"
              title="All clusters"
            >
              <IconHome {...ICON} aria-hidden="true" />
            </RouterLink>
          </li>
        </ul>
      </nav>
    );
  }

  return (
    <nav className={styles.rail} aria-label="Sections">
      <Link to="/" className={styles.logo} aria-label="Glowpod overview">
        <Logo />
      </Link>

      {many && (
        <RouterLink to="/" className={styles.item} aria-label="All clusters" title="All clusters">
          <IconHome {...ICON} aria-hidden="true" />
        </RouterLink>
      )}

      {!panel.open && (
        <button
          type="button"
          className={cx(styles.item, styles.expand)}
          aria-label="Expand panel"
          title="Expand panel"
          onClick={() => {
            panel.setOpen(true);
          }}
        >
          <IconLayoutSidebarLeftExpand {...ICON} aria-hidden="true" />
        </button>
      )}

      <ul className={styles.list}>
        {sectionsFor(mode).map((s) => (
          <li key={s.id}>
            <RailLink section={s} active={active?.id === s.id} />
          </li>
        ))}
      </ul>

      <div className={styles.bottom}>
        <RailLink section={settingsSection} active={active?.id === settingsSection.id} />
      </div>
    </nav>
  );
}

function RailLink({ section, active }: { section: Section; active: boolean }) {
  const Icon = section.icon;
  return (
    <Link
      to={sectionHome(section)}
      className={cx(styles.item, active && styles.active)}
      aria-label={section.label}
      aria-current={active ? "page" : undefined}
      title={section.label}
    >
      <Icon {...ICON} aria-hidden="true" />
    </Link>
  );
}
