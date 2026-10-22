import { IconX } from "@tabler/icons-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { type Toast, ToastContext } from "./toast";
import styles from "./Toast.module.css";

/** How long a toast stays; failures stay until dismissed. */
export const TOAST_MS = 5000;
const MAX_TOASTS = 3;

interface Shown extends Toast {
  id: number;
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Shown[]>([]);
  const next = useRef(0);

  const dismiss = useCallback((id: number) => {
    setToasts((ts) => ts.filter((t) => t.id !== id));
  }, []);
  const show = useCallback((t: Toast) => {
    const id = ++next.current;
    setToasts((ts) => [...ts, { ...t, id }].slice(-MAX_TOASTS));
  }, []);

  return (
    <ToastContext value={show}>
      {children}
      <div className={styles.region} role="region" aria-label="Notifications">
        {toasts.map((t) => (
          <ToastItem key={t.id} toast={t} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext>
  );
}

function ToastItem({ toast, onDismiss }: { toast: Shown; onDismiss: (id: number) => void }) {
  useEffect(() => {
    if (toast.tone === "critical") return;
    const timer = setTimeout(() => {
      onDismiss(toast.id);
    }, TOAST_MS);
    return () => {
      clearTimeout(timer);
    };
  }, [toast, onDismiss]);

  return (
    <div
      className={styles.toast}
      data-tone={toast.tone}
      role={toast.tone === "critical" ? "alert" : "status"}
    >
      <span className={styles.dot} aria-hidden="true" />
      <div className={styles.text}>
        <p className={styles.message}>{toast.message}</p>
        {toast.detail && <p className={styles.detail}>{toast.detail}</p>}
      </div>
      <button
        type="button"
        className={styles.close}
        aria-label="Dismiss"
        onClick={() => {
          onDismiss(toast.id);
        }}
      >
        <IconX size={16} stroke={1.75} aria-hidden="true" />
      </button>
    </div>
  );
}
