import { describe, expect, it } from "vitest";

import {
  arcPath,
  fractionOf,
  layoutFor,
  pointAt,
  SIZE_SPECS,
  tickLine,
  zoneFor,
  type GaugeSize,
} from "./geometry";

describe("fractionOf", () => {
  it.each([
    { value: 24.8, max: 40, want: 0.62 },
    { value: 0, max: 40, want: 0 },
    { value: 40, max: 40, want: 1 },
    { value: 55, max: 40, want: 1 },
    { value: -3, max: 40, want: 0 },
    { value: 5, max: 0, want: 0 },
    { value: 5, max: -1, want: 0 },
    { value: Number.NaN, max: 40, want: 0 },
    { value: 5, max: Number.POSITIVE_INFINITY, want: 0 },
  ])("$value of $max → $want", ({ value, max, want }) => {
    expect(fractionOf(value, max)).toBeCloseTo(want);
  });
});

describe("zoneFor", () => {
  it.each([
    { fraction: 0, want: "ok" },
    { fraction: 0.62, want: "ok" },
    { fraction: 0.6999, want: "ok" },
    { fraction: 0.7, want: "high" },
    { fraction: 0.78, want: "high" },
    { fraction: 0.85, want: "critical" },
    { fraction: 1, want: "critical" },
  ])("$fraction → $want with default thresholds", ({ fraction, want }) => {
    expect(zoneFor(fraction)).toBe(want);
  });

  it("uses custom thresholds", () => {
    expect(zoneFor(0.62, [0.6, 0.9])).toBe("high");
    expect(zoneFor(0.92, [0.6, 0.9])).toBe("critical");
  });
});

describe("pointAt", () => {
  it.each([
    { fraction: 0, x: 0, y: 100 },
    { fraction: 0.5, x: 100, y: 0 },
    { fraction: 1, x: 200, y: 100 },
  ])("$fraction lies on the arc at ($x, $y)", ({ fraction, x, y }) => {
    const p = pointAt(fraction, 100, 100, 100);
    expect(p.x).toBeCloseTo(x);
    expect(p.y).toBeCloseTo(y);
  });
});

describe("arcPath", () => {
  it("draws a half circle from the left end over the top to the right end", () => {
    expect(arcPath(89, 110, 110)).toBe("M 21 110 A 89 89 0 0 1 199 110");
  });
});

describe("layoutFor", () => {
  it.each(["L", "M", "S"] as const)("keeps size %s inside its box", (size: GaugeSize) => {
    const layout = layoutFor(size);
    const { spec } = layout;
    expect(layout.width).toBe(SIZE_SPECS[size].width);
    // Track fits horizontally and at the top.
    expect(layout.cx - layout.radius - spec.stroke / 2).toBeGreaterThanOrEqual(0);
    expect(layout.cx + layout.radius + spec.stroke / 2).toBeLessThanOrEqual(layout.width);
    expect(layout.cy - layout.radius - spec.stroke / 2).toBeGreaterThanOrEqual(0);
    // Knob at either end fits.
    const reach = spec.knobRadius + spec.knobOutline / 2;
    expect(layout.cy + reach).toBeLessThanOrEqual(layout.height);
    expect(layout.cy - layout.radius - reach).toBeGreaterThanOrEqual(0);
  });

  it("puts ticks outside the track and inside the box", () => {
    const layout = layoutFor("L");
    const { from, to } = tickLine(0.85, layout);
    const dist = (p: { x: number; y: number }) => Math.hypot(p.x - layout.cx, p.y - layout.cy);
    expect(dist(from)).toBeGreaterThan(layout.radius + layout.spec.stroke / 2);
    expect(to.y).toBeGreaterThanOrEqual(0);
    expect(to.x).toBeLessThanOrEqual(layout.width);
  });

  it("puts the requested arc inside the track at L and M only", () => {
    expect(layoutFor("L").innerRadius).toBeLessThan(layoutFor("L").radius - 11);
    expect(layoutFor("M").spec.innerStroke).toBeGreaterThan(0);
    expect(layoutFor("S").spec.innerStroke).toBe(0);
  });
});
