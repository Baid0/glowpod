// The Go API's answers, worked out from the demo cluster's objects: the Overview, Table output,
// pod and node pages, apps, Helm releases and the resource map.

import type {
  AppDetail,
  AppStatus,
  AppSummary,
  AppWorkload,
  Deploy,
  Gauge,
  HelmRelease,
  HelmReleaseDetail,
  KubeEvent,
  MapNode,
  NodeDetail,
  Overview,
  PodContainer,
  PodDetail,
  PodSummary,
  Problem,
  ResourceMap,
  ResourceObject,
  ResourceTable,
  ResourceType,
  StoryItem,
  TableColumn,
} from "../lib/types";
import { CLUSTER_NAME, KUBE_VERSION, WORKLOADS } from "./catalog";
import {
  controllerOf,
  DemoError,
  K,
  meta,
  matchesSelector,
  nameOf,
  namespaceOf,
  podRequests,
  resourceKey,
  revisionOf,
  type DemoCluster,
  type DemoEvent,
} from "./cluster";
import {
  child,
  drift,
  isObj,
  iso,
  labelsOf,
  list,
  memQuantity,
  quantity,
  str,
  text,
  type Obj,
} from "./util";
import { toYaml } from "./yaml";

const MINUTE = 60_000;

export const CLUSTER_INFO = { id: "default", name: CLUSTER_NAME, server: "demo.glowpod.dev:443" };

/** A namespace filter: "" is all of them, else a comma-separated list. */
export function inScope(ns: string, scope: string): boolean {
  if (!scope) return true;
  return scope.split(",").includes(ns);
}

/** Durations the way kubectl prints ages: 45s, 3m20s, 25m, 4h49m, 3d, 61d. */
export function age(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  const d = Math.floor(h / 24);
  if (s < 120) return `${String(s)}s`;
  if (m < 10) return s % 60 ? `${String(m)}m${String(s % 60)}s` : `${String(m)}m`;
  if (m < 180) return `${String(m)}m`;
  if (h < 8) return m % 60 ? `${String(h)}h${String(m % 60)}m` : `${String(h)}h`;
  if (h < 48) return `${String(h)}h`;
  if (d < 8) return h % 24 ? `${String(d)}d${String(h % 24)}h` : `${String(d)}d`;
  if (d < 730) return `${String(d)}d`;
  return `${String(Math.floor(d / 365))}y${String(d % 365)}d`;
}

const createdAt = (o: Obj) => Date.parse(String(meta(o).creationTimestamp));

// ---- Pods ----

function shorten(s: string): string {
  const line = s.split("\n")[0] ?? "";
  return line.length > 120 ? line.slice(0, 117) + "…" : line;
}

function cpuQuantity(cores: number): string {
  return cores >= 1 && Number.isInteger(cores)
    ? String(cores)
    : `${String(Math.round(cores * 1000))}m`;
}

function statusesOf(p: Obj): Obj[] {
  return list(child(p, "status").containerStatuses);
}

function condition(o: Obj, type: string): Obj | undefined {
  return list(child(o, "status").conditions).find((c) => c.type === type);
}

export function podProblem(p: Obj, now: number): Problem | undefined {
  const base = {
    kind: "Pod",
    namespace: namespaceOf(p),
    name: nameOf(p),
    since: String(meta(p).creationTimestamp),
  };
  for (const cs of statusesOf(p)) {
    const last =
      isObj(cs.lastState) && isObj(cs.lastState.terminated) ? cs.lastState.terminated : undefined;
    if (last?.reason === "OOMKilled" && now - Date.parse(String(last.finishedAt)) <= 60 * MINUTE) {
      const c = list(child(p, "spec").containers).find((x) => x.name === cs.name);
      const limit =
        isObj(c?.resources) && isObj(c.resources.limits)
          ? text(c.resources.limits.memory)
          : undefined;
      return {
        ...base,
        severity: "critical",
        reason: "OOMKilled",
        title: "Ran out of memory",
        detail: limit ? `hit its ${limit} memory limit` : "ran out of memory",
        hint: "Raise its memory limit, or find what uses the memory.",
        since: String(last.finishedAt),
      };
    }
    const waiting = isObj(cs.state) && isObj(cs.state.waiting) ? cs.state.waiting : undefined;
    if (waiting?.reason === "CrashLoopBackOff") {
      const n = Number(cs.restartCount ?? 0);
      return {
        ...base,
        severity: "critical",
        reason: "CrashLoopBackOff",
        title: "Keeps restarting",
        detail: `restarted ${String(n)} ${n === 1 ? "time" : "times"}${last ? ` · exit code ${String(last.exitCode)}` : ""}`,
        hint: "Check the logs from the last crash.",
        since: last ? String(last.finishedAt) : base.since,
      };
    }
  }
  const phase = child(p, "status").phase;
  if (phase === "Pending" && now - createdAt(p) >= 2 * MINUTE) {
    const sched = condition(p, "PodScheduled");
    if (sched?.status === "False") {
      const message = text(sched.message) ?? "";
      const m = /Insufficient (cpu|memory)/.exec(message);
      const req = podRequests(p.spec);
      const detail = m
        ? m[1] === "memory"
          ? `no node has ${memQuantity(req.memory)} memory free`
          : `no node has ${cpuQuantity(req.cpu)} CPU free`
        : shorten(message) || "no node fits it";
      return {
        ...base,
        severity: "high",
        reason: "Pending",
        title: "Waiting for a node",
        detail,
        hint: "Free up room, add a node, or lower its requests.",
      };
    }
    return {
      ...base,
      severity: "high",
      reason: "Pending",
      title: "Stuck starting",
      detail: `pending for ${age(now - createdAt(p))}`,
      hint: "Check its events for volume or image problems.",
    };
  }
  return undefined;
}

export function podSummary(p: Obj, now: number): PodSummary {
  const statuses = statusesOf(p);
  const containers = list(child(p, "spec").containers);
  const problem = podProblem(p, now);
  const phase = text(child(p, "status").phase) ?? "Pending";
  const health: PodSummary["health"] =
    problem?.severity === "critical" || phase === "Failed"
      ? "failing"
      : phase === "Running"
        ? "running"
        : phase === "Pending"
          ? "pending"
          : "done";
  const node = text(child(p, "spec").nodeName);
  return {
    namespace: namespaceOf(p),
    name: nameOf(p),
    phase,
    health,
    ready: `${String(statuses.filter((s) => s.ready === true).length)}/${String(containers.length)}`,
    restarts: statuses.reduce((n, s) => n + Number(s.restartCount ?? 0), 0),
    ...(node ? { node } : {}),
    createdAt: String(meta(p).creationTimestamp),
    ...(problem ? { problem } : {}),
  };
}

/** What kubectl shows in a pod's STATUS column. */
function podStatusWord(p: Obj): string {
  const status = child(p, "status");
  for (const cs of statusesOf(p)) {
    const w = isObj(cs.state) && isObj(cs.state.waiting) ? cs.state.waiting : undefined;
    if (w?.reason) return str(w.reason);
  }
  if (status.phase === "Succeeded") return "Completed";
  return text(status.phase) ?? "Unknown";
}

export function nodeProblems(n: Obj): Problem[] {
  const out: Problem[] = [];
  const words: Record<string, string> = {
    MemoryPressure: "memory",
    DiskPressure: "disk",
    PIDPressure: "process ids",
  };
  for (const c of list(child(n, "status").conditions)) {
    const type = String(c.type);
    const base = { kind: "Node", name: nameOf(n), since: String(c.lastTransitionTime) };
    if (type === "Ready" && c.status !== "True") {
      out.push({
        ...base,
        severity: "critical",
        reason: "NotReady",
        title: "Node isn't ready",
        detail: text(c.message) ?? "the kubelet stopped reporting",
        hint: "Check the node's kubelet and network.",
      });
    } else if (words[type] && c.status === "True") {
      out.push({
        ...base,
        severity: "high",
        reason: type,
        title: `Node is low on ${words[type] ?? ""}`,
        detail: shorten(text(c.message) ?? ""),
        hint: `Free up ${words[type] ?? ""} or move pods off the node.`,
      });
    }
  }
  return out;
}

function sortProblems(list: Problem[]): Problem[] {
  return list.sort((a, b) => {
    if (a.severity !== b.severity) return a.severity === "critical" ? -1 : 1;
    return Date.parse(b.since) - Date.parse(a.since);
  });
}

// ---- Workloads and apps ----

