import type { MapNode, ResourceMap } from "../../lib/types";

export const BOX_W = 216;
export const BOX_H = 56;
export const GAP_X = 72;
export const GAP_Y = 16;
/** Room above the boxes for the column headings. */
export const HEAD = 28;

export interface Placed extends MapNode {
  x: number;
  y: number;
}

/**
 * Places nodes in their columns. Workloads go first, by name; every other
 * column is ordered by the average row of what it connects to, which keeps
 * most lines from crossing. Empty columns are dropped.
 */
export function layout(map: ResourceMap): {
  nodes: Placed[];
  width: number;
  height: number;
  /** The columns drawn, left to right. */
  columns: number[];
} {
  const byColumn = new Map<number, MapNode[]>();
  for (const n of map.nodes) byColumn.set(n.column, [...(byColumn.get(n.column) ?? []), n]);
  const row = new Map<string, number>();
  const neighbours = (id: string) =>
    map.edges.flatMap((e) => (e.from === id ? [e.to] : e.to === id ? [e.from] : []));
  const centre = (n: MapNode) => {
    const rows = neighbours(n.id)
      .map((id) => row.get(id))
      .filter((r): r is number => r !== undefined);
    return rows.length > 0 ? rows.reduce((a, b) => a + b, 0) / rows.length : Infinity;
  };
  const place = (column: number) => {
    const list = byColumn.get(column) ?? [];
    const sorted = column === 2 ? list : list.toSorted((a, b) => centre(a) - centre(b));
    sorted.forEach((n, i) => row.set(n.id, i));
    byColumn.set(column, sorted);
  };
  // Workloads anchor the map; the rest arrange around them.
  place(2);
  place(1);
  place(0);
  place(3);

  const columns = [0, 1, 2, 3].filter((c) => (byColumn.get(c)?.length ?? 0) > 0);
  const tallest = Math.max(1, ...columns.map((c) => byColumn.get(c)?.length ?? 0));
  const height = HEAD + tallest * (BOX_H + GAP_Y) - GAP_Y;
  const nodes: Placed[] = [];
  columns.forEach((c, ci) => {
    const list = byColumn.get(c) ?? [];
    // Shorter columns are centred against the tallest.
    const offset = ((tallest - list.length) * (BOX_H + GAP_Y)) / 2;
    list.forEach((n, i) => {
      nodes.push({ ...n, x: ci * (BOX_W + GAP_X), y: HEAD + offset + i * (BOX_H + GAP_Y) });
    });
  });
  const width = Math.max(BOX_W, columns.length * (BOX_W + GAP_X) - GAP_X);
  return { nodes, width, height, columns };
}

const WORKLOADS: Record<string, string> = {
  Deployment: "deployments",
  StatefulSet: "statefulsets",
  DaemonSet: "daemonsets",
};
const RESOURCES: Record<string, string> = {
  Ingress: "networking.k8s.io/v1/ingresses",
  Service: "core/v1/services",
  ConfigMap: "core/v1/configmaps",
  Secret: "core/v1/secrets",
  PersistentVolumeClaim: "core/v1/persistentvolumeclaims",
};

export function nodePath(n: MapNode): string | undefined {
  if (n.missing) return undefined;
  const ns = encodeURIComponent(n.namespace);
  const name = encodeURIComponent(n.name);
  const w = WORKLOADS[n.kind];
  if (w) return `/workloads/${w}/${ns}/${name}`;
  const r = RESOURCES[n.kind];
  return r ? `/resources/${r}/${ns}/${name}` : undefined;
}

/** Fits a name in a box by cutting the middle: "payments-a…-x2kq9". */
export function fit(text: string, max: number): string {
  if (text.length <= max) return text;
  const keep = max - 1;
  return text.slice(0, Math.ceil(keep / 2)) + "…" + text.slice(text.length - Math.floor(keep / 2));
}
