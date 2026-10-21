import type { Problem } from "../../lib/types";

/** Where "View" goes for a problem's object. */
export function problemPath(p: Problem): string {
  const ns = encodeURIComponent(p.namespace ?? "");
  const name = encodeURIComponent(p.name);
  switch (p.kind) {
    case "Node":
      return `/nodes/${name}`;
    case "Job":
      return `/workloads/jobs/${ns}/${name}`;
    default:
      return `/workloads/pods/${ns}/${name}`;
  }
}

const SHORT_KINDS: Record<string, string> = {
  Deployment: "deploy",
  ReplicaSet: "rs",
  StatefulSet: "sts",
  DaemonSet: "ds",
  Job: "job",
  CronJob: "cj",
  Node: "node",
  Service: "svc",
  PersistentVolumeClaim: "pvc",
  HorizontalPodAutoscaler: "hpa",
  Ingress: "ing",
};

/** kubectl-style object names: pods by name, others as "deploy/web". */
export function shortObject(kind: string, name: string): string {
  if (kind === "Pod") return name;
  return `${SHORT_KINDS[kind] ?? kind.toLowerCase()}/${name}`;
}