/** The top-level owner of a pod: its Deployment rather than its ReplicaSet. */
function topOwner(c: DemoCluster, p: Obj): { kind: string; name: string } | undefined {
  const owner = controllerOf(p);
  if (owner?.kind !== "ReplicaSet") return owner;
  const rs = c.find(K.replicasets, namespaceOf(p), owner.name);
  return (rs && controllerOf(rs)) ?? owner;
}

const replicasOf = (o: Obj) => {
  const r = child(o, "spec").replicas;
  return typeof r === "number" ? r : 1;
};

interface Workload extends AppWorkload {
  namespace: string;
  app: string;
  deployed: number;
}

function failingOwners(c: DemoCluster, now: number): Set<string> {
  const out = new Set<string>();
  for (const p of c.objects(K.pods)) {
    if (podProblem(p, now)?.severity !== "critical") continue;
    const o = topOwner(c, p);
    if (o) out.add(`${o.kind}/${namespaceOf(p)}/${o.name}`);
  }
  return out;
}

function appKey(o: Obj): string {
  const a = labelsOf(o, "metadata", "annotations");
  const l = labelsOf(o, "metadata", "labels");
  return (
    a["meta.helm.sh/release-name"] ??
    l["app.kubernetes.io/part-of"] ??
    l["app.kubernetes.io/name"] ??
    l["app.kubernetes.io/instance"] ??
    l.app ??
    l["k8s-app"] ??
    nameOf(o)
  );
}

function workloads(c: DemoCluster, scope: string, now: number): Workload[] {
  const failing = failingOwners(c, now);
  const out: Workload[] = [];
  const images = (o: Obj) =>
    list(child(child(child(o, "spec"), "template"), "spec").containers).map(
      (x) => text(x.image) ?? "",
    );
  for (const d of c.objects(K.deployments)) {
    if (!inScope(namespaceOf(d), scope)) continue;
    const st = child(d, "status");
    const want = replicasOf(d);
    const ready = Number(st.readyReplicas ?? 0);
    let status: AppStatus = "high";
    if (
      ready >= want &&
      Number(st.updatedReplicas ?? 0) >= want &&
      Number(st.availableReplicas ?? 0) >= want
    )
      status = "ok";
    else if (failing.has(`Deployment/${namespaceOf(d)}/${nameOf(d)}`)) status = "critical";
    else if (list(st.conditions).some((x) => x.type === "Progressing" && x.status === "False"))
      status = "critical";
    const newest = Math.max(0, ...c.replicaSetsOf(d).map(createdAt));
    out.push({
      kind: "Deployment",
      group: "apps",
      version: "v1",
      resource: "deployments",
      name: nameOf(d),
      ready,
      total: want,
      status,
      images: images(d),
      namespace: namespaceOf(d),
      app: appKey(d),
      deployed: newest || createdAt(d),
    });
  }
  for (const s of c.objects(K.statefulsets)) {
    if (!inScope(namespaceOf(s), scope)) continue;
    const ready = Number(child(s, "status").readyReplicas ?? 0);
    const want = replicasOf(s);
    out.push({
      kind: "StatefulSet",
      group: "apps",
      version: "v1",
      resource: "statefulsets",
      name: nameOf(s),
      ready,
      total: want,
      status:
        ready >= want
          ? "ok"
          : failing.has(`StatefulSet/${namespaceOf(s)}/${nameOf(s)}`)
            ? "critical"
            : "high",
      images: images(s),
      namespace: namespaceOf(s),
      app: appKey(s),
      deployed: createdAt(s),
    });
  }
  for (const ds of c.objects(K.daemonsets)) {
    if (!inScope(namespaceOf(ds), scope)) continue;
    const st = child(ds, "status");
    const ready = Number(st.numberReady ?? 0);
    const want = Number(st.desiredNumberScheduled ?? 0);
    out.push({
      kind: "DaemonSet",
      group: "apps",
      version: "v1",
      resource: "daemonsets",
      name: nameOf(ds),
      ready,
      total: want,
      status:
        ready >= want
          ? "ok"
          : failing.has(`DaemonSet/${namespaceOf(ds)}/${nameOf(ds)}`)
            ? "critical"
            : "high",
      images: images(ds),
      namespace: namespaceOf(ds),
      app: appKey(ds),
      deployed: createdAt(ds),
    });
  }
  return out;
}

const SEVERITY: Record<AppStatus, number> = { ok: 0, high: 1, critical: 2 };

export function apps(c: DemoCluster, scope: string): AppSummary[] {
  const now = c.now();
  const byKey = new Map<string, AppSummary & { last: number }>();
  for (const w of workloads(c, scope, now)) {
    const k = `${w.namespace}/${w.app}`;
    let a = byKey.get(k);
    if (!a) {
      a = {
        name: w.app,
        namespace: w.namespace,
        workloads: 0,
        ready: 0,
        total: 0,
        status: "ok",
        last: 0,
      };
      byKey.set(k, a);
    }
    a.workloads++;
    a.ready += w.ready;
    a.total += w.total;
    if (SEVERITY[w.status] > SEVERITY[a.status]) a.status = w.status;
    a.last = Math.max(a.last, w.deployed);
  }
  return [...byKey.values()]
    .map(({ last, ...a }) => ({ ...a, lastDeploy: iso(last) }))
    .sort(
      (a, b) =>
        SEVERITY[b.status] - SEVERITY[a.status] ||
        a.namespace.localeCompare(b.namespace) ||
        a.name.localeCompare(b.name),
    );
}

function deploys(
  c: DemoCluster,
  filter: (d: { namespace: string; name: string }) => boolean,
  n: number,
): Deploy[] {
  const out: Deploy[] = [];
  for (const rs of c.objects(K.replicasets)) {
    const owner = controllerOf(rs);
    if (owner?.kind !== "Deployment" || !filter({ namespace: namespaceOf(rs), name: owner.name }))
      continue;
    out.push({
      namespace: namespaceOf(rs),
      name: owner.name,
      revision: String(revisionOf(rs)),
      image:
        text(list(child(child(child(rs, "spec"), "template"), "spec").containers)[0]?.image) ?? "",
      at: String(meta(rs).creationTimestamp),
    });
  }
  return out.sort((a, b) => Date.parse(b.at) - Date.parse(a.at)).slice(0, n);
}

export function appDetail(c: DemoCluster, namespace: string, name: string): AppDetail {
  const now = c.now();
  const summary = apps(c, namespace).find((a) => a.name === name);
  if (!summary)
    throw new DemoError(404, "not_found", `There's no app called ${name} in ${namespace}.`);
  const ws = workloads(c, namespace, now).filter((w) => w.app === name);
  const owned = new Set(ws.map((w) => `${w.kind}/${w.name}`));
  const pods = c
    .objects(K.pods)
    .filter((p) => namespaceOf(p) === namespace)
    .filter((p) => {
      const o = topOwner(c, p);
      return o !== undefined && owned.has(`${o.kind}/${o.name}`);
    })
    .map((p) => podSummary(p, now));
  return {
    app: summary,
    workloads: ws.map((w) => ({
      kind: w.kind,
      group: w.group,
      version: w.version,
      resource: w.resource,
      name: w.name,
      ready: w.ready,
      total: w.total,
      status: w.status,
      images: w.images,
    })),
    pods,
    problems: sortProblems(pods.flatMap((p) => (p.problem ? [p.problem] : []))),
    deploys: deploys(c, (d) => d.namespace === namespace && owned.has(`Deployment/${d.name}`), 10),
  };
}

// ---- Overview ----

/** A metric's history: where it was an hour ago, for the trend. */
function trend(seed: number, now: number, scale: number): number {
  return Math.round((drift(seed, now) - drift(seed, now - 3_600_000)) * scale * 1000) / 1000;
}

