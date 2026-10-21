/**
 * Pure geometry and classification for HalfGauge: a 180° arc where the left
 * end is 0% and the right end is 100%.
 */

export type GaugeSize = "L" | "M" | "S";
export type GaugeZone = "ok" | "high" | "critical";
export type Thresholds = readonly [high: number, critical: number];

export const DEFAULT_THRESHOLDS: Thresholds = [0.7, 0.85];

interface SizeSpec {
  /** Overall width in px. */
  width: number;
  /** Value arc and track thickness. */
  stroke: number;
  /** Requested (inner) arc thickness; 0 = not drawn at this size. */
  innerStroke: number;
  /** Gap between the track and the inner arc. */
  innerGap: number;
  /** Knob radius and outline width; 0 = no knob. */
  knobRadius: number;
  knobOutline: number;
  /** Threshold tick length and its gap from the track; 0 = no ticks. */
  tickLength: number;
  tickGap: number;
  /** Dot size of the dashed (no-metrics / error) track. */
  dotStroke: number;
}

export const SIZE_SPECS: Record<GaugeSize, SizeSpec> = {
  L: {
    width: 220,
    stroke: 22,
    innerStroke: 4,
    innerGap: 4,
    knobRadius: 12,
    knobOutline: 3,
    tickLength: 6,
    tickGap: 3,
    dotStroke: 5,
  },
  M: {
    width: 140,
    stroke: 14,
    innerStroke: 3,
    innerGap: 3,
    knobRadius: 8,
    knobOutline: 2.5,
    tickLength: 4,
    tickGap: 2,
    dotStroke: 4,
  },
  S: {
    width: 64,
    stroke: 7,
    innerStroke: 0,
    innerGap: 0,
    knobRadius: 0,
    knobOutline: 0,
    tickLength: 0,
    tickGap: 0,
    dotStroke: 3,
  },
};

export interface Layout {
  width: number;
  height: number;
  cx: number;
  cy: number;
  /** Radius of the track's center line. */
  radius: number;
  /** Radius of the requested arc's center line. */
  innerRadius: number;
  spec: SizeSpec;
}

export function layoutFor(size: GaugeSize): Layout {
  const spec = SIZE_SPECS[size];
  const half = spec.stroke / 2;
  const knobReach = spec.knobRadius + spec.knobOutline / 2;
  // Room outside the track for ticks and the part of the knob that overhangs it.
  const pad = Math.max(spec.tickGap + spec.tickLength, knobReach - half, 0) + 1;
  const radius = spec.width / 2 - half - pad;
  const cy = pad + half + radius;
  const height = Math.ceil(cy + Math.max(half, knobReach) + 1);
  const innerRadius = radius - half - spec.innerGap - spec.innerStroke / 2;
  return { width: spec.width, height, cx: spec.width / 2, cy, radius, innerRadius, spec };
}

/** Clamps value / max into [0, 1]. A non-positive max reads as empty. */
export function fractionOf(value: number, max: number): number {
  if (!Number.isFinite(value) || !Number.isFinite(max) || max <= 0) return 0;
  return Math.min(1, Math.max(0, value / max));
}

export function zoneFor(fraction: number, thresholds: Thresholds = DEFAULT_THRESHOLDS): GaugeZone {
  const [high, critical] = thresholds;
  if (fraction >= critical) return "critical";
  if (fraction >= high) return "high";
  return "ok";
}

export const ZONE_WORDS: Record<GaugeZone, string> = {
  ok: "OK",
  high: "High",
  critical: "Critical",
};

export interface Point {
  x: number;
  y: number;
}

/** Point on the arc at `fraction` (0 = left end, 1 = right end). */
export function pointAt(fraction: number, radius: number, cx: number, cy: number): Point {
  const angle = Math.PI * (1 - fraction);
  return { x: cx + radius * Math.cos(angle), y: cy - radius * Math.sin(angle) };
}

/** SVG path for the full half circle, drawn left to right over the top. */
export function arcPath(radius: number, cx: number, cy: number): string {
  return `M ${round(cx - radius)} ${round(cy)} A ${round(radius)} ${round(radius)} 0 0 1 ${round(cx + radius)} ${round(cy)}`;
}

/** A short radial tick just outside the track, at `fraction`. */
export function tickLine(fraction: number, layout: Layout): { from: Point; to: Point } {
  const { spec, radius, cx, cy } = layout;
  const start = radius + spec.stroke / 2 + spec.tickGap;
  return {
    from: pointAt(fraction, start, cx, cy),
    to: pointAt(fraction, start + spec.tickLength, cx, cy),
  };
}

function round(n: number): number {
  return Math.round(n * 100) / 100;
}
