import { IconPlant2 } from "@tabler/icons-react";
import { Link } from "react-router";

import styles from "./ComingSoon.module.css";

/** Stand-in for pages a later phase builds. Nothing is hidden, just not built yet. */
export function ComingSoon({ title, phase }: { title: string; phase: number }) {
  return (
    <div className={styles.box}>
      <IconPlant2 size={32} stroke={1.75} aria-hidden="true" className={styles.icon} />
      <h2 className={styles.title}>{title} is still growing</h2>
      <p className={styles.text}>This page arrives in phase {phase} of the build.</p>
      <Link to="/" className={styles.link}>
        Back to the overview
      </Link>
    </div>
  );
}

export function NotFound() {
  return (
    <div className={styles.box}>
      <h2 className={styles.title}>Nothing at this address</h2>
      <p className={styles.text}>The page may have moved. Press ⌘K to search.</p>
      <Link to="/" className={styles.link}>
        Back to the overview
      </Link>
    </div>
  );
}