export function clusterGauges(c: DemoCluster, now: number): Overview["gauges"] {
  let cpuMax = 0;
  let memMax = 0;
  let cpuUsed = 0;
  let memUsed = 0;
  let podMax = 0;
  for (const n of c.objects(K.nodes)) {
    const alloc = child(child(n, "status"), "allocatable");
    const u = c.nodeUsage(nameOf(n), now);
    cpuMax += quantity(alloc.cpu);
    memMax += quantity(alloc.memory);
    cpuUsed += u.cpu * quantity(alloc.cpu);
    memUsed += u.memory * quantity(alloc.memory);
    podMax += quantity(alloc.pods);
  }
  let cpuReq = 0;
  let memReq = 0;
  let running = 0;
  for (const p of c.objects(K.pods)) {
    const phase = child(p, "status").phase;
    if (phase === "Running") running++;
    if (!child(p, "spec").nodeName || phase === "Succeeded" || phase === "Failed") continue;
    const r = podRequests(p.spec);
    cpuReq += r.cpu;
    memReq += r.memory;
  }
  let storageMax = 0;
  let storageUsed = 0;
  let volumes = 0;
  for (const pvc of c.objects(K.pvcs)) {
    if (child(pvc, "status").phase !== "Bound") continue;
    const size = quantity(labelsOf(pvc, "status", "capacity").storage);
    volumes++;
    storageMax += size;
    // Databases fill up their disks; caches don't.
    const share = nameOf(pvc).includes("prometheus")
      ? 0.71
      : nameOf(pvc).includes("db")
        ? 0.58
        : 0.34;
    storageUsed += size * (share + drift(volumes, now) * 0.004);
  }
  const nodes = c.objects(K.nodes).length;
  const peak = 131e6;
  return {
    cpu: {
      state: "live",
      value: cpuUsed,
      max: cpuMax,
      unit: "cores",
      requested: cpuReq,
      trend: trend(1, now, 0.02),
      trendWindow: 3600,
      nodes,
    },
    memory: {
      state: "live",
      value: memUsed,
      max: memMax,
      unit: "bytes",
      requested: memReq,
      trend: trend(2, now, 0.01),
      trendWindow: 3600,
      nodes,
    },
    pods: {
      state: "live",
      value: running,
      max: podMax,
      unit: "pods",
      nodes,
      perNodeMax: podMax / Math.max(1, nodes),
    },
    storage: {
      state: "live",
      value: storageUsed,
      max: storageMax,
      unit: "bytes",
      volumes,
      trend: 0.002,
      trendWindow: 3600,
    },
    network: {
      state: "live",
      value: peak * (0.37 + drift(5, now) * 0.06),
      max: peak,
      unit: "bytes/s",
      source: "prometheus",
      trend: trend(5, now, 0.06),
      trendWindow: 3600,
    },
  };
}

export function overview(c: DemoCluster, scope: string): Overview {
  const now = c.now();
  const pods = c.objects(K.pods).filter((p) => inScope(namespaceOf(p), scope));
  const health = { running: 0, pending: 0, failing: 0 };
  const problems: Problem[] = [];
  for (const p of pods) {
    const s = podSummary(p, now);
    if (s.health === "running") health.running++;
    else if (s.health === "pending") health.pending++;
    else if (s.health === "failing") health.failing++;
    if (s.problem) problems.push(s.problem);
  }
  if (!scope) for (const n of c.objects(K.nodes)) problems.push(...nodeProblems(n));
  sortProblems(problems);

  const ws = workloads(c, scope, now);
  const count = (kind: string) => {
    const these = ws.filter((w) => w.kind === kind);
    return {
      total: these.length,
      ready: these.filter((w) => w.status === "ok").length,
      pending: these.filter((w) => w.status === "high").length,
      failing: these.filter((w) => w.status === "critical").length,
    };
  };
  const jobs = c.objects(K.jobs).filter((j) => inScope(namespaceOf(j), scope));

  const consumers = pods
    .filter((p) => child(p, "status").phase === "Running")
    .map((p) => {
      const u = podUsage(p, now);
      return { namespace: namespaceOf(p), name: nameOf(p), cpu: u.cpu, memory: u.memory };
    })
    .sort((a, b) => b.cpu - a.cpu || b.memory - a.memory)
    .slice(0, 5);

  return {
    cluster: {
      ...CLUSTER_INFO,
      version: KUBE_VERSION,
      nodes: c.objects(K.nodes).length,
      namespaces: c.objects(K.namespaces).length,
    },
    namespace: scope,
    generatedAt: iso(now),
    thresholds: [0.7, 0.85],
    gauges: clusterGauges(c, now),
    workloads: {
      deployments: count("Deployment"),
      statefulSets: count("StatefulSet"),
      daemonSets: count("DaemonSet"),
      jobs: {
        succeeded: jobs.filter((j) => Number(child(j, "status").succeeded ?? 0) > 0).length,
        failed: jobs.filter((j) => Number(child(j, "status").failed ?? 0) > 0).length,
        running: jobs.filter((j) => Number(child(j, "status").active ?? 0) > 0).length,
      },
    },
    health,
    problems: problems.slice(0, 8),
    problemCount: problems.length,
    nodes: c
      .objects(K.nodes)
      .map((n) => {
        const u = c.nodeUsage(nameOf(n), now);
        return {
          name: nameOf(n),
          ready: condition(n, "Ready")?.status === "True",
          cpu: u.cpu,
          memory: u.memory,
          hot: u.cpu >= 0.85 || u.memory >= 0.85,
        };
      })
      .sort((a, b) => a.name.localeCompare(b.name)),
    topConsumers: consumers,
    apps: apps(c, scope).slice(0, 24),
    recentDeploys: deploys(c, (d) => inScope(d.namespace, scope), 5),
  };
}

/** What a running pod uses right now: cores and bytes, moving a little over time. */
export function podUsage(p: Obj, now: number): { cpu: number; memory: number } {
  const phase = child(p, "status").phase;
  if (phase !== "Running") return { cpu: 0, memory: 0 };
  const seed = (Number.parseInt(String(meta(p).uid).slice(0, 6), 16) % 97) + 1;
  const crash = statusesOf(p).some((s) => isObj(s.state) && isObj(s.state.waiting));
  let cpu = 0;
  let memory = 0;
  for (const ctr of list(child(p, "spec").containers)) {
    const req = isObj(ctr.resources) && isObj(ctr.resources.requests) ? ctr.resources.requests : {};
    const share = USAGE_SHARE.get(text(ctr.image) ?? "") ?? [0.6, 0.55];
    cpu += quantity(req.cpu) * share[0] * (1 + drift(seed, now) * 0.18);
    memory += quantity(req.memory) * share[1] * (1 + drift(seed + 1, now) * 0.04);
  }
  if (crash) return { cpu: 0.001, memory: 6 * 1024 ** 2 };
  return { cpu: Math.max(0.001, cpu), memory };
}

/** Typical use as a share of requests, per image, from the catalog. */
const USAGE_SHARE = new Map<string, [number, number]>(
  WORKLOADS.flatMap((w) =>
    [w.image, w.previousImage ?? w.image].map((image): [string, [number, number]] => [
      image,
      [w.usage[0] / quantity(w.cpu), w.usage[1] / quantity(w.memory)],
    ]),
  ),
);

// ---- Events ----

export function kubeEvent(e: DemoEvent): KubeEvent {
  return {
    id: e.id,
    type: e.type,
    reason: e.reason,
    message: e.message,
    object: e.object,
    count: e.count,
    time: e.time,
  };
}

export function events(
  c: DemoCluster,
  scope: string,
  limit: number,
  kind?: string,
  name?: string,
): KubeEvent[] {
  return c.events
    .filter((e) => (e.object.namespace ? inScope(e.object.namespace, scope) : !scope || !!kind))
    .filter((e) => (kind ? e.object.kind === kind : true) && (name ? e.object.name === name : true))
    .sort((a, b) => Date.parse(b.time) - Date.parse(a.time))
    .slice(0, limit)
    .map(kubeEvent);
}

// ---- Pod and node pages ----

const RESOURCE_OF: Record<string, { group: string; resource: string }> = {
  Deployment: { group: "apps", resource: "deployments" },
  StatefulSet: { group: "apps", resource: "statefulsets" },
  DaemonSet: { group: "apps", resource: "daemonsets" },
  ReplicaSet: { group: "apps", resource: "replicasets" },
  Job: { group: "batch", resource: "jobs" },
  CronJob: { group: "batch", resource: "cronjobs" },
};

function resources(v: unknown): { cpu?: number; memory?: number } {
  if (!isObj(v)) return {};
  return {
    ...(v.cpu !== undefined ? { cpu: quantity(v.cpu) } : {}),
    ...(v.memory !== undefined ? { memory: quantity(v.memory) } : {}),
  };
}

function probeAction(p: Obj): string {
  if (isObj(p.httpGet)) return `GET ${str(p.httpGet.path, "/")} on port ${str(p.httpGet.port)}`;
  if (isObj(p.tcpSocket)) return `TCP on port ${str(p.tcpSocket.port)}`;
  if (isObj(p.exec))
    return `runs ${(Array.isArray(p.exec.command) ? p.exec.command.map((x) => str(x)) : []).join(" ") || "a command"}`;
  return "a check";
}

