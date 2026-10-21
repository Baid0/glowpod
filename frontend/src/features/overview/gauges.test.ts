import { describe, expect, it } from "vitest";

import type { Gauge } from "../../lib/types";
import { gaugeView, trendWindowLabel } from "./gauges";

const live = { loading: false, stale: false };
const gib = 1024 ** 3;

describe("gaugeView", () => {
  it("loading and missing gauges", () => {
    expect(gaugeView("cpu", undefined, { loading: true, stale: false }).state).toBe("loading");
    expect(gaugeView("cpu", undefined, live).state).toBe("error");
  });

  it("CPU keeps cores and shows the requested share and trend", () => {
    const g: Gauge = {
      state: "live",
      value: 24.8,
      max: 40,
      unit: "cores",
      requested: 30,
      trend: 0.04,
      trendWindow: 3600,
    };
    expect(gaugeView("cpu", g, live)).toMatchObject({
      label: "CPU",
      state: "live",
      value: 24.8,
      max: 40,
      unit: "cores",
      requested: 30,
      trend: 0.04,
      trendLabel: "4% in 1h",
    });
  });

  it("memory scales bytes to one unit", () => {
    const g: Gauge = {
      state: "live",
      value: 49.9 * gib,
      max: 64 * gib,
      unit: "bytes",
      requested: 44.8 * gib,
    };
    const v = gaugeView("memory", g, live);
    expect(v.unit).toBe("GiB");
    expect(v.value).toBeCloseTo(49.9);
    expect(v.max).toBe(64);
    expect(v.requested).toBeCloseTo(44.8);
    expect(v.trend).toBeUndefined();
  });

  it("pods show nodes × max and a pod-count trend", () => {
    const g: Gauge = {
      state: "live",
      value: 297,
      max: 660,
      unit: "pods",
      nodes: 6,
      perNodeMax: 110,
      trend: 12 / 660,
      trendWindow: 720,
    };
    expect(gaugeView("pods", g, live)).toMatchObject({
      note: "6 nodes × 110 max",
      trendLabel: "12 in 12 min",
    });
  });

  it("storage without usage shows capacity, not a metrics-server link", () => {
    const g: Gauge = { state: "no-metrics", value: 0, max: 7 * gib, unit: "bytes", volumes: 2 };
    expect(gaugeView("storage", g, live)).toMatchObject({
      state: "no-metrics",
      noDataLabel: "7 GiB",
      helpHref: null,
      note: "2 volumes",
    });
  });

  it("network uses decimal rates against the 7-day peak", () => {
    const g: Gauge = {
      state: "live",
      value: 120e6,
      max: 200e6,
      unit: "bytes/s",
      source: "prometheus",
    };
    expect(gaugeView("network", g, live)).toMatchObject({
      display: "value",
      value: 120,
      max: 200,
      detail: "MB/s in + out",
      note: "of 7-day peak 200",
      trendLabel: "via Prometheus",
    });
  });

  it("live gauges go stale when refreshes fail; other states stay", () => {
    const ctx = { loading: false, stale: true, updated: "updated 3 min ago" };
    expect(gaugeView("cpu", { state: "live", value: 1, max: 2, unit: "cores" }, ctx)).toMatchObject(
      {
        state: "stale",
        updated: "updated 3 min ago",
      },
    );
    expect(
      gaugeView("cpu", { state: "no-metrics", value: 0, max: 2, unit: "cores" }, ctx).state,
    ).toBe("no-metrics");
  });

  it("passes the cluster's thresholds", () => {
    const v = gaugeView(
      "cpu",
      { state: "live", value: 1, max: 2, unit: "cores" },
      { ...live, thresholds: [0.6, 0.9] },
    );
    expect(v.thresholds).toEqual([0.6, 0.9]);
  });
});

describe("trendWindowLabel", () => {
  it.each([
    [undefined, "in 1h"],
    [3600, "in 1h"],
    [3550, "in 1h"],
    [720, "in 12 min"],
    [20, "in 1 min"],
  ])("%s s → %s", (s, want) => {
    expect(trendWindowLabel(s)).toBe(want);
  });
});
