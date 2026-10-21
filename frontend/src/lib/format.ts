const one = new Intl.NumberFormat("en", { maximumFractionDigits: 1 });

/** 0.62 → "62%". */
export function percent(fraction: number): string {
  return `${Math.round(fraction * 100)}%`;
}

const BYTE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

/** Picks one binary unit for a value and its max, so "49.9 of 64 GiB" reads well. */
export function byteScale(max: number): { divisor: number; unit: string } {
  let i = 0;
  let v = max;
  while (v >= 1024 && i < BYTE_UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  return { divisor: 1024 ** i, unit: BYTE_UNITS[i] ?? "B" };
}

/** 1536 → "1.5 KiB". */
export function bytes(n: number): string {
  const { divisor, unit } = byteScale(n);
  return `${one.format(n / divisor)} ${unit}`;
}

/** Decimal rate units for network: 120e6 → "120 MB/s". */
export function rate(bytesPerSecond: number): { value: string; unit: string } {
  const units = ["B/s", "kB/s", "MB/s", "GB/s"];
  let i = 0;
  let v = bytesPerSecond;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  return { value: one.format(v >= 10 ? Math.round(v) : v), unit: units[i] ?? "B/s" };
}

export function number(n: number): string {
  return one.format(n);
}

/** Initials for an avatar: "kubernetes-admin" → "KA", "luka" → "LU". */
export function initials(name: string): string {
  const parts = name
    .replace(/^.*[:/]/, "") // system:serviceaccount:ns:name → name
    .split(/[\s._@-]+/)
    .filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return (parts[0] ?? "").slice(0, 2).toUpperCase();
  return ((parts[0]?.[0] ?? "") + (parts[1]?.[0] ?? "")).toUpperCase();
}
