import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { DEFAULT_CLUSTER } from "./api";
import {
  DEFAULT_REFRESH_MS,
  forCluster,
  KEYS,
  normalizeNamespaces,
  PreferencesContext,
  REFRESH_CHOICES,
  validThresholds,
  type Density,
  type Mode,
  type ThemeChoice,
  type Thresholds,
} from "./preferences";

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // not persisted; the in-memory value still works
  }
}

function usePersisted<T extends string | boolean | number>(
  key: string,
  fallback: T,
  parse: (raw: string) => T | undefined,
): [T, (v: T) => void] {
  const [value, setValue] = useState<T>(() => {
    const raw = read(key);
    return (raw !== null ? parse(raw) : undefined) ?? fallback;
  });
  const set = useCallback(
    (v: T) => {
      setValue(v);
      write(key, String(v));
    },
    [key],
  );
  return [value, set];
}

export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = usePersisted<Mode>(KEYS.mode, "deep", (raw) =>
    raw === "glance" || raw === "deep" ? raw : undefined,
  );
  const [panelCollapsed, setPanelCollapsed] = usePersisted<boolean>(
    KEYS.panelCollapsed,
    false,
    (raw) => raw === "true",
  );
  // Per cluster, each stored as the ?namespace= value. The one namespace
  // saved before there were many clusters belongs to the in-cluster one.
  const [namespaceMap, setNamespaceMap] = usePersisted<string>(
    KEYS.namespaces,
    legacyNamespaces(),
    (raw) => raw,
  );
  const namespaceFor = useCallback(
    (cluster: string) => {
      const v = parseMap(namespaceMap)[cluster];
      return typeof v === "string" ? normalizeNamespaces(v.split(",")).join(",") : "";
    },
    [namespaceMap],
  );
  const setNamespacesFor = useCallback(
    (cluster: string, list: string[]) => {
      const others = Object.entries(parseMap(namespaceMap)).filter(([k]) => k !== cluster);
      const v = normalizeNamespaces(list).join(",");
      setNamespaceMap(JSON.stringify(Object.fromEntries(v ? [...others, [cluster, v]] : others)));
    },
    [namespaceMap, setNamespaceMap],
  );
  const [theme, setTheme] = usePersisted<ThemeChoice>(KEYS.theme, "dark", (raw) =>
    raw === "dark" || raw === "light" || raw === "system" ? raw : undefined,
  );
  const [density, setDensity] = usePersisted<Density>(KEYS.density, "comfortable", (raw) =>
    raw === "comfortable" || raw === "compact" ? raw : undefined,
  );
  const [refreshMs, setRefreshMs] = usePersisted<number>(
    KEYS.refresh,
    DEFAULT_REFRESH_MS,
    (raw) => {
      const n = Number(raw);
      return (REFRESH_CHOICES as readonly number[]).includes(n) ? n : undefined;
    },
  );
  const [thresholdMap, setThresholdMap] = usePersisted<string>(KEYS.thresholds, "{}", (raw) => raw);

  const systemLight = useSystemLight();
  const resolvedTheme = theme === "system" ? (systemLight ? "light" : "dark") : theme;
  useEffect(() => {
    const root = document.documentElement;
    if (resolvedTheme === "light") root.dataset.theme = "light";
    else delete root.dataset.theme;
    root.dataset.density = density;
  }, [resolvedTheme, density]);

  const thresholdsFor = useCallback(
    (cluster: string): Thresholds | undefined => {
      const t = parseMap(thresholdMap)[cluster];
      return validThresholds(t) ? t : undefined;
    },
    [thresholdMap],
  );
  const setThresholds = useCallback(
    (cluster: string, t: Thresholds | undefined) => {
      const others = Object.entries(parseMap(thresholdMap)).filter(([k]) => k !== cluster);
      setThresholdMap(JSON.stringify(Object.fromEntries(t ? [...others, [cluster, t]] : others)));
    },
    [thresholdMap, setThresholdMap],
  );

  // Outside a cluster's pages (and in tests), namespaces are the in-cluster one's.
  const value = useMemo(
    () =>
      forCluster(
        {
          mode,
          setMode,
          panelCollapsed,
          setPanelCollapsed,
          namespace: "",
          setNamespace: () => undefined,
          namespaces: [],
          setNamespaces: () => undefined,
          namespaceFor,
          setNamespacesFor,
          theme,
          setTheme,
          resolvedTheme,
          density,
          setDensity,
          refreshMs,
          setRefreshMs,
          thresholdsFor,
          setThresholds,
        },
        DEFAULT_CLUSTER,
      ),
    [
      namespaceFor,
      setNamespacesFor,
      mode,
      setMode,
      panelCollapsed,
      setPanelCollapsed,
      theme,
      setTheme,
      resolvedTheme,
      density,
      setDensity,
      refreshMs,
      setRefreshMs,
      thresholdsFor,
      setThresholds,
    ],
  );
  return <PreferencesContext value={value}>{children}</PreferencesContext>;
}

/** The namespaces saved before there were many clusters, as a map. */
function legacyNamespaces(): string {
  const ns = read(KEYS.legacyNamespace);
  return JSON.stringify(ns ? { [DEFAULT_CLUSTER]: ns } : {});
}

function parseMap(raw: string): Record<string, unknown> {
  try {
    const v: unknown = JSON.parse(raw);
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/** Whether the OS asks for a light theme, kept up to date. */
function useSystemLight(): boolean {
  const query = useMemo(
    () =>
      typeof window.matchMedia === "function"
        ? window.matchMedia("(prefers-color-scheme: light)")
        : null,
    [],
  );
  const [light, setLight] = useState(query?.matches ?? false);
  useEffect(() => {
    if (!query) return;
    const onChange = (e: MediaQueryListEvent) => {
      setLight(e.matches);
    };
    query.addEventListener("change", onChange);
    return () => {
      query.removeEventListener("change", onChange);
    };
  }, [query]);
  return light;
}