function story(p: Obj, evs: KubeEvent[], now: number): StoryItem[] {
  const items: StoryItem[] = [];
  const add = (time: string | undefined, tone: StoryItem["tone"], title: string, detail = "") => {
    if (!time) return;
    items.push({ time, tone, title, ...(detail ? { detail } : {}), count: 1 });
  };
  const owner = controllerOf(p);
  add(
    String(meta(p).creationTimestamp),
    "info",
    owner ? `Created by ${owner.kind} ${owner.name}` : "Created",
  );
  for (const c of list(child(p, "status").conditions)) {
    const at = text(c.lastTransitionTime);
    if (c.type === "PodScheduled" && c.status === "True")
      add(at, "info", `Scheduled on ${String(child(p, "spec").nodeName)}`);
    else if (c.type === "PodScheduled") add(at, "high", "Waiting for a node", text(c.message));
    else if (c.type === "Ready" && c.status === "True") add(at, "ok", "Ready to serve traffic");
    else if (c.type === "Ready" && child(p, "status").phase === "Running")
      add(at, "high", "Not ready", text(c.message));
  }
  const exit = (name: string, t: Obj) =>
    t.reason === "OOMKilled"
      ? `Container ${name} ran out of memory (OOMKilled)`
      : t.exitCode === 0
        ? `Container ${name} finished (exit code 0)`
        : `Container ${name} exited with code ${String(t.exitCode)} (${str(t.reason, "Error")})`;
  for (const s of statusesOf(p)) {
    const name = String(s.name);
    const last =
      isObj(s.lastState) && isObj(s.lastState.terminated) ? s.lastState.terminated : undefined;
    if (last)
      add(text(last.finishedAt), last.exitCode === 0 ? "info" : "critical", exit(name, last));
    const state = isObj(s.state) ? s.state : {};
    if (isObj(state.running))
      add(text(state.running.startedAt), "ok", `Container ${name} is running`);
    else if (isObj(state.terminated))
      add(
        text(state.terminated.finishedAt),
        state.terminated.exitCode === 0 ? "info" : "critical",
        exit(name, state.terminated),
      );
    else if (isObj(state.waiting) && state.waiting.reason === "CrashLoopBackOff")
      add(
        iso(now),
        "critical",
        `Container ${name} keeps restarting (CrashLoopBackOff)`,
        `Restarted ${String(s.restartCount)} times so far. Kubernetes waits longer between each try.`,
      );
  }
  for (const e of evs) {
    if (e.reason === "Scheduled" || e.reason === "Created" || e.reason === "Killing") continue;
    let tone: StoryItem["tone"] = e.type === "Warning" ? "high" : "info";
    let title = e.reason;
    if (e.reason === "Pulled") title = `Image ${/"([^"]+)"/.exec(e.message)?.[1] ?? ""} ready`;
    else if (e.reason === "Started") title = e.message.replace(/\.$/, "");
    else if (e.reason === "BackOff") {
      tone = "critical";
      title = "Back-off restarting a failed container (BackOff)";
    } else if (e.reason === "FailedScheduling")
      title = "Couldn't be placed on a node (FailedScheduling)";
    else if (e.reason === "Evicted") {
      tone = "critical";
      title = "Evicted (Evicted)";
    }
    items.push({ time: e.time, tone, title, detail: e.message, count: e.count });
  }
  items.sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
  const out: StoryItem[] = [];
  for (const it of items) {
    const prev = out.at(-1);
    if (prev?.title === it.title && prev.tone === it.tone) {
      prev.count += it.count;
      prev.time = it.time;
      if (it.detail) prev.detail = it.detail;
    } else out.push({ ...it });
  }
  return out;
}

export function podDetail(c: DemoCluster, namespace: string, name: string): PodDetail {
  const p = c.find(K.pods, namespace, name);
  if (!p)
    throw new DemoError(
      404,
      "not_found",
      `There's no pod called ${name} in ${namespace}. It may have been replaced.`,
    );
  const now = c.now();
  const statuses = statusesOf(p);
  const owner = topOwner(c, p);
  const ownerRes = owner ? RESOURCE_OF[owner.kind] : undefined;
  const evs = events(c, namespace, 50, "Pod", name);
  const usage = podUsage(p, now);
  const containers = list(child(p, "spec").containers);
  return {
    pod: podSummary(p, now),
    createdAt: String(meta(p).creationTimestamp),
    ...(text(child(p, "status").podIP) ? { ip: String(child(p, "status").podIP) } : {}),
    ...(text(child(p, "status").qosClass) ? { qosClass: String(child(p, "status").qosClass) } : {}),
    ...(owner && ownerRes
      ? { owner: { ...ownerRes, version: "v1", kind: owner.kind, name: owner.name } }
      : {}),
    containers: containers.map((ctr): PodContainer => {
      const s = statuses.find((x) => x.name === ctr.name) ?? {};
      const state = isObj(s.state) ? s.state : {};
      const last =
        isObj(s.lastState) && isObj(s.lastState.terminated) ? s.lastState.terminated : undefined;
      const res = isObj(ctr.resources) ? ctr.resources : {};
      const which = isObj(state.running)
        ? "running"
        : isObj(state.terminated)
          ? "terminated"
          : isObj(state.waiting)
            ? "waiting"
            : "unknown";
      const raw = state[which];
      const detail: Obj = isObj(raw) ? raw : {};
      const probes = (["readiness", "liveness", "startup"] as const).flatMap((kind) => {
        const pr = ctr[`${kind}Probe`];
        return isObj(pr)
          ? [{ kind, action: probeAction(pr), periodSeconds: Number(pr.periodSeconds ?? 10) }]
          : [];
      });
      const share = containers.length ? 1 / containers.length : 1;
      return {
        name: String(ctr.name),
        role: "app",
        image: String(ctr.image),
        state: statuses.length ? which : "waiting",
        ...(text(detail.reason) ? { reason: String(detail.reason) } : {}),
        ...(text(detail.message) ? { message: String(detail.message) } : {}),
        ...(typeof detail.exitCode === "number" ? { exitCode: detail.exitCode } : {}),
        ...(text(detail.startedAt) ? { since: String(detail.startedAt) } : {}),
        ready: s.ready === true,
        restarts: Number(s.restartCount ?? 0),
        ...(last
          ? {
              lastTermination: {
                reason: str(last.reason, ""),
                exitCode: Number(last.exitCode ?? 0),
                finishedAt: String(last.finishedAt),
              },
            }
          : {}),
        requests: resources(res.requests),
        limits: resources(res.limits),
        ...(which === "running"
          ? { usage: { cpu: usage.cpu * share, memory: usage.memory * share } }
          : {}),
        ...(Array.isArray(ctr.ports)
          ? {
              ports: list(ctr.ports).map((x) => ({
                ...(text(x.name) ? { name: String(x.name) } : {}),
                port: Number(x.containerPort),
                protocol: str(x.protocol, "TCP"),
              })),
            }
          : {}),
        ...(probes.length ? { probes } : {}),
      };
    }),
    conditions: list(child(p, "status").conditions).map((x) => ({
      type: String(x.type),
      status: String(x.status),
      ...(text(x.reason) ? { reason: String(x.reason) } : {}),
      ...(text(x.message) ? { message: String(x.message) } : {}),
      ...(text(x.lastTransitionTime) ? { since: String(x.lastTransitionTime) } : {}),
    })),
    volumes: [
      ...list(child(p, "spec").volumes).map((v) => {
        const kind = Object.keys(v).find((k) => k !== "name") ?? "unknown";
        const raw = v[kind];
        const src: Obj = isObj(raw) ? raw : {};
        const source = text(src.claimName) ?? text(src.name) ?? text(src.secretName);
        return { name: String(v.name), kind, ...(source ? { source } : {}) };
      }),
      { name: `kube-api-access-${String(meta(p).uid).slice(0, 5)}`, kind: "projected" },
    ],
    events: evs,
    story: story(p, evs, now),
    metricsAvailable: true,
  };
}

function nodeGauge(
  state: Gauge["state"],
  value: number,
  max: number,
  unit: Gauge["unit"],
  requested?: number,
): Gauge {
  return { state, value, max, unit, ...(requested !== undefined ? { requested } : {}) };
}

