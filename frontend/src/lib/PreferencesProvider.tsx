import { useCallback, useMemo, useState, type ReactNode } from "react";

import { KEYS, PreferencesContext, type Mode } from "./preferences";

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

function usePersisted<T extends string | boolean>(
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

  const value = useMemo(
    () => ({ mode, setMode, panelCollapsed, setPanelCollapsed, namespace, setNamespace }),
    [mode, setMode, panelCollapsed, setPanelCollapsed, namespace, setNamespace],
  );
  return <PreferencesContext value={value}>{children}</PreferencesContext>;
}
