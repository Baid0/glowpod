import type { StatusTone } from "../../components/Badge";

/** Helm's release statuses as pill tones. */
export function helmTone(status: string): StatusTone {
  switch (status) {
    case "deployed":
      return "ok";
    case "failed":
      return "critical";
    case "superseded":
    case "uninstalled":
      return "neutral";
    default:
      // pending-install, pending-upgrade, pending-rollback, uninstalling, unknown
      return "high";
  }
}

/** "pending-upgrade" → "Pending upgrade". */
export function helmWord(status: string): string {
  const s = status.replace(/-/g, " ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

const PAGES: Record<string, string> = {
  Deployment: "/workloads/deployments",
  StatefulSet: "/workloads/statefulsets",
  DaemonSet: "/workloads/daemonsets",
  Job: "/workloads/jobs",
  CronJob: "/workloads/cronjobs",
};

/** The page for an object a chart installed, from its apiVersion and kind. */
export function objectPath(o: {
  apiVersion: string;
  kind: string;
  namespace?: string;
  name: string;
}) {
  const ns = o.namespace ? encodeURIComponent(o.namespace) : "_";
  const name = encodeURIComponent(o.name);
  const page = PAGES[o.kind];
  if (page && o.namespace) return `${page}/${ns}/${name}`;
  const [group, version] = o.apiVersion.includes("/")
    ? o.apiVersion.split("/")
    : ["core", o.apiVersion];
  // Kinds become resources by the usual rule; good enough for links.
  const lower = o.kind.toLowerCase();
  const plural = lower.endsWith("s")
    ? lower + "es"
    : /[^aeiou]y$/.test(lower)
      ? lower.slice(0, -1) + "ies"
      : lower + "s";
  return `/resources/${encodeURIComponent(group ?? "core")}/${encodeURIComponent(version ?? "v1")}/${plural}/${ns}/${name}`;
}
