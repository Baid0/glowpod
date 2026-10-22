import { IconArrowRight } from "@tabler/icons-react";

import styles from "./DemoBanner.module.css";

/** Says plainly that this is sample data, and where to get the real thing. */
export function DemoBanner() {
  return (
    <aside className={styles.banner} aria-label="About this demo">
      <span className={styles.pill}>
        <span className={styles.dot} aria-hidden="true" />
        Demo
      </span>
      <p className={styles.text}>
        <span className={styles.long}>
          Sample data in your browser, no cluster involved. Changes stay in this tab until you
          reload.
        </span>
        <span className={styles.short}>Sample data only.</span>
      </p>
      <a className={styles.home} href="/">
        glowpod.dev
      </a>
      <a className={styles.install} href="/#install">
        Install
        <IconArrowRight size={16} stroke={1.75} aria-hidden="true" />
      </a>
    </aside>
  );
}
