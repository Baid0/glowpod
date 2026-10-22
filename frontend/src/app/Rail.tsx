import { IconLayoutSidebarLeftExpand } from "@tabler/icons-react";
import { Link, useLocation } from "react-router";

import { cx } from "../lib/cx";
import { usePreferences } from "../lib/preferences";
import { Logo } from "./Logo";
import { usePanel } from "./panel";
import { sectionFor, sectionHome, sectionsFor, settingsSection, type Section } from "./navigation";
import styles from "./Rail.module.css";

const ICON = { size: 18, stroke: 1.75 } as const;

export function Rail() {
  const { mode } = usePreferences();
  const panel = usePanel();
  const { pathname } = useLocation();
  const active = sectionFor(pathname, mode);

  return (
    <nav className={styles.rail} aria-label="Sections">
      <Link to="/" className={styles.logo} aria-label="Glowpod overview">
        <Logo />
      </Link>

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
