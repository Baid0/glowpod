// Shapes of the Go API's JSON. Keep in sync with backend/internal/{overview,summary,metrics,api}.

export interface ClusterInfo {
  id: string;
  name: string;
  server: string;
}

export interface Session {
  mode: "local" | "in-cluster";
  user: { username: string };
  clusters: ClusterInfo[];
}

export type GaugeState = "live" | "no-metrics" | "error";

export interface Gauge {
  state: GaugeState;
  value: number;
  max: number;
  unit: "cores" | "bytes" | "pods" | "bytes/s";
  requested?: number;
  /** Change in value/max over trendWindow seconds. */
  trend?: number;
  trendWindow?: number;
  message?: string;
  nodes?: number;
  perNodeMax?: number;
  volumes?: number;
  source?: string;
}

export type Severity = "critical" | "high";

export interface Problem {
  severity: Severity;
  reason: string;
  title: string;
  detail: string;
  hint: string;
  kind: string;
  namespace?: string;
  name: string;
  since: string;
}

export interface WorkloadCount {
  total: number;
  ready: number;
  pending: number;
  failing: number;
}

export type AppStatus = "ok" | "high" | "critical";

export interface Overview {
  cluster: ClusterInfo & { version: string; nodes: number; namespaces: number };
  namespace: string;
  generatedAt: string;
  thresholds: [number, number];
  gauges: {
    cpu: Gauge;
    memory: Gauge;
    pods: Gauge;
    storage: Gauge;
    network?: Gauge;
  };
  workloads: {
    deployments: WorkloadCount;
    statefulSets: WorkloadCount;
    daemonSets: WorkloadCount;
    jobs: { succeeded: number; failed: number; running: number };
  };
  health: { running: number; pending: number; failing: number };
  problems: Problem[];
  problemCount: number;
  nodes: { name: string; ready: boolean; cpu?: number; memory?: number; hot: boolean }[];
  topConsumers: { namespace: string; name: string; cpu: number; memory: number }[];
  apps: {
    name: string;
    namespace: string;
    workloads: number;
    ready: number;
    total: number;
    status: AppStatus;
    lastDeploy?: string;
  }[];
  recentDeploys: { namespace: string; name: string; revision: string; image: string; at: string }[];
  unavailable?: { section: string; message: string }[];
}

export interface KubeEvent {
  id: string;
  type: "Normal" | "Warning";
  reason: string;
  message: string;
  object: { kind: string; namespace?: string; name: string };
  count: number;
  time: string;
}

export interface PodSummary {
  namespace: string;
  name: string;
  phase: string;
  health: "running" | "pending" | "failing" | "done";
  ready: string;
  restarts: number;
  node?: string;
  problem?: Problem;
}

export type StreamMessage =
  | { type: "ready" }
  | { type: "pod"; op: "added" | "modified" | "deleted"; pod: PodSummary }
  | { type: "event"; op: "added" | "modified"; event: KubeEvent }
  | { type: "resync"; resource: "pods" | "events" }
  | { type: "error"; resource: "pods" | "events"; code: string; message: string };
