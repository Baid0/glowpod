import { describe, expect, it } from "vitest";

import type { TableColumn, TableRow } from "../../lib/types";
import {
  columnsFor,
  compareRows,
  detailPath,
  formatCell,
  isBuiltInGroup,
  matchesSearch,
  nameColumn,
  statusTone,
} from "./cells";

const col = (name: string, priority = 0, type = "string", format?: string): TableColumn => ({
  name,
  type,
  priority,
  ...(format ? { format } : {}),
});

const row = (cells: unknown[], ns = "payments", created?: string): TableRow => ({
  cells,
  object: {
    name: String(cells[0]),
    namespace: ns,
    uid: String(cells[0]),
    ...(created ? { creationTimestamp: created } : {}),
  },
});

describe("statusTone", () => {
  it.each([
    ["Running", "ok"],
    ["Completed", "ok"],
    ["Bound", "ok"],
    ["Ready", "ok"],
    ["Pending", "high"],
    ["ContainerCreating", "high"],
    ["Init:0/2", "high"],
    ["Terminating", "high"],
    ["Ready,SchedulingDisabled", "high"],
    ["CrashLoopBackOff", "critical"],
    ["OOMKilled", "critical"],
    ["ImagePullBackOff", "critical"],
    ["Error", "critical"],
    ["NotReady", "critical"],
    ["Evicted", "critical"],
    ["Shiny", "neutral"],
  ])("%s → %s", (text, tone) => {
    expect(statusTone(text)).toBe(tone);
  });
});

describe("columnsFor", () => {
  const cols = [col("Name"), col("Status"), col("IP", 1), col("Node", 1)];
  it("Glance shows only what kubectl get shows", () => {
    expect(columnsFor(cols, "glance", new Set())).toEqual([0, 1]);
  });
  it("Deep shows the wide columns too, minus hidden ones", () => {
    expect(columnsFor(cols, "deep", new Set())).toEqual([0, 1, 2, 3]);
    expect(columnsFor(cols, "deep", new Set(["IP"]))).toEqual([0, 1, 3]);
  });
});

describe("nameColumn", () => {
  it("finds the name-formatted column, else the first", () => {
    expect(nameColumn([col("Ready"), col("Name", 0, "string", "name")])).toBe(1);
    expect(nameColumn([col("Title"), col("Kind")])).toBe(0);
  });
});

describe("compareRows", () => {
  it("sorts numbers numerically and names naturally", () => {
    const restarts = col("Restarts", 0, "integer");
    expect(compareRows(row(["a", 2]), row(["b", 10]), 1, restarts)).toBeLessThan(0);
    const name = col("Name");
    expect(compareRows(row(["pod-2"]), row(["pod-10"]), 0, name)).toBeLessThan(0);
  });

  it("sorts Age by creation time, youngest first", () => {
    const age = col("Age");
    const old = row(["old", "3d"], "a", "2026-10-19T00:00:00Z");
    const young = row(["young", "5m"], "a", "2026-10-22T00:00:00Z");
    expect(compareRows(young, old, 1, age)).toBeLessThan(0);
  });
});

describe("formatCell", () => {
  it.each([
    [null, "—"],
    ["", "—"],
    ["<none>", "<none>"],
    [3, "3"],
    [true, "true"],
    [["a", "b"], "a, b"],
    [{ k: 1 }, '{"k":1}'],
  ])("%j → %s", (v, want) => {
    expect(formatCell(v)).toBe(want);
  });
});

describe("matchesSearch", () => {
  const r = row(["payments-api-7d9f6b-x2kq9", "CrashLoopBackOff"], "payments");
  it.each([
    ["", true],
    ["payments", true],
    ["crash payments", true],
    ["CRASH", true],
    ["running", false],
  ])("%j → %s", (q, want) => {
    expect(matchesSearch(r, q)).toBe(want);
  });
});

describe("detailPath", () => {
  it("pods get their own page; others the generic one", () => {
    expect(
      detailPath({ group: "", version: "v1", resource: "pods" }, row(["api-1"], "payments")),
    ).toBe("/workloads/pods/payments/api-1");
    expect(
      detailPath({ group: "apps", version: "v1", resource: "deployments" }, row(["web"], "shop")),
    ).toBe("/resources/apps/v1/deployments/shop/web");
    const node: TableRow = { cells: ["w1"], object: { name: "w1", uid: "n1" } };
    expect(detailPath({ group: "", version: "v1", resource: "nodes" }, node)).toBe(
      "/resources/core/v1/nodes/_/w1",
    );
  });
});

describe("isBuiltInGroup", () => {
  it.each([
    ["", true],
    ["apps", true],
    ["networking.k8s.io", true],
    ["argoproj.io", false],
    ["monitoring.coreos.com", false],
  ])("%j → %s", (g, want) => {
    expect(isBuiltInGroup(g)).toBe(want);
  });
});
