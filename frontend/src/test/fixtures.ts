import type { KubeEvent, Overview, Problem, Session } from "../lib/types";

export const session: Session = {
  mode: "local",
  user: { username: "kubernetes-admin" },
  clusters: [{ id: "default", name: "prod-eu-1", server: "192.0.2.10:6443" }],
};

export const namespaces = ["checkout", "payments", "search"];

const gib = 1024 ** 3;

/** Roughly the design board's cluster. */
export function overview(namespace = ""): Overview {
  return {
    cluster: {
      id: "default",
      name: "prod-eu-1",
      server: "192.0.2.10:6443",
      version: "v1.34.2",
      nodes: 3,
      namespaces: 5,
    },
    namespace,
    generatedAt: new Date().toISOString(),
    thresholds: [0.7, 0.85],
    gauges: {
      cpu: {
        state: "live",
        value: 24.8,
        max: 40,
        unit: "cores",
        requested: 30,
        trend: 0.04,
        trendWindow: 3600,
      },
      memory: {
        state: "live",
        value: 49.9 * gib,
        max: 64 * gib,
        unit: "bytes",
        requested: 44.8 * gib,
      },
      pods: { state: "live", value: 297, max: 660, unit: "pods", nodes: 6, perNodeMax: 110 },
      network: { state: "live", value: 120e6, max: 200e6, unit: "bytes/s", source: "prometheus" },
      storage: {
        state: "no-metrics",
        value: 0,
        max: 3.9 * 1024 * gib,
        unit: "bytes",
        volumes: 18,
        message: "showing capacity only",
      },
    },
    workloads: {
      deployments: { total: 24, ready: 22, pending: 0, failing: 2 },
      statefulSets: { total: 7, ready: 6, pending: 1, failing: 0 },
      daemonSets: { total: 4, ready: 4, pending: 0, failing: 0 },
      jobs: { succeeded: 38, failed: 0, running: 0 },
    },
    health: { running: 290, pending: 4, failing: 3 },
    problems: problems(),
    problemCount: 4,
    nodes: [
      { name: "ip-10-0-1-21", ready: true, cpu: 0.5, memory: 0.72, hot: false },
      { name: "ip-10-0-2-14", ready: true, cpu: 0.88, memory: 0.93, hot: true },
      { name: "ip-10-0-3-31", ready: true, cpu: 0.41, memory: 0.7, hot: false },
    ],
    topConsumers: [
      { namespace: "payments", name: "payments-api-7d9f6b-ok", cpu: 1.25, memory: 512 * 1024 ** 2 },
      { namespace: "search", name: "search-indexer-1", cpu: 0.058, memory: 15.5 * 1024 ** 2 },
    ],
    apps: [
      {
        name: "payments-api",
        namespace: "payments",
        workloads: 1,
        ready: 3,
        total: 4,
        status: "critical",
      },
      {
        name: "checkout-web",
        namespace: "checkout",
        workloads: 1,
        ready: 4,
        total: 4,
        status: "ok",
      },
    ],
    recentDeploys: [
      {
        namespace: "checkout",
        name: "checkout-web",
        revision: "7",
        image: "ghcr.io/acme/checkout:v7",
        at: new Date(Date.now() - 3 * 60_000).toISOString(),
      },
    ],
  };
}

export function event(id: string, type: KubeEvent["type"], minutesAgo: number): KubeEvent {
  return {
    id,
    type,
    reason: type === "Warning" ? "BackOff" : "Pulled",
    message: "",
    object: { kind: "Pod", namespace: "payments", name: "payments-api-7d9f6b-x2kq9" },
    count: 1,
    time: new Date(Date.now() - minutesAgo * 60_000).toISOString(),
  };
}

const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

/** The design board's "Needs attention" list, plus a node under pressure. */
export function problems(): Problem[] {
  return [
    {
      severity: "critical",
      reason: "CrashLoopBackOff",
      title: "Keeps restarting",
      detail: "restarted 14 times · exit code 1",
      hint: "Check the logs from the last crash.",
      kind: "Pod",
      namespace: "payments",
      name: "payments-api-7d9f6b-x2kq9",
      since: minutesAgo(23),
    },
    {
      severity: "critical",
      reason: "OOMKilled",
      title: "Ran out of memory",
      detail: "hit its 512Mi memory limit",
      hint: "Raise its memory limit, or find what uses the memory.",
      kind: "Pod",
      namespace: "checkout",
      name: "checkout-web-6f4b9-m8zr2",
      since: minutesAgo(41),
    },
    {
      severity: "high",
      reason: "Pending",
      title: "Waiting for a node",
      detail: "no node has 6Gi memory free",
      hint: "Free up room, add a node, or lower its requests.",
      kind: "Pod",
      namespace: "search",
      name: "search-indexer-0",
      since: minutesAgo(6),
    },
    {
      severity: "high",
      reason: "MemoryPressure",
      title: "Node is low on memory",
      detail: "",
      hint: "Free up memory or move pods off the node.",
      kind: "Node",
      name: "ip-10-0-2-14",
      since: minutesAgo(9),
    },
  ];
}

export function events(): KubeEvent[] {
  return [
    {
      ...event("e1", "Warning", 1),
      message: "Back-off restarting failed container app",
    },
    {
      ...event("e2", "Normal", 3),
      reason: "ScalingReplicaSet",
      message: "Scaled up replica set checkout-web-6f4b9 to 4",
      object: { kind: "Deployment", namespace: "checkout", name: "checkout-web" },
    },
    {
      ...event("e3", "Normal", 12),
      reason: "Completed",
      message: "Job completed",
      object: { kind: "Job", namespace: "ops", name: "backup-nightly-29341" },
    },
  ];
}