export function nodeDetail(c: DemoCluster, name: string): NodeDetail {
  const n = c.find(K.nodes, "", name);
  if (!n) throw new DemoError(404, "not_found", `There's no node called ${name}.`);
  const now = c.now();
  const status = child(n, "status");
  const alloc = child(status, "allocatable");
  const u = c.nodeUsage(name, now);
  const req = c.requestsOn(name);
  const pods = c.objects(K.pods).filter((p) => child(p, "spec").nodeName === name);
  const info = child(status, "nodeInfo");
  const problems = new Set(nodeProblems(n).map((p) => p.reason));
  return {
    name,
    ready: condition(n, "Ready")?.status === "True",
    unschedulable: child(n, "spec").unschedulable === true,
    roles: [],
    createdAt: String(meta(n).creationTimestamp),
    conditions: list(status.conditions).map((x) => ({
      type: String(x.type),
      status: String(x.status),
      ...(text(x.reason) ? { reason: String(x.reason) } : {}),
      ...(text(x.message) ? { message: String(x.message) } : {}),
      since: String(x.lastTransitionTime),
      problem: x.type === "Ready" ? x.status !== "True" : problems.has(String(x.type)),
    })),
    taints: list(child(n, "spec").taints).map((t) => ({
      key: String(t.key),
      ...(text(t.value) ? { value: String(t.value) } : {}),
      effect: String(t.effect),
    })),
    addresses: list(status.addresses).map((a) => ({
      type: String(a.type),
      address: String(a.address),
    })),
    info: {
      kubeletVersion: String(info.kubeletVersion),
      osImage: String(info.osImage),
      kernelVersion: String(info.kernelVersion),
      containerRuntime: String(info.containerRuntimeVersion),
      architecture: String(info.architecture),
    },
    gauges: {
      cpu: nodeGauge("live", u.cpu * quantity(alloc.cpu), quantity(alloc.cpu), "cores", req.cpu),
      memory: nodeGauge(
        "live",
        u.memory * quantity(alloc.memory),
        quantity(alloc.memory),
        "bytes",
        req.memory,
      ),
      pods: nodeGauge(
        "live",
        pods.filter((p) => child(p, "status").phase === "Running").length,
        quantity(alloc.pods),
        "pods",
      ),
    },
    pods: pods.map((p) => podSummary(p, now)),
    events: events(c, "", 50, "Node", name),
    labels: labelsOf(n, "metadata", "labels"),
  };
}

// ---- Helm ----

export function helmReleases(c: DemoCluster, scope: string): HelmRelease[] {
  return c
    .helmReleases()
    .filter((r) => inScope(r.namespace, scope))
    .map((r) => helmRevision(c, r.name, r.history.length))
    .filter((r): r is HelmRelease => r !== undefined)
    .sort((a, b) => a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name));
}

function helmRevision(c: DemoCluster, name: string, revision: number): HelmRelease | undefined {
  const r = c.helmReleases().find((x) => x.name === name);
  const h = r?.history[revision - 1];
  if (!r || !h) return undefined;
  return {
    name: r.name,
    namespace: r.namespace,
    revision,
    status: h.status,
    chart: r.chart,
    chartVersion:
      revision === r.history.length
        ? r.chartVersion
        : r.chartVersion.replace(/\d+(?=[^\d]*$)/, (d) =>
            String(Math.max(0, Number(d) - (r.history.length - revision))),
          ),
    appVersion: r.appVersion,
    updated: iso(c.started - h.daysAgo * 86_400_000),
    description: h.description,
  };
}

export function helmRelease(c: DemoCluster, namespace: string, name: string): HelmReleaseDetail {
  const r = c.helmReleases().find((x) => x.name === name && x.namespace === namespace);
  const current = r && helmRevision(c, name, r.history.length);
  if (!r || !current)
    throw new DemoError(
      404,
      "not_found",
      `There's no Helm release called ${name} in ${namespace}.`,
    );
  const objects: HelmReleaseDetail["objects"] = [];
  for (const [, objs] of c.store) {
    for (const o of objs) {
      if (labelsOf(o, "metadata", "annotations")["meta.helm.sh/release-name"] !== name) continue;
      objects.push({
        apiVersion: String(o.apiVersion),
        kind: String(o.kind),
        ...(namespaceOf(o) ? { namespace: namespaceOf(o) } : {}),
        name: nameOf(o),
      });
    }
  }
  objects.sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name));
  return {
    ...current,
    history: r.history
      .map((_, i) => helmRevision(c, name, i + 1))
      .filter((x): x is HelmRelease => x !== undefined)
      .reverse(),
    objects,
  };
}

// ---- Map ----

export function resourceMap(c: DemoCluster, namespace: string): ResourceMap {
  const now = c.now();
  const nodes = new Map<string, MapNode>();
  const edges: { from: string; to: string }[] = [];
  const add = (n: MapNode) => {
    if (!nodes.has(n.id)) nodes.set(n.id, n);
    return n.id;
  };
  const inNs = (key: string) => c.objects(key).filter((o) => namespaceOf(o) === namespace);
  const ws = workloads(c, namespace, now);
  const wlObjects = [...inNs(K.deployments), ...inNs(K.statefulsets), ...inNs(K.daemonsets)];
  for (const o of wlObjects) {
    const w = ws.find((x) => x.kind === o.kind && x.name === nameOf(o));
    const id = add({
      id: `${String(o.kind)}/${nameOf(o)}`,
      column: 2,
      kind: String(o.kind),
      namespace,
      name: nameOf(o),
      status: w?.status ?? "neutral",
      detail: `${String(w?.ready ?? 0)}/${String(w?.total ?? 0)} ready`,
    });
    const spec = child(child(child(o, "spec"), "template"), "spec");
    const uses = (kind: string, name: string, optional = false) => {
      const key = kind === "ConfigMap" ? K.configmaps : kind === "Secret" ? K.secrets : K.pvcs;
      const exists = !!c.find(key, namespace, name);
      const uid = add({
        id: `${kind}/${name}`,
        column: 3,
        kind,
        namespace,
        name,
        status: exists ? "neutral" : "critical",
        ...(exists ? {} : { detail: "doesn't exist", missing: !optional }),
      });
      edges.push({ from: id, to: uid });
    };
    for (const ctr of list(spec.containers)) {
      for (const e of list(ctr.envFrom)) {
        if (isObj(e.configMapRef)) uses("ConfigMap", String(e.configMapRef.name));
        if (isObj(e.secretRef)) uses("Secret", String(e.secretRef.name));
      }
      for (const e of list(ctr.env)) {
        const from = isObj(e.valueFrom) ? e.valueFrom : {};
        if (isObj(from.secretKeyRef)) uses("Secret", String(from.secretKeyRef.name));
        if (isObj(from.configMapKeyRef)) uses("ConfigMap", String(from.configMapKeyRef.name));
      }
    }
    for (const v of list(spec.volumes)) {
      if (isObj(v.configMap)) uses("ConfigMap", String(v.configMap.name));
      if (isObj(v.secret)) uses("Secret", String(v.secret.secretName));
      if (isObj(v.persistentVolumeClaim))
        uses("PersistentVolumeClaim", String(v.persistentVolumeClaim.claimName));
    }
    if (o.kind === "StatefulSet") {
      for (const t of list(child(o, "spec").volumeClaimTemplates)) {
        for (let i = 0; i < replicasOf(o); i++)
          uses("PersistentVolumeClaim", `${nameOf(t)}-${nameOf(o)}-${String(i)}`);
      }
    }
  }
  for (const s of inNs(K.services)) {
    const sel = labelsOf(s, "spec", "selector");
    const id = add({
      id: `Service/${nameOf(s)}`,
      column: 1,
      kind: "Service",
      namespace,
      name: nameOf(s),
      status: "neutral",
      detail: str(child(s, "spec").type, "ClusterIP"),
    });
    for (const o of wlObjects) {
      const labels = labelsOf(o, "spec", "template", "metadata", "labels");
      if (Object.keys(sel).length && Object.entries(sel).every(([k, v]) => labels[k] === v)) {
        edges.push({ from: id, to: `${String(o.kind)}/${nameOf(o)}` });
      }
    }
  }
  for (const ing of inNs("networking.k8s.io/ingresses")) {
    const hosts = list(child(ing, "spec").rules).map((r) => str(r.host, "*"));
    const id = add({
      id: `Ingress/${nameOf(ing)}`,
      column: 0,
      kind: "Ingress",
      namespace,
      name: nameOf(ing),
      status: "neutral",
      detail: hosts.join(", "),
    });
    for (const r of list(child(ing, "spec").rules)) {
      for (const p of list(isObj(r.http) ? r.http.paths : undefined)) {
        const svc =
          isObj(p.backend) && isObj(p.backend.service) ? String(p.backend.service.name) : "";
        if (!svc) continue;
        const sid = `Service/${svc}`;
        if (!nodes.has(sid))
          add({
            id: sid,
            column: 1,
            kind: "Service",
            namespace,
            name: svc,
            status: "critical",
            detail: "doesn't exist",
            missing: true,
          });
        edges.push({ from: id, to: sid });
      }
    }
  }
  const sorted = [...nodes.values()].sort(
    (a, b) => a.column - b.column || a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name),
  );
  return { namespace, nodes: sorted, edges, truncated: false };
}

