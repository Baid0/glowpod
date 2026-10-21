import type { ReactNode } from "react";

import { cx } from "../../lib/cx";
import styles from "./Badge.module.css";

/** ok = healthy, high = 70–84% / pending / waiting, critical = ≥85% / failing. */
export type StatusTone = "ok" | "high" | "critical" | "neutral";

export interface BadgeProps {
  tone: StatusTone;
  /** The status word. Always shown: color is never the only signal. */
  children: ReactNode;
  /** Optional icon, used in lists. */
  icon?: ReactNode;
  className?: string;
}

/** Neutral pill with a colored dot and a word. */
export function Badge({ tone, children, icon, className }: BadgeProps) {
  return (
    <span className={cx(styles.badge, className)} data-tone={tone}>
      <span className={styles.dot} aria-hidden="true" />
      {icon && (
        <span className={styles.icon} aria-hidden="true">
          {icon}
        </span>
      )}
      <span>{children}</span>
    </span>
  );
}
