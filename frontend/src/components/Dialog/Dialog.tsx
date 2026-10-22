import { useEffect, useId, useRef, type ReactNode } from "react";

import styles from "./Dialog.module.css";

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  /** One or two plain sentences under the title. */
  description?: ReactNode;
  children?: ReactNode;
  /** Buttons, right-aligned; put the main action last. */
  footer?: ReactNode;
  /** wide fits a diff or an editor. */
  size?: "md" | "wide";
  /** Submitting the form runs this; Enter in a field submits. */
  onSubmit?: () => void;
}

/** A modal on the native <dialog>: focus stays inside and Esc closes it. */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = "md",
  onSubmit,
}: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      className={styles.dialog}
      data-size={size}
      aria-labelledby={titleId}
      aria-describedby={description ? descId : undefined}
      onClose={onClose}
    >
      {open && (
        <form
          method="dialog"
          className={styles.box}
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit?.();
          }}
        >
          <div className={styles.head}>
            <h2 id={titleId} className={styles.title}>
              {title}
            </h2>
            {description && (
              <div id={descId} className={styles.description}>
                {description}
              </div>
            )}
          </div>
          {children && <div className={styles.body}>{children}</div>}
          {footer && <div className={styles.footer}>{footer}</div>}
        </form>
      )}
    </dialog>
  );
}