// ---- Discovery and Table output ----

const BUILTIN: [string, string, string, boolean, string[], string[]?][] = [
  // group/version, resource, kind, namespaced, short names, subresources
  ["v1", "pods", "Pod", true, ["po"], ["log", "exec", "eviction", "status"]],
  ["v1", "services", "Service", true, ["svc"]],
  ["v1", "configmaps", "ConfigMap", true, ["cm"]],
  ["v1", "secrets", "Secret", true, []],
  ["v1", "namespaces", "Namespace", false, ["ns"]],
  ["v1", "nodes", "Node", false, ["no"], ["status"]],
  ["v1", "events", "Event", true, ["ev"]],
  ["v1", "persistentvolumeclaims", "PersistentVolumeClaim", true, ["pvc"]],
  ["v1", "persistentvolumes", "PersistentVolume", false, ["pv"]],
  ["v1", "serviceaccounts", "ServiceAccount", true, ["sa"]],
  ["v1", "endpoints", "Endpoints", true, ["ep"]],
  ["v1", "limitranges", "LimitRange", true, ["limits"]],
  ["v1", "resourcequotas", "ResourceQuota", true, ["quota"]],
  ["v1", "replicationcontrollers", "ReplicationController", true, ["rc"], ["scale"]],
  ["apps/v1", "deployments", "Deployment", true, ["deploy"], ["scale", "status"]],
  ["apps/v1", "statefulsets", "StatefulSet", true, ["sts"], ["scale", "status"]],
  ["apps/v1", "daemonsets", "DaemonSet", true, ["ds"], ["status"]],
  ["apps/v1", "replicasets", "ReplicaSet", true, ["rs"], ["scale", "status"]],
  ["apps/v1", "controllerrevisions", "ControllerRevision", true, []],
  ["batch/v1", "jobs", "Job", true, [], ["status"]],
  ["batch/v1", "cronjobs", "CronJob", true, ["cj"], ["status"]],
  ["autoscaling/v2", "horizontalpodautoscalers", "HorizontalPodAutoscaler", true, ["hpa"]],
  ["networking.k8s.io/v1", "ingresses", "Ingress", true, ["ing"]],
  ["networking.k8s.io/v1", "ingressclasses", "IngressClass", false, []],
  ["networking.k8s.io/v1", "networkpolicies", "NetworkPolicy", true, ["netpol"]],
  ["discovery.k8s.io/v1", "endpointslices", "EndpointSlice", true, []],
  ["storage.k8s.io/v1", "storageclasses", "StorageClass", false, ["sc"]],
  ["storage.k8s.io/v1", "csidrivers", "CSIDriver", false, []],
  ["storage.k8s.io/v1", "csinodes", "CSINode", false, []],
  ["storage.k8s.io/v1", "volumeattachments", "VolumeAttachment", false, []],
  ["policy/v1", "poddisruptionbudgets", "PodDisruptionBudget", true, ["pdb"]],
  ["rbac.authorization.k8s.io/v1", "roles", "Role", true, []],
  ["rbac.authorization.k8s.io/v1", "rolebindings", "RoleBinding", true, []],
  ["rbac.authorization.k8s.io/v1", "clusterroles", "ClusterRole", false, []],
  ["rbac.authorization.k8s.io/v1", "clusterrolebindings", "ClusterRoleBinding", false, []],
  ["scheduling.k8s.io/v1", "priorityclasses", "PriorityClass", false, ["pc"]],
  ["coordination.k8s.io/v1", "leases", "Lease", true, []],
  [
    "apiextensions.k8s.io/v1",
    "customresourcedefinitions",
    "CustomResourceDefinition",
    false,
    ["crd", "crds"],
  ],
  [
    "admissionregistration.k8s.io/v1",
    "validatingwebhookconfigurations",
    "ValidatingWebhookConfiguration",
    false,
    [],
  ],
  [
    "admissionregistration.k8s.io/v1",
    "mutatingwebhookconfigurations",
    "MutatingWebhookConfiguration",
    false,
    [],
  ],
];

const VERBS = ["create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"];

export function resourceTypes(c: DemoCluster): ResourceType[] {
  const out: ResourceType[] = BUILTIN.map(
    ([gv, resource, kind, namespaced, shortNames, subresources]) => {
      const [group, version] = gv.includes("/") ? gv.split("/") : ["", gv];
      return {
        group: group ?? "",
        version: version ?? "v1",
        resource,
        kind,
        namespaced,
        verbs: VERBS,
        ...(shortNames.length ? { shortNames } : {}),
        ...(subresources ? { subresources } : {}),
      };
    },
  );
  for (const crd of c.objects("apiextensions.k8s.io/customresourcedefinitions")) {
    const spec = child(crd, "spec");
    const names = child(spec, "names");
    const short = Array.isArray(names.shortNames) ? names.shortNames.map(String) : [];
    out.push({
      group: String(spec.group),
      version: text(list(spec.versions)[0]?.name) ?? "v1",
      resource: String(names.plural),
      kind: String(names.kind),
      namespaced: spec.scope === "Namespaced",
      verbs: VERBS,
      ...(short.length ? { shortNames: short } : {}),
      categories: ["all"],
    });
  }
  return out;
}

export function typeFor(c: DemoCluster, group: string, resource: string): ResourceType {
  const g = group === "core" ? "" : group;
  const t = resourceTypes(c).find((x) => x.group === g && x.resource === resource);
  if (!t)
    throw new DemoError(
      404,
      "not_found",
      `This cluster doesn't serve ${resource}${g ? "." + g : ""}.`,
    );
  return t;
}

type Cells = (o: Obj, c: DemoCluster, now: number) => unknown[];

const col = (name: string, type = "string", priority = 0, format?: string): TableColumn => ({
  name,
  type,
  priority,
  ...(format ? { format } : {}),
});
const NAME = col("Name", "string", 0, "name");
const AGE = col("Age", "string", 0, "date");

const containersOf = (o: Obj) =>
  list(child(child(child(o, "spec"), "template"), "spec").containers);
const names = (o: Obj) =>
  containersOf(o)
    .map((x) => String(x.name))
    .join(",");
const imagesOf = (o: Obj) =>
  containersOf(o)
    .map((x) => String(x.image))
    .join(",");
const selectorText = (o: Obj) =>
  Object.entries(labelsOf(o, "spec", "selector", "matchLabels"))
    .map(([k, v]) => `${k}=${v}`)
    .join(",");
const ageOf = (o: Obj, now: number) => age(now - createdAt(o));

