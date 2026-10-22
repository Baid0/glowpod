import { createContext, useContext } from "react";

export interface Toast {
  /** ok for done, critical for failed. */
  tone: "ok" | "critical";
  message: string;
  detail?: string;
}

export const ToastContext = createContext<(t: Toast) => void>(() => undefined);

/** Shows a short note in the corner: "Scaled web to 3 replicas." */
export function useToast() {
  return useContext(ToastContext);
}
