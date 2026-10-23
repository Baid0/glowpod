import type {
  AppDetail,
  ClusterInfo,
  KubeEvent,
  Overview,
  PodDetail,
  PodSummary,
  Problem,
  ResourceTable,
  SearchHit,
  SearchResult,
  Session,
} from "../lib/types";

export const session: Session = {
  mode: "local",
  method: "kubeconfig",
  csrfToken: "test-csrf",
  user: { username: "kubernetes-admin" },
};

export const clusters: ClusterInfo[] = [
  { id: "default", name: "prod-eu-1", server: "192.0.2.10:6443", current: true },
];

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
      nodesReady: 3,
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

/** A Table page like the API server returns for pods, with -o wide columns. */
export function podTable(
  opts: { names?: string[]; next?: string; remaining?: number } = {},
): ResourceTable {
  const names = opts.names ?? [
    "payments-api-7d9f6b-x2kq9",
    "checkout-web-6f4b9-m8zr2",
    "search-indexer-0",
  ];
  const status = ["CrashLoopBackOff", "Running", "Pending"];
  return {
    type: {
      group: "",
      version: "v1",
      resource: "pods",
      kind: "Pod",
      namespaced: true,
      verbs: ["get", "list", "watch"],
    },
    columns: [
      { name: "Name", type: "string", format: "name", priority: 0 },
      { name: "Ready", type: "string", priority: 0 },
      { name: "Status", type: "string", priority: 0 },
      { name: "Restarts", type: "integer", priority: 0 },
      { name: "Age", type: "string", priority: 0 },
      { name: "Node", type: "string", priority: 1 },
    ],
    rows: names.map((name, i) => ({
      cells: [name, i === 0 ? "0/1" : "1/1", status[i % 3], i === 0 ? 14 : i, `${i + 1}h`, `w${i}`],
      object: {
        name,
        namespace: ["payments", "checkout", "search"][i % 3],
        uid: `uid-${name}`,
        creationTimestamp: new Date(Date.now() - (i + 1) * 3_600_000).toISOString(),
      },
    })),
    ...(opts.next ? { continue: opts.next } : {}),
    ...(opts.remaining ? { remainingItemCount: opts.remaining } : {}),
  };
}

/** A crash-looping pod, like the design board's payments-api. */
export function podDetail(): PodDetail {
  const ago = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
  return {
    pod: {
      namespace: "payments",
      name: "payments-api-7d9f6b-x2kq9",
      phase: "Running",
      health: "failing",
      ready: "0/1",
      restarts: 14,
      node: "ip-10-0-1-21",
      createdAt: ago(60),
      problem: problems()[0],
    },
    createdAt: ago(60),
    ip: "10.244.1.7",
    qosClass: "Burstable",
    owner: {
      group: "apps",
      version: "v1",
      resource: "deployments",
      kind: "Deployment",
      name: "payments-api",
    },
    containers: [
      {
        name: "app",
        role: "app",
        image: "ghcr.io/acme/payments:v3",
        state: "waiting",
        reason: "CrashLoopBackOff",
        message: "back-off 5m0s restarting failed container",
        ready: false,
        restarts: 14,
        lastTermination: { reason: "Error", exitCode: 1, finishedAt: ago(2) },
        requests: { cpu: 0.25, memory: 256 * 1024 ** 2 },
        limits: { memory: 512 * 1024 ** 2 },
        usage: { cpu: 0.01, memory: 400 * 1024 ** 2 },
        ports: [{ name: "http", port: 8080, protocol: "TCP" }],
        probes: [{ kind: "readiness", action: "GET /ready on port http", periodSeconds: 10 }],
      },
      {
        name: "proxy",
        role: "sidecar",
        image: "envoyproxy/envoy:v1.36",
        state: "running",
        since: ago(58),
        ready: true,
        restarts: 0,
        requests: {},
        limits: {},
      },
    ],
    conditions: [
      { type: "PodScheduled", status: "True", since: ago(59) },
      {
        type: "Ready",
        status: "False",
        message: "containers with unready status: [app]",
        since: ago(40),
      },
    ],
    volumes: [
      { name: "config", kind: "configMap", source: "payments-config" },
      { name: "tmp", kind: "emptyDir" },
    ],
    events: [
      {
        ...event("ev1", "Warning", 1),
        message: "Back-off restarting failed container app",
        count: 14,
      },
      { ...event("ev2", "Normal", 57), reason: "Started", message: "Started container app" },
    ],
    story: [
      { time: ago(60), tone: "info", title: "Created by ReplicaSet payments-api-7d9f6b", count: 1 },
      { time: ago(59), tone: "info", title: "Scheduled on ip-10-0-1-21", count: 1 },
      {
        time: ago(20),
        tone: "critical",
        title: "Back-off restarting a failed container (BackOff)",
        detail: "Back-off restarting failed container app",
        count: 14,
      },
      {
        time: ago(2),
        tone: "critical",
        title: "Container app exited with code 1 (Error)",
        detail: "missing env DB_URL",
        count: 1,
      },
      {
        time: ago(0),
        tone: "critical",
        title: "Container app keeps restarting (CrashLoopBackOff)",
        count: 1,
      },
    ],
    metricsAvailable: true,
  };
}