const TABLES: Record<string, { columns: TableColumn[]; cells: Cells }> = {
  [K.pods]: {
    columns: [
      NAME,
      col("Ready"),
      col("Status"),
      col("Restarts"),
      AGE,
      col("IP", "string", 1),
      col("Node", "string", 1),
      col("Nominated Node", "string", 1),
      col("Readiness Gates", "string", 1),
    ],
    cells: (p, _c, now) => {
      const st = statusesOf(p);
      const restarts = st.reduce((n, s) => n + Number(s.restartCount ?? 0), 0);
      const last = st
        .map((s) =>
          isObj(s.lastState) && isObj(s.lastState.terminated)
            ? Date.parse(String(s.lastState.terminated.finishedAt))
            : 0,
        )
        .reduce((a, b) => Math.max(a, b), 0);
      return [
        nameOf(p),
        `${String(st.filter((s) => s.ready === true).length)}/${String(list(child(p, "spec").containers).length)}`,
        podStatusWord(p),
        restarts && last ? `${String(restarts)} (${age(now - last)} ago)` : String(restarts),
        ageOf(p, now),
        text(child(p, "status").podIP) ?? "<none>",
        text(child(p, "spec").nodeName) ?? "<none>",
        "<none>",
        "<none>",
      ];
    },
  },
  [K.deployments]: {
    columns: [
      NAME,
      col("Ready"),
      col("Up-to-date", "integer"),
      col("Available", "integer"),
      AGE,
      col("Containers", "string", 1),
      col("Images", "string", 1),
      col("Selector", "string", 1),
    ],
    cells: (d, _c, now) => {
      const s = child(d, "status");
      return [
        nameOf(d),
        `${str(s.readyReplicas ?? 0)}/${String(replicasOf(d))}`,
        Number(s.updatedReplicas ?? 0),
        Number(s.availableReplicas ?? 0),
        ageOf(d, now),
        names(d),
        imagesOf(d),
        selectorText(d),
      ];
    },
  },
  [K.replicasets]: {
    columns: [
      NAME,
      col("Desired", "integer"),
      col("Current", "integer"),
      col("Ready", "integer"),
      AGE,
      col("Containers", "string", 1),
      col("Images", "string", 1),
      col("Selector", "string", 1),
    ],
    cells: (rs, _c, now) => {
      const s = child(rs, "status");
      return [
        nameOf(rs),
        replicasOf(rs),
        Number(s.replicas ?? 0),
        Number(s.readyReplicas ?? 0),
        ageOf(rs, now),
        names(rs),
        imagesOf(rs),
        selectorText(rs),
      ];
    },
  },
  [K.statefulsets]: {
    columns: [NAME, col("Ready"), AGE, col("Containers", "string", 1), col("Images", "string", 1)],
    cells: (s, _c, now) => [
      nameOf(s),
      `${str(child(s, "status").readyReplicas ?? 0)}/${String(replicasOf(s))}`,
      ageOf(s, now),
      names(s),
      imagesOf(s),
    ],
  },
  [K.daemonsets]: {
    columns: [
      NAME,
      col("Desired", "integer"),
      col("Current", "integer"),
      col("Ready", "integer"),
      col("Up-to-date", "integer"),
      col("Available", "integer"),
      col("Node Selector"),
      AGE,
      col("Containers", "string", 1),
      col("Images", "string", 1),
      col("Selector", "string", 1),
    ],
    cells: (ds, _c, now) => {
      const s = child(ds, "status");
      return [
        nameOf(ds),
        Number(s.desiredNumberScheduled ?? 0),
        Number(s.currentNumberScheduled ?? 0),
        Number(s.numberReady ?? 0),
        Number(s.updatedNumberScheduled ?? 0),
        Number(s.numberAvailable ?? 0),
        "<none>",
        ageOf(ds, now),
        names(ds),
        imagesOf(ds),
        selectorText(ds),
      ];
    },
  },
  [K.jobs]: {
    columns: [
      NAME,
      col("Status"),
      col("Completions"),
      col("Duration"),
      AGE,
      col("Containers", "string", 1),
      col("Images", "string", 1),
      col("Selector", "string", 1),
    ],
    cells: (j, _c, now) => {
      const s = child(j, "status");
      const start = Date.parse(String(s.startTime ?? meta(j).creationTimestamp));
      const end = text(s.completionTime) ? Date.parse(String(s.completionTime)) : now;
      return [
        nameOf(j),
        s.completionTime ? "Complete" : "Running",
        `${str(s.succeeded ?? 0)}/1`,
        age(end - start),
        ageOf(j, now),
        names(j),
        imagesOf(j),
        `batch.kubernetes.io/controller-uid=${String(meta(j).uid)}`,
      ];
    },
  },
  [K.cronjobs]: {
    columns: [
      NAME,
      col("Schedule"),
      col("Timezone"),
      col("Suspend", "boolean"),
      col("Active", "integer"),
      col("Last Schedule"),
      AGE,
      col("Containers", "string", 1),
      col("Images", "string", 1),
      col("Selector", "string", 1),
    ],
    cells: (cj, _c, now) => {
      const spec = child(cj, "spec");
      const s = child(cj, "status");
      const tpl = child(child(child(spec, "jobTemplate"), "spec"), "template");
      const ctrs = list(child(tpl, "spec").containers);
      return [
        nameOf(cj),
        String(spec.schedule),
        "<none>",
        spec.suspend === true,
        list(s.active).length,
        text(s.lastScheduleTime) ? age(now - Date.parse(String(s.lastScheduleTime))) : "<none>",
        ageOf(cj, now),
        ctrs.map((x) => String(x.name)).join(","),
        ctrs.map((x) => String(x.image)).join(","),
        "<none>",
      ];
    },
  },
  [K.services]: {
    columns: [
      NAME,
      col("Type"),
      col("Cluster-IP"),
      col("External-IP"),
      col("Port(s)"),
      AGE,
      col("Selector", "string", 1),
    ],
    cells: (s, _c, now) => {
      const spec = child(s, "spec");
      const ext = list(child(child(s, "status"), "loadBalancer").ingress).map((i) =>
        String(i.hostname ?? i.ip),
      );
      const ports = list(spec.ports).map(
        (p) =>
          `${String(p.port)}${p.nodePort ? `:${str(p.nodePort)}` : ""}/${str(p.protocol, "TCP")}`,
      );
      const sel = Object.entries(labelsOf(s, "spec", "selector"))
        .map(([k, v]) => `${k}=${v}`)
        .join(",");
      return [
        nameOf(s),
        str(spec.type, "ClusterIP"),
        str(spec.clusterIP, "None"),
        ext.length ? ext.join(",") : spec.type === "LoadBalancer" ? "<pending>" : "<none>",
        ports.join(","),
        ageOf(s, now),
        sel || "<none>",
      ];
    },
  },
  "networking.k8s.io/ingresses": {
    columns: [NAME, col("Class"), col("Hosts"), col("Address"), col("Ports"), AGE],
    cells: (i, _c, now) => {
      const spec = child(i, "spec");
      const hosts = list(spec.rules).map((r) => str(r.host, "*"));
      const addr = list(child(child(i, "status"), "loadBalancer").ingress).map((x) =>
        String(x.hostname ?? x.ip),
      );
      return [
        nameOf(i),
        str(spec.ingressClassName, "<none>"),
        hosts.join(","),
        addr.join(","),
        list(spec.tls).length ? "80, 443" : "80",
        ageOf(i, now),
      ];
    },
  },
  "networking.k8s.io/networkpolicies": {
    columns: [NAME, col("Pod-Selector"), AGE],
    cells: (n, _c, now) => {
      const sel = Object.entries(labelsOf(n, "spec", "podSelector", "matchLabels"))
        .map(([k, v]) => `${k}=${v}`)
        .join(",");
      return [nameOf(n), sel || "<none>", ageOf(n, now)];
    },
  },
  "networking.k8s.io/ingressclasses": {
    columns: [NAME, col("Controller"), col("Parameters"), AGE],
    cells: (o, _c, now) => [
      nameOf(o),
      String(child(o, "spec").controller),
      "<none>",
      ageOf(o, now),
    ],
  },
  [K.pvcs]: {
    columns: [
      NAME,
      col("Status"),
      col("Volume"),
      col("Capacity"),
      col("Access Modes"),
      col("StorageClass"),
      col("VolumeAttributesClass"),
      AGE,
      col("VolumeMode", "string", 1),
    ],
    cells: (p, _c, now) => {
      const spec = child(p, "spec");
      const st = child(p, "status");
      const bound = st.phase === "Bound";
      return [
        nameOf(p),
        str(st.phase, "Pending"),
        bound ? str(spec.volumeName, "") : "",
        bound ? (labelsOf(p, "status", "capacity").storage ?? "") : "",
        bound ? "RWO" : "",
        str(spec.storageClassName, ""),
        "<unset>",
        ageOf(p, now),
        str(spec.volumeMode, "Filesystem"),
      ];
    },
  },
  [K.pvs]: {
    columns: [
      NAME,
      col("Capacity"),
      col("Access Modes"),
      col("Reclaim Policy"),
      col("Status"),
      col("Claim"),
      col("StorageClass"),
      col("VolumeAttributesClass"),
      col("Reason"),
      AGE,
      col("VolumeMode", "string", 1),
    ],
    cells: (p, _c, now) => {
      const spec = child(p, "spec");
      const claim = child(spec, "claimRef");
      return [
        nameOf(p),
        labelsOf(p, "spec", "capacity").storage ?? "",
        "RWO",
        str(spec.persistentVolumeReclaimPolicy, "Delete"),
        str(child(p, "status").phase, "Available"),
        claim.name ? `${String(claim.namespace)}/${str(claim.name)}` : "",
        str(spec.storageClassName, ""),
        "<unset>",
        "",
        ageOf(p, now),
        "Filesystem",
      ];
    },
  },
  "storage.k8s.io/storageclasses": {
    columns: [
      NAME,
      col("Provisioner"),
      col("ReclaimPolicy"),
      col("VolumeBindingMode"),
      col("AllowVolumeExpansion", "boolean"),
      AGE,
    ],
    cells: (s, _c, now) => [
      nameOf(s),
      String(s.provisioner),
      str(s.reclaimPolicy, "Delete"),
      str(s.volumeBindingMode, "Immediate"),
      s.allowVolumeExpansion === true,
      ageOf(s, now),
    ],
  },
  [K.configmaps]: {
    columns: [NAME, col("Data", "integer"), AGE],
    cells: (o, _c, now) => [nameOf(o), Object.keys(child(o, "data")).length, ageOf(o, now)],
  },
  [K.secrets]: {
    columns: [NAME, col("Type"), col("Data", "integer"), AGE],
    cells: (o, _c, now) => [
      nameOf(o),
      str(o.type, "Opaque"),
      Object.keys(child(o, "data")).length,
      ageOf(o, now),
    ],
  },
  [K.nodes]: {
    columns: [
      NAME,
      col("Status"),
      col("Roles"),
      AGE,
      col("Version"),
      col("Internal-IP", "string", 1),
      col("External-IP", "string", 1),
      col("OS-Image", "string", 1),
      col("Kernel-Version", "string", 1),
      col("Container-Runtime", "string", 1),
    ],
    cells: (n, _c, now) => {
      const ready = condition(n, "Ready")?.status === "True" ? "Ready" : "NotReady";
      const info = child(child(n, "status"), "nodeInfo");
      const ipOf = (t: string) =>
        text(list(child(n, "status").addresses).find((a) => a.type === t)?.address) ?? "<none>";
      return [
        nameOf(n),
        child(n, "spec").unschedulable === true ? `${ready},SchedulingDisabled` : ready,
        "<none>",
        ageOf(n, now),
        String(info.kubeletVersion),
        ipOf("InternalIP"),
        ipOf("ExternalIP"),
        String(info.osImage),
        String(info.kernelVersion),
        String(info.containerRuntimeVersion),
      ];
    },
  },
  [K.namespaces]: {
    columns: [NAME, col("Status"), AGE],
    cells: (o, _c, now) => [nameOf(o), str(child(o, "status").phase, "Active"), ageOf(o, now)],
  },
  "/serviceaccounts": {
    columns: [NAME, col("Secrets", "integer"), AGE],
    cells: (o, _c, now) => [nameOf(o), 0, ageOf(o, now)],
  },
  "policy/poddisruptionbudgets": {
    columns: [
      NAME,
      col("Min Available"),
      col("Max Unavailable"),
      col("Allowed Disruptions", "integer"),
      AGE,
    ],
    cells: (o, _c, now) => [
      nameOf(o),
      str(child(o, "spec").minAvailable, "N/A"),
      str(child(o, "spec").maxUnavailable, "N/A"),
      Number(child(o, "status").disruptionsAllowed ?? 0),
      ageOf(o, now),
    ],
  },
  "apiextensions.k8s.io/customresourcedefinitions": {
    columns: [NAME, col("Created At", "date")],
    cells: (o) => [nameOf(o), String(meta(o).creationTimestamp)],
  },
  "cert-manager.io/certificates": {
    columns: [
      NAME,
      col("Ready"),
      col("Secret"),
      col("Issuer", "string", 1),
      col("Status", "string", 1),
      AGE,
    ],
    cells: (o, _c, now) => {
      const ready = condition(o, "Ready");
      return [
        nameOf(o),
        str(ready?.status, "Unknown"),
        String(child(o, "spec").secretName),
        String(child(child(o, "spec"), "issuerRef").name),
        str(ready?.message, ""),
        ageOf(o, now),
      ];
    },
  },
  "cert-manager.io/clusterissuers": {
    columns: [NAME, col("Ready"), col("Status", "string", 1), AGE],
    cells: (o, _c, now) => {
      const ready = condition(o, "Ready");
      return [nameOf(o), str(ready?.status, "Unknown"), str(ready?.message, ""), ageOf(o, now)];
    },
  },
};

