import { createContext, useContext } from "react";

export type Mode = "glance" | "deep";

export interface Preferences {
  mode: Mode;
  setMode: (m: Mode) => void;
  panelCollapsed: boolean;
  setPanelCollapsed: (c: boolean) => void;
  /** "" means all namespaces. */
  namespace: string;
  setNamespace: (ns: string) => void;
}

export const PreferencesContext = createContext<Preferences | null>(null);

// Saved per user. In local mode that's this browser; storage can be
// unavailable (private windows), so every access is guarded.
export const KEYS = {
  mode: "glowpod.mode",
  panelCollapsed: "glowpod.panelCollapsed",
  namespace: "glowpod.namespace",
} as const;

export function usePreferences(): Preferences {
  const p = useContext(PreferencesContext);
  if (!p) throw new Error("usePreferences must be used inside <PreferencesProvider>");
  return p;
}
