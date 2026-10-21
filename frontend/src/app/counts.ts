import type { KubeEvent, Overview } from "../lib/types";
import type { CountKey } from "./navigation";

const RECENT_WARNING_MS = 60 * 60 * 1000;

export interface Count {
  text: string;
  /** Show a rose dot: something here is failing. */
  problem: boolean;
  label: string;
}

export function countFor(
  key: CountKey,
  o: Overview | undefined,
  events: KubeEvent[] | undefined,
  now: number,
): Count | undefined {
  if (key === "events") {
    if (!events) return undefined;
    const warnings = events.filter(
      (e) => e.type === "Warning" && now - Date.parse(e.time) < RECENT_WARNING_MS,
    ).length;
    return warnings > 0
      ? {
          text: String(warnings),
          problem: true,
          label: `${warnings} ${warnings === 1 ? "warning" : "warnings"} in the last hour`,
        }
      : undefined;
  }
  if (!o) return undefined;
  switch (key) {
    case "nodes": {
      const hot = o.nodes.filter((n) => n.hot || !n.ready).length;
      return hot > 0
        ? {
            text: `${hot} hot · ${o.nodes.length}`,
            problem: true,
            label: `${hot} of ${o.nodes.length} nodes need attention`,
          }
        : { text: String(o.nodes.length), problem: false, label: `${o.nodes.length} nodes` };
    }
    case "namespaces":
      return {
        text: String(o.cluster.namespaces),
        problem: false,
        label: `${o.cluster.namespaces} namespaces`,
      };
    case "deployments":
    case "statefulSets":
    case "daemonSets": {
      const w = o.workloads[key];
      return {
        text: String(w.total),
        problem: w.failing > 0,
        label: `${w.total}, ${w.failing} failing`,
      };
    }
    case "jobs": {
      const j = o.workloads.jobs;
      const total = j.succeeded + j.failed + j.running;
      return {
        text: String(total),
        problem: j.failed > 0,
        label: `${total} in 24 hours, ${j.failed} failed`,
      };
    }
    case "pods": {
      const h = o.health;
      const total = h.running + h.pending + h.failing;
      return {
        text: String(total),
        problem: h.failing > 0,
        label: `${total} pods, ${h.failing} failing`,
      };
    }
  }
}
