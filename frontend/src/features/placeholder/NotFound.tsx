import { Link as RouterLink } from "react-router";

import styles from "./NotFound.module.css";
import { Link } from "../../lib/clusterLinks";

/** The page for addresses Glowpod doesn't know, or a cluster it isn't connected to. */
export function NotFound({ what = "page" }: { what?: "page" | "cluster" }) {
  if (what === "cluster") {
    return (
      <div className={styles.box}>
        <h2 className={styles.title}>Glowpod isn't connected to that cluster</h2>
        <p className={styles.text}>It may have been removed, or renamed in your kubeconfig.</p>
        <RouterLink to="/" className={styles.link}>
          See all clusters
        </RouterLink>
      </div>
    );
  }
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
