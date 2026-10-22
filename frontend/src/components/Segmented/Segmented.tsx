import { useRef, type KeyboardEvent } from "react";

import styles from "./Segmented.module.css";

export interface SegmentedOption<T extends string | number> {
  value: T;
  label: string;
}

/** One choice among a few, as a radio group. Arrow keys move the choice. */
export function Segmented<T extends string | number>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange: (v: T) => void;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: KeyboardEvent, i: number) => {
    const step =
      e.key === "ArrowRight" || e.key === "ArrowDown"
        ? 1
        : e.key === "ArrowLeft" || e.key === "ArrowUp"
          ? -1
          : 0;
    if (!step) return;
    e.preventDefault();
    const next = (i + step + options.length) % options.length;
    const o = options[next];
    if (!o) return;
    onChange(o.value);
    refs.current[next]?.focus();
  };
  return (
    <div className={styles.group} role="radiogroup" aria-label={label}>
      {options.map((o, i) => (
        <button
          key={String(o.value)}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          tabIndex={o.value === value ? 0 : -1}
          className={styles.segment}
          onClick={() => {
            onChange(o.value);
          }}
          onKeyDown={(e) => {
            onKey(e, i);
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
