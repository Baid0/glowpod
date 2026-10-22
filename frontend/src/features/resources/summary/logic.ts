import type { StatusTone } from "../../../components/Badge";

// Conditions where True is bad news.
const BAD_WHEN_TRUE = /Pressure|Unavailable|Failed|Failure|Degraded|Stalled|Error|ReplicaFailure/;

/** ok for a healthy condition, critical for a bad one, high while unknown. */
export function conditionTone(type: string, status: string): StatusTone {
  if (status === "Unknown") return "high";
  const good = BAD_WHEN_TRUE.test(type) ? status === "False" : status === "True";
  return good ? "ok" : "critical";
}

const WORKLOAD_RESOURCES = new Set(["deployments", "statefulsets", "daemonsets", "replicasets"]);

export function isWorkload(resource: string) {
  return WORKLOAD_RESOURCES.has(resource);
}
