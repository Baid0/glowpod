import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import {
  DEFAULT_REFRESH_MS,
  KEYS,
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
  const [namespace, setNamespace] = usePersisted<string>(KEYS.namespace, "", (raw) => raw);
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

  const value = useMemo(
    () => ({
      mode,
      setMode,
      panelCollapsed,
      setPanelCollapsed,
      namespace,
      setNamespace,
      theme,
      setTheme,
      resolvedTheme,
      density,
      setDensity,
      refreshMs,
      setRefreshMs,
      thresholdsFor,
      setThresholds,
    }),
    [
      mode,
      setMode,
      panelCollapsed,
      setPanelCollapsed,
      namespace,
      setNamespace,
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
