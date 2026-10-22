import type { StatusTone } from "../../components/Badge";
import type { Mode } from "../../lib/preferences";
import type { ResourceRef, TableColumn, TableRow } from "../../lib/types";

/** Columns whose text is a state word, shown as a status pill. */
const STATUS_COLUMNS = new Set(["Status", "Phase", "State"]);

export function isStatusColumn(c: TableColumn): boolean {
  return STATUS_COLUMNS.has(c.name);
}

const CRITICAL =
  /CrashLoopBackOff|Error|OOMKilled|ImagePullBackOff|ErrImagePull|InvalidImageName|Failed|Evicted|CreateContainer|NotReady|Lost|Unknown|BackOff|DeadlineExceeded/i;
const HIGH =
  /Pending|ContainerCreating|PodInitializing|Init:|Terminating|SchedulingDisabled|Waiting|Released|Progressing/i;
const OK =
  /^(Running|Succeeded|Completed|Complete|Active|Bound|Ready|Available|True|Healthy|Synced)$/i;

/** Maps a Kubernetes state word to a pill tone. Unknown words stay neutral. */
export function statusTone(text: string): StatusTone {
  if (CRITICAL.test(text)) return "critical";
  if (HIGH.test(text)) return "high";
  if (OK.test(text)) return "ok";
  return "neutral";
}

/** Glance shows what `kubectl get` shows; Deep adds the `-o wide` columns. */
export function columnsFor(
  columns: TableColumn[],
  mode: Mode,
  hidden: ReadonlySet<string>,
): number[] {
  const out: number[] = [];
  columns.forEach((c, i) => {
    if (c.priority > 0 && (mode === "glance" || hidden.has(c.name))) return;
    out.push(i);
  });
  return out;
}

/** The column holding the object's name. */
export function nameColumn(columns: TableColumn[]): number {
  const i = columns.findIndex((c) => c.format === "name");
  return i >= 0 ? i : 0;
}

const isNumberColumn = (c: TableColumn) => c.type === "integer" || c.type === "number";

export function isNumeric(c: TableColumn): boolean {
  return isNumberColumn(c);
}

/** Durations like "4h49m": muted, and "Age" sorts by creation time. */
export function isAgeColumn(c: TableColumn): boolean {
  return c.name === "Age" || c.name === "Last Seen";
}

const collator = new Intl.Collator("en", { numeric: true, sensitivity: "base" });

/** Compares two rows on one column, the way a person expects. */
export function compareRows(a: TableRow, b: TableRow, index: number, column: TableColumn): number {
  if (column.name === "Age" && a.object.creationTimestamp && b.object.creationTimestamp) {
    // Newer objects are younger: a later timestamp is a smaller age.
    return Date.parse(b.object.creationTimestamp) - Date.parse(a.object.creationTimestamp);
  }
  const x = a.cells[index];
  const y = b.cells[index];
  if (isNumberColumn(column)) return Number(x ?? 0) - Number(y ?? 0);
  return collator.compare(formatCell(x), formatCell(y));
}

/** Renders any cell value as text. */
export function formatCell(v: unknown): string {
  if (v === null || v === undefined || v === "") return "—";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean" || typeof v === "bigint") return String(v);
  if (Array.isArray(v)) return v.map(formatCell).join(", ");
  return JSON.stringify(v);
}

/** Every word must appear somewhere in the row (cells or namespace). */
export function matchesSearch(row: TableRow, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return true;
  const hay = [...row.cells.map(formatCell), row.object.namespace ?? ""].join(" ").toLowerCase();
  return words.every((w) => hay.includes(w));
}

/** Where a row's name links to. Pods get their own page; the rest the generic one. */
export function detailPath(ref: ResourceRef, row: TableRow): string {
  return objectPagePath(ref, row.object.namespace ?? "", row.object.name);
}

/** The page of one object; namespace "" for cluster-scoped kinds. */
export function objectPagePath(ref: ResourceRef, namespace: string, name: string): string {
  const ns = encodeURIComponent(namespace || "_");
  const n = encodeURIComponent(name);
  if ((ref.group === "" || ref.group === "core") && ref.resource === "pods") {
    return `/workloads/pods/${ns}/${n}`;
  }
  return `/resources/${encodeURIComponent(ref.group || "core")}/${encodeURIComponent(ref.version)}/${encodeURIComponent(ref.resource)}/${ns}/${n}`;
}

/** Groups that ship with Kubernetes, as opposed to CRDs people install. */
export function isBuiltInGroup(group: string): boolean {
  return group === "" || !group.includes(".") || group.endsWith(".k8s.io");
}
