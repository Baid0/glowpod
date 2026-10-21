import type { KubeEvent, Overview, Session } from "../lib/types";

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
    problems: [],
    problemCount: 0,
    nodes: [
      { name: "ip-10-0-1-21", ready: true, cpu: 0.5, memory: 0.72, hot: false },
      { name: "ip-10-0-2-14", ready: true, cpu: 0.88, memory: 0.93, hot: true },
      { name: "ip-10-0-3-31", ready: true, cpu: 0.41, memory: 0.7, hot: false },
    ],
    topConsumers: [],
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
    recentDeploys: [],
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
