import { Link } from "react-router";

import styles from "./NotFound.module.css";

/** The page for addresses Glowpod doesn't know. */
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
