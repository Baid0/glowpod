import styles from "./Logo.module.css";

/** The pod-shaped mark: a lime bean with a sprout. Shape only, never the mascot. */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg
      className={styles.logo}
      width={size}
      height={size}
      viewBox="0 0 32 32"
      aria-hidden="true"
      focusable="false"
    >
      <path className={styles.sprout} d="M16 10.5V6.5" />
      <path className={styles.leaf} d="M16 7c1-2.6 3.6-3.8 6.4-3.3-.6 2.6-3.1 4.2-6.4 3.3z" />
      <path
        className={styles.body}
        d="M16 10c5.8 0 9.5 4.4 9.5 10S21.6 29.5 16 29.5 6.5 25.6 6.5 20 10.2 10 16 10z"
      />
    </svg>
  );
}
