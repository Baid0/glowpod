// Shapes of the Go API's JSON. Keep in sync with backend/internal/{overview,summary,metrics,api}.

export interface ClusterInfo {
  /** Used in URLs: /c/<id>/ and /api/v1/clusters/<id>/. */
  id: string;
  /** The kubeconfig context, or the in-cluster name. */
  name: string;
  server: string;
  /** The cluster to open first. */
  current?: boolean;
  /** On a Hub, an imported cluster's agent: not connected yet, connected or gone. */
  state?: "pending" | "connected" | "disconnected";
  /** When state last changed. */
  since?: string;
}

/** One row of the Clusters page: the same numbers as the cluster's Overview. */
export interface ClusterSummary {
  cluster: ClusterInfo & { version: string; nodes: number; nodesReady: number; namespaces: number };
  gauges: { cpu: Gauge; memory: Gauge; pods: Gauge };
  health: { running: number; pending: number; failing: number };
  problemCount: number;
  /** The Overview's "Needs attention" rows (the first 50). */
  problems: Problem[];
  thresholds: [number, number];
  generatedAt: string;
}

export type SignInMethod = "kubeconfig" | "token" | "oidc" | "ldap" | "password";

export interface Session {
  /** Glowpod's version, e.g. 0.1.0. */
  version?: string;
  mode: "local" | "in-cluster";
  method: SignInMethod;
  /** Sent back in X-CSRF-Token on every write. */
  csrfToken: string;
  user: { username: string; groups?: string[] };
  /** Set on a Glowpod Hub. */
  hub?: HubSession;
}

export interface HubSession {
  role: "admin" | "user";
  /** admin signed in with the bootstrap password and must finish first start. */
  setupRequired?: boolean;
  /** For admins: the address agents connect to. */
  serverURL?: string;
}

/** A kubeconfig you downloaded from the Hub; its token acts as you. */
export interface KubeToken {
  id: string;
  user: string;
  groups?: string[];
  created: string;
  expires: string;
  lastUsed?: string;
}

/** GET /api/v1/hub/kubeconfig/tokens: your live kubeconfigs. */
export interface KubeTokenList {
  tokens: KubeToken[];
  ttlSeconds: number;
}

/** POST /api/v1/hub/clusters: how to install the agent. */
export interface ImportCommand {
  cluster: ClusterInfo;
  command: string;
  /** Only for an HTTPS Hub: the same, without checking its certificate. */
  insecureCommand?: string;
  expiresAt: string;
}

/** no-access: the signed-in user may not read what the gauge needs. */
export type GaugeState = "live" | "no-metrics" | "no-access" | "error";

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
  cluster: ClusterInfo & { version: string; nodes: number; nodesReady: number; namespaces: number };
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
  /** Subresources such as "scale", "exec" and "log". */
  subresources?: string[];
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
  /** Picked namespaces the user may not list; the others are shown. */
  skipped?: string[];
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

/** "May I verb this?" Group uses "core" or "" for the core group. */
export interface AccessCheck {
  verb: "get" | "list" | "watch" | "create" | "update" | "patch" | "delete";
  group: string;
  resource: string;
  subresource?: string;
  namespace?: string;
  name?: string;
}

/** PUT .../resources/...: a dry run has before/after; a save has the object. */
export interface UpdateResult {
  dryRun: boolean;
  before?: string;
  after?: string;
  object?: ResourceObject;
}

/** POST .../resources/{group}/{version}/{resource}: the new object, or what it would be. */
export interface CreateResult {
  dryRun: boolean;
  object: ResourceObject;
}

/** What happened to one object of an Apply (or would, on a dry run). */
export type ApplyOutcome = "created" | "configured" | "unchanged" | "applied" | "failed";

export interface ApplyItem {
  apiVersion?: string;
  kind?: string;
  namespace?: string;
  name?: string;
  /** URL form, for linking to the object. */
  group?: string;
  version?: string;
  resource?: string;
  result: ApplyOutcome;
  code?: string;
  message?: string;
  detail?: string;
}

/** POST .../apply */
export interface ApplyResult {
  dryRun: boolean;
  items: ApplyItem[];
}

export interface NodeCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  since: string;
  /** Bad news: not Ready, or under memory, disk or PID pressure. */
  problem: boolean;
}

