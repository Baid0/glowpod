import { useEffect, useMemo, useState } from "react";

/** Whether a media query matches, kept up to date. False where unsupported. */
export function useMediaQuery(query: string): boolean {
  const list = useMemo(
    () => (typeof window.matchMedia === "function" ? window.matchMedia(query) : null),
    [query],
  );
  const [matches, setMatches] = useState(list?.matches ?? false);
  useEffect(() => {
    if (!list) return;
    const onChange = (e: MediaQueryListEvent) => {
      setMatches(e.matches);
    };
    list.addEventListener("change", onChange);
    return () => {
      list.removeEventListener("change", onChange);
    };
  }, [list]);
  return matches;
}
