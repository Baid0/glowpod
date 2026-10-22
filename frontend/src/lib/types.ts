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
  createdAt: string;
  problem?: Problem;
}

export type StreamMessage =
  | { type: "ready" }
  | { type: "pod"; op: "added" | "modified" | "deleted"; pod: PodSummary }
  | { type: "event"; op: "added" | "modified"; event: KubeEvent }
  | { type: "resync"; resource: "pods" | "events" }
  | { type: "error"; resource: "pods" | "events"; code: string; message: string };

/** A resource type from discovery. group "" is the core group. */
export interface ResourceType {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
  verbs: string[];
  shortNames?: string[];
  categories?: string[];
}

export interface TableColumn {
  name: string;
  type: string;
  format?: string;
  description?: string;
  /** 0 = what `kubectl get` shows; higher = `-o wide` extras. */
  priority: number;
}

export interface TableRow {
  cells: unknown[];
  object: {
    name: string;
    namespace?: string;
    uid: string;
    creationTimestamp?: string;
    labels?: Record<string, string>;
  };
}

export interface ResourceTable {
  type: ResourceType;
  columns: TableColumn[];
  rows: TableRow[];
  continue?: string;
  resourceVersion?: string;
  remainingItemCount?: number;
}

/** Group/version/resource as they appear in URLs ("core" for the core group). */
export interface ResourceRef {
  group: string;
  version: string;
  resource: string;
}

export interface WatchMessage {
  type: "ready" | "changed" | "resync" | "error";
  op?: "added" | "modified" | "deleted";
  ref?: { kind: string; namespace?: string; name: string };
  code?: string;
  message?: string;
}

export interface Resources {
  cpu?: number;
  memory?: number;
}

export interface PodContainer {
  name: string;
  role: "init" | "sidecar" | "app";
  image: string;
  state: "running" | "waiting" | "terminated" | "unknown";
  reason?: string;
  message?: string;
  exitCode?: number;
  since?: string;
  ready: boolean;
  restarts: number;
  lastTermination?: { reason?: string; exitCode: number; finishedAt: string };
  requests: Resources;
  limits: Resources;
  usage?: Resources;
  ports?: { name?: string; port: number; protocol: string }[];
  probes?: { kind: string; action: string; periodSeconds: number }[];
}

export interface StoryItem {
  time: string;
  tone: "ok" | "info" | "high" | "critical";
  title: string;
  detail?: string;
  count: number;
}

export interface PodDetail {
  pod: PodSummary;
  createdAt: string;
  ip?: string;
  qosClass?: string;
  owner?: { group: string; version: string; resource: string; kind: string; name: string };
  containers: PodContainer[];
  conditions: { type: string; status: string; reason?: string; message?: string; since?: string }[];
  volumes: { name: string; kind: string; source?: string }[];
  events: KubeEvent[];
  story: StoryItem[];
  metricsAvailable: boolean;
}

/** GET .../resources/{group}/{version}/{resource}/{name}?format=yaml */
export interface ResourceObject {
  object: Record<string, unknown> & {
    kind?: string;
    metadata?: {
      name?: string;
      namespace?: string;
      creationTimestamp?: string;
      labels?: Record<string, string>;
    };
    data?: Record<string, string>;
  };
  yaml: string;
  /** Secret values were hidden. */
  redacted: boolean;
}

export type AppSummary = Overview["apps"][number];

export interface AppWorkload {
  kind: string;
  group: string;
  version: string;
  resource: string;
  name: string;
  ready: number;
  total: number;
  status: AppStatus;
  images: string[];
}

export interface Deploy {
  namespace: string;
  name: string;
  revision: string;
  image: string;
  at: string;
}

/** GET /apps/{namespace}/{name} */
export interface AppDetail {
  app: AppSummary;
  workloads: AppWorkload[];
  pods: PodSummary[];
  problems: Problem[];
  deploys: Deploy[];
}
