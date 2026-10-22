// Small helpers the demo cluster shares: quantities, names, randomness and drift.

export type Obj = Record<string, unknown>;

export const MiB = 1024 ** 2;
export const GiB = 1024 ** 3;

export function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** The map at obj[key], created when missing. */
export function child(obj: Obj, key: string): Obj {
  const v = obj[key];
  if (isObj(v)) return v;
  const made: Obj = {};
  obj[key] = made;
  return made;
}

export function list(v: unknown): Obj[] {
  return Array.isArray(v) ? v.filter(isObj) : [];
}

/** A scalar as text; anything else (missing, a map, a list) is the fallback. */
export function str(v: unknown, fallback = ""): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return fallback;
}

export function text(v: unknown): string | undefined {
  return typeof v === "string" ? v : undefined;
}

export function labelsOf(o: Obj, ...path: string[]): Record<string, string> {
  let cur: unknown = o;
  for (const k of path) cur = isObj(cur) ? cur[k] : undefined;
  const out: Record<string, string> = {};
  if (isObj(cur)) for (const [k, v] of Object.entries(cur)) if (typeof v === "string") out[k] = v;
  return out;
}

const SUFFIX: Record<string, number> = {
  Ki: 1024,
  Mi: MiB,
  Gi: GiB,
  Ti: 1024 ** 4,
  k: 1e3,
  K: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  m: 1e-3,
};

/** "250m" → 0.25, "6Gi" → 6442450944. Unreadable quantities count as 0. */
export function quantity(v: unknown): number {
  if (typeof v === "number") return v;
  if (typeof v !== "string") return 0;
  const m = /^([0-9.]+)([a-zA-Z]*)$/.exec(v.trim());
  if (!m) return 0;
  const n = Number(m[1]);
  const unit = m[2] ?? "";
  if (!Number.isFinite(n)) return 0;
  if (unit === "") return n;
  const f = SUFFIX[unit];
  return f === undefined ? 0 : n * f;
}

/** Memory as kubectl writes it: 512Mi, 6Gi. */
export function memQuantity(bytes: number): string {
  if (bytes % GiB === 0) return `${String(bytes / GiB)}Gi`;
  return `${String(Math.round(bytes / MiB))}Mi`;
}

/** A small deterministic random source, so the demo starts the same way every time. */
export class Random {
  constructor(private seed: number) {}

  next(): number {
    // mulberry32
    this.seed = (this.seed + 0x6d2b79f5) | 0;
    let t = this.seed;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  }

  int(max: number): number {
    return Math.floor(this.next() * max);
  }

  pick<T>(items: readonly T[]): T {
    const item = items[this.int(items.length)];
    if (item === undefined) throw new Error("pick from an empty list");
    return item;
  }

  /** The kind of suffix Kubernetes gives generated names. */
  suffix(length: number): string {
    const chars = "bcdfghjklmnpqrstvwxz2456789";
    let s = "";
    for (let i = 0; i < length; i++) s += chars[this.int(chars.length)] ?? "x";
    return s;
  }
}

/** JSON with object keys sorted, so equal values print the same whatever their key order. */
function canonical(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canonical).join(",")}]`;
  if (isObj(v)) {
    return `{${Object.keys(v)
      .sort()
      .filter((k) => v[k] !== undefined)
      .map((k) => `${JSON.stringify(k)}:${canonical(v[k])}`)
      .join(",")}}`;
  }
  return v === undefined ? "null" : JSON.stringify(v);
}

/** A stable hash of a value, as a pod-template-hash style string. */
export function templateHash(v: unknown): string {
  const s = canonical(v);
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  const chars = "bcdfghjklmnpqrstvwxz2456789";
  let out = "";
  for (let i = 0; i < 10; i++) {
    h = Math.imul(h ^ (h >>> 13) ^ i, 2654435761);
    out += chars[(h >>> 0) % chars.length] ?? "x";
  }
  return out;
}

/**
 * A slow, smooth wobble in [-1, 1] for metrics: the same at the same time,
 * so values move a little between refreshes instead of jumping around.
 */
export function drift(seed: number, ms: number): number {
  const s = ms / 1000;
  return (
    0.55 * Math.sin(s / 97 + seed) +
    0.3 * Math.sin(s / 31 + seed * 2.3) +
    0.15 * Math.sin(s / 7 + seed * 4.1)
  );
}

export const iso = (ms: number) => new Date(ms).toISOString();