export interface NodeDetail {
  name: string;
  ready: boolean;
  unschedulable: boolean;
  roles: string[];
  createdAt: string;
  conditions: NodeCondition[];
  taints: { key: string; value?: string; effect: string }[];
  addresses: { type: string; address: string }[];
  info: {
    kubeletVersion: string;
    osImage: string;
    kernelVersion: string;
    containerRuntime: string;
    architecture: string;
  };
  gauges: { cpu: Gauge; memory: Gauge; pods: Gauge };
  pods: PodSummary[];
  events: KubeEvent[];
  labels?: Record<string, string>;
}

export interface DrainPod {
  namespace: string;
  name: string;
  reason?: string;
  localData?: boolean;
}

export interface DrainPlan {
  evict: DrainPod[];
  skip: DrainPod[];
}

export interface DrainResult {
  evicted: DrainPod[];
  blocked: DrainPod[];
  failed: DrainPod[];
  skipped: DrainPod[];
}

export interface HelmRelease {
  name: string;
  namespace: string;
  revision: number;
  /** deployed, failed, pending-install, pending-upgrade, superseded, uninstalling… */
  status: string;
  chart: string;
  chartVersion: string;
  appVersion?: string;
  updated: string;
  description?: string;
}

export interface HelmReleaseDetail extends HelmRelease {
  history: HelmRelease[];
  objects: { apiVersion: string; kind: string; namespace?: string; name: string }[];
}

export type MapStatus = "ok" | "high" | "critical" | "neutral";

export interface MapNode {
  id: string;
  /** 0 entry (Ingress), 1 Services, 2 workloads, 3 ConfigMaps / Secrets / claims. */
  column: 0 | 1 | 2 | 3;
  kind: string;
  namespace: string;
  name: string;
  status: MapStatus;
  detail?: string;
  missing?: boolean;
}

export interface ResourceMap {
  namespace: string;
  nodes: MapNode[];
  edges: { from: string; to: string }[];
  truncated: boolean;
}

/** GET /api/v1/auth/methods: how people can sign in (server mode). */
export interface AuthMethod {
  type: "oidc" | "ldap" | "password" | "token";
  name?: string;
}

export interface NamespaceList {
  namespaces: string[];
  /** The user may not list namespaces; these are guesses and others can be typed. */
  restricted?: boolean;
}

/** A person or group as Kubernetes sees them, prefix included. */
export interface Subject {
  kind: "User" | "Group";
  name: string;
}

export type ClusterRoleChoice = "owner" | "read-only" | "custom";
export type ProjectRole = "owner" | "member" | "read-only";

/** Someone's access to the whole cluster. */
export interface Member extends Subject {
  /** Set for members Glowpod manages; absent for access made elsewhere. */
  role?: ClusterRoleChoice;
  clusterRole: string;
  binding: string;
}

/** GET .../members: Glowpod's members, and access made with kubectl or GitOps. */
export interface Members {
  members: Member[];
  others: Member[];
}

export interface ProjectMember extends Subject {
  role: ProjectRole;
  /** The project's namespaces where this member's binding is missing. */
  missing?: string[];
}

export interface Project {
  name: string;
  namespaces: string[];
  members: ProjectMember[];
}

/** GET .../projects. */
export interface Projects {
  projects: Project[];
  unassigned: string[];
}

/** A Glowpod account on a Hub. */
export interface HubUser {
  username: string;
  role: "admin" | "user";
  created: string;
}

export interface OIDCSettings {
  enabled: boolean;
  name: string;
  issuer: string;
  clientId: string;
  /** Write-only: empty keeps the kept one. */
  clientSecret?: string;
  clientSecretSet: boolean;
  scopes: string[];
  usernameClaim: string;
  groupsClaim: string;
}

export interface LDAPSettings {
  enabled: boolean;
  url: string;
  bindDN: string;
  /** Write-only: empty keeps the kept one. */
  bindPassword?: string;
  bindPasswordSet: boolean;
  userBase: string;
  userFilter: string;
  usernameAttribute: string;
  groupBase: string;
  groupFilter: string;
  groupAttribute: string;
  startTLS: boolean;
  ca: string;
}

/** GET /api/v1/hub/settings. */
export interface HubSettings {
  serverURL: string;
  auth: { oidc: OIDCSettings; ldap: LDAPSettings };
  /** Methods set with Helm values or flags, which the UI can't change. */
  fixed: { oidc: boolean; ldap: boolean };
  /** What to register with the SSO provider. */
  redirectURL: string;
}

export interface CheckResult {
  ok: boolean;
  message: string;
}

/** POST /api/v1/hub/restore: what came back. */
export interface RestoreResult {
  created: string;
  users: number;
  clusters: number;
}