function eventRows(
  c: DemoCluster,
  scope: string,
  now: number,
): { cells: unknown[]; object: Obj }[] {
  return c.events
    .filter((e) => !e.object.namespace || inScope(e.object.namespace, scope))
    .map((e) => ({
      cells: [
        age(now - Date.parse(e.time)),
        e.type,
        e.reason,
        `${e.object.kind.toLowerCase()}/${e.object.name}`,
        "",
        e.source,
        e.message,
        age(now - Date.parse(e.firstTime)),
        e.count,
        e.id,
      ],
      object: {
        metadata: {
          name: e.id,
          namespace: e.object.namespace ?? "default",
          uid: e.id,
          creationTimestamp: e.firstTime,
        },
      },
    }));
}

export interface ListQuery {
  namespace: string;
  labelSelector: string;
  fieldSelector: string;
  limit: number;
  continue: string;
}

function fieldMatches(o: Obj, selector: string): boolean {
  for (const term of selector.split(",").filter(Boolean)) {
    const [path = "", want = ""] = term.split("=");
    const v =
      path === "metadata.name"
        ? nameOf(o)
        : path === "metadata.namespace"
          ? namespaceOf(o)
          : path === "spec.nodeName"
            ? child(o, "spec").nodeName
            : path === "status.phase"
              ? child(o, "status").phase
              : undefined;
    if (str(v, "") !== want) return false;
  }
  return true;
}

/** The objects a list or watch covers. */
export function selectObjects(
  c: DemoCluster,
  key: string,
  q: Pick<ListQuery, "namespace" | "labelSelector" | "fieldSelector">,
): Obj[] {
  return c
    .objects(key)
    .filter((o) => !namespaceOf(o) || inScope(namespaceOf(o), q.namespace))
    .filter(
      (o) =>
        !q.labelSelector || matchesSelector(labelsOf(o, "metadata", "labels"), q.labelSelector),
    )
    .filter((o) => !q.fieldSelector || fieldMatches(o, q.fieldSelector));
}

export function table(
  c: DemoCluster,
  group: string,
  resource: string,
  q: ListQuery,
): ResourceTable {
  const type = typeFor(c, group, resource);
  const now = c.now();
  const key = resourceKey(group, resource);
  let rows: ResourceTable["rows"];
  let columns: TableColumn[];
  if (key === K.events) {
    columns = [
      col("Last Seen"),
      col("Type"),
      col("Reason"),
      col("Object"),
      col("Subobject", "string", 1),
      col("Source", "string", 1),
      col("Message"),
      col("First Seen", "string", 1),
      col("Count", "integer", 1),
      col("Name", "string", 1, "name"),
    ];
    rows = eventRows(c, q.namespace, now).map((r) => ({
      cells: r.cells,
      object: {
        name: nameOf(r.object),
        namespace: namespaceOf(r.object),
        uid: String(meta(r.object).uid),
        creationTimestamp: String(meta(r.object).creationTimestamp),
      },
    }));
  } else {
    const t = TABLES[key] ?? {
      columns: [NAME, AGE],
      cells: (o: Obj, _c: DemoCluster, n: number) => [nameOf(o), ageOf(o, n)],
    };
    columns = t.columns;
    const objs = selectObjects(c, key, q).sort(
      (a, b) => namespaceOf(a).localeCompare(namespaceOf(b)) || nameOf(a).localeCompare(nameOf(b)),
    );
    rows = objs.map((o) => {
      const labels = labelsOf(o, "metadata", "labels");
      return {
        cells: t.cells(o, c, now),
        object: {
          name: nameOf(o),
          ...(namespaceOf(o) ? { namespace: namespaceOf(o) } : {}),
          uid: String(meta(o).uid),
          creationTimestamp: String(meta(o).creationTimestamp),
          ...(Object.keys(labels).length ? { labels } : {}),
        },
      };
    });
  }
  const start = Number(q.continue || 0);
  const limit = q.limit > 0 ? q.limit : rows.length;
  const page = rows.slice(start, start + limit);
  const rest = rows.length - start - page.length;
  return {
    type,
    columns,
    rows: page,
    resourceVersion: String(c.objects(key).length + c.events.length),
    ...(rest > 0 ? { continue: String(start + limit), remainingItemCount: rest } : {}),
  };
}

/** One object as the API returns it: Secret values hidden unless revealed. */
export function objectView(
  c: DemoCluster,
  group: string,
  resource: string,
  namespace: string,
  name: string,
  reveal: boolean,
): ResourceObject {
  const type = typeFor(c, group, resource);
  const key = resourceKey(group, resource);
  const o = c.find(key, type.namespaced ? namespace : "", name);
  if (!o) {
    throw new DemoError(
      404,
      "not_found",
      `There's no ${type.kind} called ${name}${type.namespaced ? ` in ${namespace}` : ""}. It may have been deleted.`,
    );
  }
  const copy = structuredClone(o);
  const secret = key === K.secrets && !reveal;
  if (secret) {
    for (const f of ["data", "stringData"]) {
      const m = copy[f];
      if (isObj(m)) for (const k of Object.keys(m)) m[k] = "";
    }
  }
  return { object: copy, yaml: toYaml(copy), redacted: secret };
}
