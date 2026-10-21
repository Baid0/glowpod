import { useEffect, useRef, useState, useSyncExternalStore } from "react";

const REDUCED_MOTION = "(prefers-reduced-motion: reduce)";

function subscribeReducedMotion(onChange: () => void): () => void {
  if (typeof window.matchMedia !== "function") return () => undefined;
  const query = window.matchMedia(REDUCED_MOTION);
  query.addEventListener("change", onChange);
  return () => {
    query.removeEventListener("change", onChange);
  };
}

function readReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia(REDUCED_MOTION).matches;
}

export function usePrefersReducedMotion(): boolean {
  return useSyncExternalStore(subscribeReducedMotion, readReducedMotion, () => false);
}

const easeOut = (t: number) => 1 - Math.pow(1 - t, 3);

interface TweenOptions {
  /** Duration of the first move away from 0 (the sweep-in). */
  firstMs: number;
  /** Duration of every later move. */
  updateMs: number;
}

/**
 * Animates a number toward `target` with an ease-out curve. The first move
 * away from 0 uses `firstMs`, later ones `updateMs`. Jumps straight to the
 * target when the user prefers reduced motion.
 */
export function useTween(target: number, { firstMs, updateMs }: TweenOptions): number {
  const reduced = usePrefersReducedMotion();
  const [shown, setShown] = useState(0);
  const current = useRef(0);
  const swept = useRef(false);

  useEffect(() => {
    if (reduced || typeof requestAnimationFrame !== "function") {
      current.current = target;
      return;
    }
    const from = current.current;
    if (from === target) {
      // Already there (e.g. reduced motion was just turned off); sync the state.
      const frame = requestAnimationFrame(() => {
        setShown(target);
      });
      return () => {
        cancelAnimationFrame(frame);
      };
    }

    const duration = swept.current ? updateMs : firstMs;
    if (target !== 0) swept.current = true;

    const start = performance.now();
    let frame = requestAnimationFrame(function step(now: number) {
      const t = Math.min(1, (now - start) / duration);
      current.current = from + (target - from) * easeOut(t);
      setShown(current.current);
      if (t < 1) frame = requestAnimationFrame(step);
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [target, reduced, firstMs, updateMs]);

  return reduced ? target : shown;
}
