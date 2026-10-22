/** Safe readers for objects that arrive as untyped JSON. */

export type Obj = Record<string, unknown>;

export function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** The value at path, or undefined when any step is missing. */
export function at(obj: unknown, ...path: (string | number)[]): unknown {
  let cur: unknown = obj;
  for (const key of path) {
    if (typeof key === "number") {
      if (!Array.isArray(cur)) return undefined;
      cur = cur[key];
    } else {
      if (!isObj(cur)) return undefined;
      cur = cur[key];
    }
  }
  return cur;
}

export function str(v: unknown): string | undefined {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return undefined;
}

export function num(v: unknown): number | undefined {
  return typeof v === "number" ? v : undefined;
}

export function arr(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

export function record(v: unknown): Record<string, string> {
  if (!isObj(v)) return {};
  const out: Record<string, string> = {};
  for (const [k, val] of Object.entries(v)) {
    const s = str(val);
    if (s !== undefined) out[k] = s;
  }
  return out;
}

/** "app=web,tier=front" from a matchLabels-style map. */
export function selectorString(labels: Record<string, string>): string {
  return Object.entries(labels)
    .map(([k, v]) => `${k}=${v}`)
    .join(",");
}
