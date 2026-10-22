import type { HalfGaugeProps } from "../../components/HalfGauge";
import { byteScale, bytes, number } from "../../lib/format";
import type { Gauge } from "../../lib/types";

export type GaugeKind = "cpu" | "memory" | "pods" | "storage" | "network";

export const GAUGE_LABELS: Record<GaugeKind, string> = {
  cpu: "CPU",
  memory: "Memory",
  pods: "Pods",
  storage: "Storage",
  network: "Network",
};

export interface GaugeContext {
  /** No data yet. */
  loading: boolean;
  /** The last refresh failed; show what we have as stale. */
  stale: boolean;
  /** e.g. "updated 3 min ago", shown on stale gauges. */
  updated?: string;
  thresholds?: [number, number];
}

/** "in 1h" once an hour of history exists, else "in 12 min". */
export function trendWindowLabel(seconds: number | undefined): string {
  if (!seconds || seconds >= 59 * 60) return "in 1h";
  return `in ${Math.max(1, Math.round(seconds / 60))} min`;
}

const RATE_UNITS = ["B/s", "kB/s", "MB/s", "GB/s"];

function rateScale(max: number): { divisor: number; unit: string } {
  let i = 0;
  let v = max;
  while (v >= 1000 && i < RATE_UNITS.length - 1) {
    v /= 1000;
    i++;
  }
  return { divisor: 1000 ** i, unit: RATE_UNITS[i] ?? "B/s" };
}

/** Maps an API gauge to HalfGauge props for the Overview row. */
export function gaugeView(
  kind: GaugeKind,
  g: Gauge | undefined,
  ctx: GaugeContext,
): HalfGaugeProps {
  const base: HalfGaugeProps = {
    label: GAUGE_LABELS[kind],
    value: 0,
    max: 0,
    ...(ctx.thresholds ? { thresholds: ctx.thresholds } : {}),
  };
  if (ctx.loading) return { ...base, state: "loading" };
  if (!g) return { ...base, state: "error" };
  if (g.state === "no-access") {
    return {
      ...base,
      state: "no-metrics",
      noDataLabel: "No access",
      helpHref: null,
      helpLabel: g.message ?? "You can't see what this gauge needs.",
    };
  }

  const state = g.state === "live" && ctx.stale ? "stale" : g.state;
  const common: HalfGaugeProps = {
    ...base,
    state,
    ...(ctx.updated ? { updated: ctx.updated } : {}),
  };
  const windowLabel = trendWindowLabel(g.trendWindow);
  const pointsTrend =
    g.trend === undefined
      ? {}
      : { trend: g.trend, trendLabel: `${number(Math.abs(g.trend * 100))}% ${windowLabel}` };

  switch (kind) {
    case "cpu":
      return {
        ...common,
        value: g.value,
        max: g.max,
        unit: "cores",
        ...(g.requested !== undefined ? { requested: g.requested } : {}),
        ...pointsTrend,
      };
    case "memory": {
      const { divisor, unit } = byteScale(g.max);
      return {
        ...common,
        value: g.value / divisor,
        max: g.max / divisor,
        unit,
        ...(g.requested !== undefined ? { requested: g.requested / divisor } : {}),
        ...pointsTrend,
      };
    }
    case "pods": {
      const delta = g.trend === undefined ? undefined : Math.round(g.trend * g.max);
      return {
        ...common,
        value: g.value,
        max: g.max,
        unit: "pods",
        note: `${g.nodes ?? 0} nodes × ${g.perNodeMax ?? 0} max`,
        ...(delta === undefined
          ? {}
          : { trend: g.trend ?? 0, trendLabel: `${Math.abs(delta)} ${windowLabel}` }),
      };
    }
    case "storage": {
      const { divisor, unit } = byteScale(g.max);
      const volumes = g.volumes ?? 0;
      return {
        ...common,
        value: g.value / divisor,
        max: g.max / divisor,
        unit,
        note: `${volumes} ${volumes === 1 ? "volume" : "volumes"}`,
        // No usage reported: show the capacity and why, not a metrics-server link.
        noDataLabel: volumes > 0 ? bytes(g.max) : "No volumes",
        helpHref: null,
        helpLabel: volumes > 0 ? "Usage isn't reported for these volumes" : "Nothing stored yet",
        ...pointsTrend,
      };
    }
    case "network": {
      const { divisor, unit } = rateScale(g.max);
      return {
        ...common,
        value: g.value / divisor,
        max: g.max / divisor,
        display: "value",
        detail: `${unit} in + out`,
        note: `of 7-day peak ${number(g.max / divisor)}`,
        trendLabel: "via Prometheus",
      };
    }
  }
}
