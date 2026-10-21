import { useId, type HTMLAttributes, type ReactNode } from "react";

import { cx } from "../../lib/cx";
import styles from "./Card.module.css";

export interface CardProps extends Omit<HTMLAttributes<HTMLElement>, "title"> {
  /** Card title (title-md). When set, the card is a labelled region. */
  title?: ReactNode;
  /** Right side of the header: a link, a badge, a live dot. */
  actions?: ReactNode;
  /** Heading level for the title; defaults to h2. */
  headingLevel?: 2 | 3;
  /** default = 24px padding, gauge = 20px (gauge cards). */
  padding?: "default" | "gauge";
}

export function Card({
  title,
  actions,
  headingLevel = 2,
  padding = "default",
  className,
  children,
  ...rest
}: CardProps) {
  const titleId = useId();
  const Heading = headingLevel === 2 ? "h2" : "h3";
  const hasHeader = title !== undefined || actions !== undefined;

  return (
    <section
      className={cx(styles.card, className)}
      data-padding={padding}
      aria-labelledby={title !== undefined ? titleId : undefined}
      {...rest}
    >
      {hasHeader && (
        <div className={styles.header}>
          {title !== undefined && (
            <Heading id={titleId} className={styles.title}>
              {title}
            </Heading>
          )}
          {actions !== undefined && <div className={styles.actions}>{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}
