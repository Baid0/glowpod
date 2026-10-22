import { createContext, useContext } from "react";

export type Mode = "glance" | "deep";
/** Dark is the default; system follows the OS. */
export type ThemeChoice = "dark" | "light" | "system";
export type Density = "comfortable" | "compact";
/** High and critical gauge zones, as fractions. */
export type Thresholds = [number, number];

export const REFRESH_CHOICES = [5_000, 15_000, 30_000, 60_000, 0] as const;
export const DEFAULT_REFRESH_MS = 15_000;

export interface Preferences {
  mode: Mode;
  setMode: (m: Mode) => void;
  panelCollapsed: boolean;
  setPanelCollapsed: (c: boolean) => void;
  /** The picked namespaces on the current cluster; empty means all of them. */
  namespaces: string[];
  setNamespaces: (ns: string[]) => void;
  /** The same as a ?namespace= value: "" for all, else names joined by commas. */
  namespace: string;
  /** Picks one namespace ("" = all). */
  setNamespace: (ns: string) => void;
  theme: ThemeChoice;
  setTheme: (t: ThemeChoice) => void;
  /** The theme in effect: system resolved against the OS. */
  resolvedTheme: "dark" | "light";
  density: Density;
  setDensity: (d: Density) => void;
  /** How often gauges refresh; 0 = only by the live stream and on focus. */
  refreshMs: number;
  setRefreshMs: (ms: number) => void;
  /** This user's thresholds for a cluster, if they changed them. */
  thresholdsFor: (cluster: string) => Thresholds | undefined;
  setThresholds: (cluster: string, t: Thresholds | undefined) => void;
  /** The ?namespace= value saved for a cluster ("" = all). */
  namespaceFor: (cluster: string) => string;
  setNamespacesFor: (cluster: string, ns: string[]) => void;
}

/** prefs with namespace and namespaces read and written for one cluster. */
export function forCluster(prefs: Preferences, cluster: string): Preferences {
  const namespace = prefs.namespaceFor(cluster);
  return {
    ...prefs,
    namespace,
    namespaces: namespace ? namespace.split(",") : [],
    setNamespaces: (ns) => {
      prefs.setNamespacesFor(cluster, ns);
    },
    setNamespace: (ns) => {
      prefs.setNamespacesFor(cluster, ns ? ns.split(",") : []);
    },
  };
}

export const PreferencesContext = createContext<Preferences | null>(null);

// Saved per user. In local mode that's this browser; storage can be
// unavailable (private windows), so every access is guarded.
export const KEYS = {
  mode: "glowpod.mode",
  panelCollapsed: "glowpod.panelCollapsed",
  /** Before many clusters: one ?namespace= value, read once into namespaces. */
  legacyNamespace: "glowpod.namespace",
  /** Picked namespaces per cluster, as JSON: { [cluster]: "shop,payments" }. */
  namespaces: "glowpod.namespaces",
  // public/theme.js reads this key before the app loads.
  theme: "glowpod.theme",
  density: "glowpod.density",
  refresh: "glowpod.refreshMs",
  thresholds: "glowpod.thresholds",
} as const;

/** Thresholds must rise and stay between 0 and 1. */
export function validThresholds(t: unknown): t is Thresholds {
  return (
    Array.isArray(t) &&
    t.length === 2 &&
    typeof t[0] === "number" &&
    typeof t[1] === "number" &&
    t[0] > 0 &&
    t[0] < t[1] &&
    t[1] < 1
  );
}

export function usePreferences(): Preferences {
  const p = useContext(PreferencesContext);
  if (!p) throw new Error("usePreferences must be used inside <PreferencesProvider>");
  return p;
}

/** "all namespaces", "shop", "shop and payments", "3 namespaces". */
export function describeNamespaces(namespaces: string[]): string {
  const [first, second] = namespaces;
  if (first === undefined) return "all namespaces";
  if (second === undefined) return first;
  if (namespaces.length === 2) return `${first} and ${second}`;
  return `${String(namespaces.length)} namespaces`;
}

/** Sorted, without duplicates or empty names. */
export function normalizeNamespaces(namespaces: string[]): string[] {
  return [...new Set(namespaces.filter(Boolean))].sort();
}
