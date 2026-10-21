import type { ReactNode } from "react";
import { Link, type LinkProps } from "react-router";

import { cx } from "../../lib/cx";
import type { ButtonVariant } from "./Button";
import styles from "./Button.module.css";

export interface ButtonLinkProps extends LinkProps {
  variant?: ButtonVariant;
  size?: "md" | "sm";
  icon?: ReactNode;
}

/** A real link (it navigates) styled as a button. */
export function ButtonLink({
  variant = "secondary",
  size = "md",
  icon,
  className,
  children,
  ...rest
}: ButtonLinkProps) {
  return (
    <Link
      className={cx(styles.button, styles.link, className)}
      data-variant={variant}
      data-size={size}
      {...rest}
    >
      {icon && (
        <span className={styles.icon} aria-hidden="true">
          {icon}
        </span>
      )}
      {children}
    </Link>
  );
}
