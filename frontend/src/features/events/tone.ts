import type { KubeEvent } from "../../lib/types";

const CRITICAL_REASONS = /BackOff|Failed|Kill|Unhealthy|Evict|OOM|Error/i;
const DONE_REASONS = /^(Completed|SuccessfulCreate|SawCompletedJob)$/;

export type EventTone = "critical" | "high" | "ok" | "info";

/** Warnings that mean something broke are critical; routine ones are high. */
export function eventTone(e: KubeEvent): EventTone {
  if (e.type === "Warning") return CRITICAL_REASONS.test(e.reason) ? "critical" : "high";
  if (DONE_REASONS.test(e.reason)) return "ok";
  return "info";
}

export const EVENT_WORDS: Record<EventTone, string> = {
  critical: "Problem",
  high: "Warning",
  ok: "Done",
  info: "Info",
};

const WORKLOADS: Record<string, string> = {
  Deployment: "deployments",
  StatefulSet: "statefulsets",
  DaemonSet: "daemonsets",
  ReplicaSet: "replicasets",
  Job: "jobs",
  CronJob: "cronjobs",
};

const RESOURCES: Record<string, string> = {
  Service: "core/v1/services",
  ConfigMap: "core/v1/configmaps",
  Secret: "core/v1/secrets",
  PersistentVolumeClaim: "core/v1/persistentvolumeclaims",
  PersistentVolume: "core/v1/persistentvolumes",
  Namespace: "core/v1/namespaces",
  Endpoints: "core/v1/endpoints",
  Ingress: "networking.k8s.io/v1/ingresses",
  HorizontalPodAutoscaler: "autoscaling/v2/horizontalpodautoscalers",
  PodDisruptionBudget: "policy/v1/poddisruptionbudgets",
};

/** The page for the object an event is about, when Glowpod has one. */
export function eventObjectPath(o: KubeEvent["object"]): string | undefined {
  const ns = encodeURIComponent(o.namespace ?? "");
  const name = encodeURIComponent(o.name);
  if (o.kind === "Pod") return `/workloads/pods/${ns}/${name}`;
  if (o.kind === "Node") return `/nodes/${name}`;
  const workload = WORKLOADS[o.kind];
  if (workload) return `/workloads/${workload}/${ns}/${name}`;
  const resource = RESOURCES[o.kind];
  if (resource) return `/resources/${resource}/${o.namespace ? ns : "_"}/${name}`;
  return undefined;
}