/** The payments app: one crash-looping pod of three. */
export function appDetail(): AppDetail {
  const ago = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
  const pod = (name: string, extra: Partial<PodSummary> = {}): PodSummary => ({
    namespace: "payments",
    name,
    phase: "Running",
    health: "running",
    ready: "1/1",
    restarts: 0,
    node: "w1",
    createdAt: ago(60),
    ...extra,
  });
  const crash = problems()[0];
  return {
    app: {
      name: "payments",
      namespace: "payments",
      workloads: 2,
      ready: 2,
      total: 3,
      status: "critical",
      lastDeploy: ago(20),
    },
    workloads: [
      {
        kind: "Deployment",
        group: "apps",
        version: "v1",
        resource: "deployments",
        name: "payments-api",
        ready: 1,
        total: 2,
        status: "critical",
        images: ["ghcr.io/acme/payments:v3"],
      },
      {
        kind: "StatefulSet",
        group: "apps",
        version: "v1",
        resource: "statefulsets",
        name: "payments-db",
        ready: 1,
        total: 1,
        status: "ok",
        images: ["postgres:17"],
      },
    ],
    pods: [
      pod("payments-api-7d9f6b-ok"),
      pod("payments-api-7d9f6b-x2kq9", {
        health: "failing",
        ready: "0/1",
        restarts: 14,
        ...(crash ? { problem: crash } : {}),
      }),
      pod("payments-db-0", { createdAt: ago(3000) }),
    ],
    problems: crash ? [crash] : [],
    deploys: [
      {
        namespace: "payments",
        name: "payments-api",
        revision: "3",
        image: "ghcr.io/acme/payments:v3",
        at: ago(20),
      },
    ],
  };
}

const searchable: SearchHit[] = [
  {
    group: "",
    version: "v1",
    resource: "pods",
    kind: "Pod",
    namespace: "payments",
    name: "payments-api-7d9f6b-x2kq9",
  },
  {
    group: "apps",
    version: "v1",
    resource: "deployments",
    kind: "Deployment",
    namespace: "payments",
    name: "payments-api",
  },
  {
    group: "",
    version: "v1",
    resource: "services",
    kind: "Service",
    namespace: "payments",
    name: "payments-api",
  },
  {
    group: "",
    version: "v1",
    resource: "secrets",
    kind: "Secret",
    namespace: "payments",
    name: "payments-db",
  },
  { group: "", version: "v1", resource: "nodes", kind: "Node", name: "ip-10-0-2-14" },
];

/** GET /search: the fixture objects whose name or namespace has every word. */
export function search(query: string): SearchResult {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  return {
    hits: searchable.filter((h) =>
      words.every((w) => `${h.name} ${h.namespace ?? ""}`.toLowerCase().includes(w)),
    ),
  };
}
