// The demo's pretend cluster: Kubernetes objects in memory, plus just enough of the controllers
// (Deployments, StatefulSets, DaemonSets, the scheduler, the kubelet and a CronJob) that scaling,
// restarting, deleting, editing, cordoning and draining behave the way people expect.

import {
  BROKEN_IMAGES,
  HELM_RELEASES,
  KUBE_VERSION,
  NAMESPACES,
  NODE_ALLOCATABLE,
  NODE_CAPACITY,
  NODES,
  WORKLOADS,
  type WorkloadSpec,
} from "./catalog";
import { extraObjects } from "./objects";
import {
  child,
  isObj,
  iso,
  labelsOf,
  list,
  quantity,
  Random,
  str,
  templateHash,
  text,
  type Obj,
} from "./util";

const MINUTE = 60_000;
const DAY = 24 * 60 * MINUTE;

/** How long a new container takes to start. */
const START_MS = 2_500;
/** A crash-looping container is retried this often (Kubernetes backs off to 5 minutes). */
const CRASH_EVERY_MS = 150_000;
/** How often the scheduler retries pods that didn't fit. */
const RESCHEDULE_MS = 30_000;
/** The demo CronJob runs this often. */
export const CRON_EVERY_MS = 5 * MINUTE;
const CRON_RUN_MS = 20_000;

export interface Clock {
  now(): number;
}

/** Group "" (or "core") and a plural name, e.g. "apps/deployments" or "/pods". */
export function resourceKey(group: string, resource: string): string {
  return `${group === "core" ? "" : group}/${resource}`;
}

export const K = {
  pods: "/pods",
  nodes: "/nodes",
  namespaces: "/namespaces",
  services: "/services",
  configmaps: "/configmaps",
  secrets: "/secrets",
  pvcs: "/persistentvolumeclaims",
  pvs: "/persistentvolumes",
  events: "/events",
  deployments: "apps/deployments",
  replicasets: "apps/replicasets",
  statefulsets: "apps/statefulsets",
  daemonsets: "apps/daemonsets",
  jobs: "batch/jobs",
  cronjobs: "batch/cronjobs",
} as const;

export interface DemoEvent {
  id: string;
  type: "Normal" | "Warning";
  reason: string;
  message: string;
  object: { kind: string; namespace?: string; name: string };
  count: number;
  time: string;
  firstTime: string;
  source: string;
}

export type Change =
  | {
      type: "object";
      key: string;
      op: "added" | "modified" | "deleted";
      kind: string;
      namespace?: string;
      name: string;
      object: Obj;
    }
  | { type: "event"; op: "added" | "modified"; event: DemoEvent };

/** An error the fake API answers with, like the Go server's. */
export class DemoError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly detail?: string,
  ) {
    super(message);
    this.name = "DemoError";
  }
}

export const nameOf = (o: Obj) => text(child(o, "metadata").name) ?? "";
export const namespaceOf = (o: Obj) => text(child(o, "metadata").namespace) ?? "";
export const meta = (o: Obj) => child(o, "metadata");

/** A ReplicaSet's or Deployment's rollout revision. */
export const revisionOf = (o: Obj) =>
  Number(labelsOf(o, "metadata", "annotations")["deployment.kubernetes.io/revision"] ?? 0);

type PodState = "unschedulable" | "creating" | "running" | "crashloop" | "succeeded";

interface PodSim {
  state: PodState;
  /** When the next step happens: started, retried after a crash, or finished. */
  due?: number;
  /** Jobs: when the run finishes. */
  finishAt?: number;
}

function ownerRef(o: Obj): Obj {
  return {
    apiVersion: o.apiVersion,
    kind: o.kind,
    name: nameOf(o),
    uid: meta(o).uid,
    controller: true,
    blockOwnerDeletion: true,
  };
}

export function controllerOf(o: Obj): { kind: string; name: string } | undefined {
  const ref = list(meta(o).ownerReferences).find((r) => r.controller === true);
  if (!ref) return undefined;
  return { kind: text(ref.kind) ?? "", name: text(ref.name) ?? "" };
}

/** Requests of all containers in a pod spec: cores and bytes. */
export function podRequests(spec: unknown): { cpu: number; memory: number } {
  let cpu = 0;
  let memory = 0;
  for (const c of list(isObj(spec) ? spec.containers : undefined)) {
    const req = isObj(c.resources) && isObj(c.resources.requests) ? c.resources.requests : {};
    cpu += quantity(req.cpu);
    memory += quantity(req.memory);
  }
  return { cpu, memory };
}

/** label=value,other!=x,present,!absent */
export function matchesSelector(labels: Record<string, string>, selector: string): boolean {
  for (const raw of selector.split(",")) {
    const term = raw.trim();
    if (!term) continue;
    const neq = /^([^!=]+)!=(.*)$/.exec(term);
    if (neq) {
      if (labels[(neq[1] ?? "").trim()] === (neq[2] ?? "").trim()) return false;
      continue;
    }
    const eq = /^([^!=]+)==?(.*)$/.exec(term);
    if (eq) {
      if (labels[(eq[1] ?? "").trim()] !== (eq[2] ?? "").trim()) return false;
      continue;
    }
    if (term.startsWith("!")) {
      if (term.slice(1) in labels) return false;
      continue;
    }
    if (!(term in labels)) return false;
  }
  return true;
}

function matchLabels(selector: unknown): Record<string, string> {
  return isObj(selector) ? labelsOf(selector, "matchLabels") : {};
}

export class DemoCluster {
  readonly store = new Map<string, Obj[]>();
  readonly events: DemoEvent[] = [];
  readonly sim = new Map<string, PodSim>();
  readonly started: number;
  private rv = 4_820_000;
  private uidCounter = 0;
  private ipCounter = 0;
  private eventCounter = 0;
  private listeners = new Set<(c: Change) => void>();
  private lastSchedule = 0;
  private lastCron = 0;
  private batch: Change[] | null = null;
  /** When each Deployment's latest rollout started, by uid. */
  private rollouts = new Map<string, number>();
  /** True while the starting state is built: what happens then is history, not news. */
  private seeding = true;
  /** Memory and CPU requests per node at the start, to see how load moved since. */
  private baseline = new Map<string, { cpu: number; memory: number }>();
  readonly random = new Random(20261022);

  constructor(readonly clock: Clock) {
    this.started = clock.now();
    this.seed();
  }

  now(): number {
    return this.clock.now();
  }

  subscribe(fn: (c: Change) => void): () => void {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  }

  private emit(c: Change) {
    if (this.batch) this.batch.push(c);
    else for (const fn of this.listeners) fn(c);
  }

  /** Runs fn and sends the changes it made afterwards, once the state is consistent. */
  private transaction<T>(fn: () => T): T {
    if (this.batch) return fn();
    this.batch = [];
    try {
      return fn();
    } finally {
      const changes = this.batch;
      this.batch = null;
      for (const c of changes) for (const l of this.listeners) l(c);
    }
  }

  uid(): string {
    this.uidCounter++;
    const h = (n: number, len: number) => n.toString(16).padStart(len, "0").slice(-len);
    const r = () => this.random.int(0xffff);
    return `${h(r(), 4)}${h(r(), 4)}-${h(r(), 4)}-4${h(r(), 3)}-a${h(r(), 3)}-${h(this.uidCounter, 4)}${h(r(), 4)}${h(r(), 4)}`;
  }

  objects(key: string): Obj[] {
    let objs = this.store.get(key);
    if (!objs) {
      objs = [];
      this.store.set(key, objs);
    }
    return objs;
  }

  find(key: string, namespace: string, name: string): Obj | undefined {
    return this.objects(key).find(
      (o) => nameOf(o) === name && (namespace === "" || namespaceOf(o) === namespace),
    );
  }

  /** Adds or replaces an object, stamping a new resourceVersion. */
  put(key: string, obj: Obj): Obj {
    const objs = this.objects(key);
    const m = meta(obj);
    const i = objs.findIndex((o) => nameOf(o) === m.name && namespaceOf(o) === namespaceOf(obj));
    // Objects made since the last announcement have no resourceVersion yet.
    const op = i >= 0 && m.resourceVersion ? "modified" : "added";
    this.rv++;
    m.resourceVersion = String(this.rv);
    if (i >= 0) objs[i] = obj;
    else objs.push(obj);
    this.emit({
      type: "object",
      key,
      op,
      kind: text(obj.kind) ?? "",
      ...(namespaceOf(obj) ? { namespace: namespaceOf(obj) } : {}),
      name: nameOf(obj),
      object: obj,
    });
    return obj;
  }

  remove(key: string, obj: Obj) {
    const objs = this.objects(key);
    const i = objs.indexOf(obj);
    if (i < 0) return;
    objs.splice(i, 1);
    this.rv++;
    if (key === K.pods) this.sim.delete(String(meta(obj).uid));
    this.emit({
      type: "object",
      key,
      op: "deleted",
      kind: text(obj.kind) ?? "",
      ...(namespaceOf(obj) ? { namespace: namespaceOf(obj) } : {}),
      name: nameOf(obj),
      object: obj,
    });
  }

  /** Records an event, or counts a repeat of one seen in the last hour. */
  record(
    type: DemoEvent["type"],
    reason: string,
    object: DemoEvent["object"],
    message: string,
    source = "kubelet",
    at = this.now(),
  ): DemoEvent {
    const same = this.events.find(
      (e) =>
        e.reason === reason &&
        e.message === message &&
        e.object.kind === object.kind &&
        e.object.name === object.name &&
        e.object.namespace === object.namespace &&
        at - Date.parse(e.time) < 60 * MINUTE,
    );
    if (same) {
      same.count++;
      same.time = iso(at);
      this.emit({ type: "event", op: "modified", event: { ...same } });
      return same;
    }
    this.eventCounter++;
    const event: DemoEvent = {
      id: `${object.name}.${(0x18a3f0000 + this.eventCounter * 7919).toString(16)}`,
      type,
      reason,
      message,
      object,
      count: 1,
      time: iso(at),
      firstTime: iso(at),
      source,
    };
    this.events.unshift(event);
    if (this.events.length > 500) this.events.length = 500;
    this.emit({ type: "event", op: "added", event: { ...event } });
    return event;
  }

  private ref(o: Obj): DemoEvent["object"] {
    const ns = namespaceOf(o);
    return { kind: text(o.kind) ?? "", ...(ns ? { namespace: ns } : {}), name: nameOf(o) };
  }

  // ---- Initial state ----

  private seed() {
    const t0 = this.started;
    const ago = (ms: number) => iso(t0 - ms);

    for (const ns of NAMESPACES) {
      const age =
        ns === "glowpod"
          ? 3 * DAY
          : ns.startsWith("kube") || ns === "default"
            ? 120 * DAY
            : 60 * DAY;
      this.objects(K.namespaces).push({
        apiVersion: "v1",
        kind: "Namespace",
        metadata: {
          name: ns,
          uid: this.uid(),
          creationTimestamp: ago(age),
          labels: { "kubernetes.io/metadata.name": ns },
          resourceVersion: "",
        },
        spec: { finalizers: ["kubernetes"] },
        status: { phase: "Active" },
      });
    }

    for (const n of NODES) {
      this.objects(K.nodes).push({
        apiVersion: "v1",
        kind: "Node",
        metadata: {
          name: n.name,
          uid: this.uid(),
          creationTimestamp: ago(n.ageDays * DAY),
          labels: {
            "kubernetes.io/hostname": n.name,
            "kubernetes.io/os": "linux",
            "kubernetes.io/arch": "amd64",
            "node.kubernetes.io/instance-type": "m6i.xlarge",
            "topology.kubernetes.io/region": "eu-west-1",
            "topology.kubernetes.io/zone": n.zone,
            "eks.amazonaws.com/nodegroup": "general",
            "eks.amazonaws.com/capacityType": "ON_DEMAND",
          },
          resourceVersion: "",
        },
        spec: { providerID: `aws:///${n.zone}/i-0${n.ip.replace(/\./g, "")}a7c` },
        status: {
          capacity: { ...NODE_CAPACITY, "ephemeral-storage": "83873772Ki" },
          allocatable: { ...NODE_ALLOCATABLE, "ephemeral-storage": "76224326324" },
          conditions: [],
          addresses: [
            { type: "InternalIP", address: n.ip },
            { type: "Hostname", address: `${n.name}.eu-west-1.compute.internal` },
            { type: "InternalDNS", address: `${n.name}.eu-west-1.compute.internal` },
          ],
          nodeInfo: {
            kubeletVersion: `${KUBE_VERSION}-eks-113cf36`,
            kubeProxyVersion: `${KUBE_VERSION}-eks-113cf36`,
            osImage: "Amazon Linux 2023.9.20251014",
            kernelVersion: "6.12.46-66.121.amzn2023.x86_64",
            containerRuntimeVersion: "containerd://2.1.4",
            architecture: "amd64",
            operatingSystem: "linux",
          },
        },
      });
    }
    this.updateNodeConditions(true);

    for (const w of WORKLOADS) this.seedWorkload(w);
    for (const [key, objs] of extraObjects(this, t0)) this.objects(key).push(...objs);
    this.seedCronJob();

    // Place what's already running, biggest first, then the pods from the last hour in order,
    // the way the cluster got here.
    const pods = this.objects(K.pods);
    const recent = (p: Obj) => t0 - Date.parse(String(meta(p).creationTimestamp)) < 60 * MINUTE;
    const order = [...pods].sort((a, b) => {
      const ra = recent(a);
      const rb = recent(b);
      if (ra !== rb) return ra ? 1 : -1;
      if (ra)
        return String(meta(a).creationTimestamp).localeCompare(String(meta(b).creationTimestamp));
      return podRequests(b.spec).memory - podRequests(a.spec).memory;
    });
    for (const p of order) {
      if (!isObj(p.spec) || p.spec.nodeName) continue;
      const node = this.pickNode(p);
      if (node) this.bind(p, node);
    }
    for (const n of this.objects(K.nodes)) this.baseline.set(nameOf(n), this.requestsOn(nameOf(n)));

    for (const p of pods) this.seedPodState(p);
    this.reconcileAll(true);
    this.seedHistory();
    this.lastSchedule = t0;
    this.lastCron = Math.floor(t0 / CRON_EVERY_MS) * CRON_EVERY_MS;
    for (const objs of this.store.values()) {
      for (const o of objs) {
        this.rv += 1 + this.random.int(400);
        meta(o).resourceVersion = String(this.rv);
      }
    }
    this.seeding = false;
  }

  private container(w: WorkloadSpec, image: string): Obj {
    const c: Obj = {
      name: w.container,
      image,
      imagePullPolicy: "IfNotPresent",
      resources: {
        requests: { cpu: w.cpu, memory: w.memory },
        limits: { memory: w.memoryLimit ?? w.memory },
      },
      terminationMessagePath: "/dev/termination-log",
      terminationMessagePolicy: "File",
    };
    if (w.port) {
      c.ports = [
        {
          name: w.port.name,
          containerPort: w.port.port,
          protocol: w.port.name === "dns" ? "UDP" : "TCP",
        },
      ];
      if (w.port.name === "http") {
        c.readinessProbe = {
          httpGet: { path: "/healthz", port: "http", scheme: "HTTP" },
          periodSeconds: 10,
          timeoutSeconds: 1,
          successThreshold: 1,
          failureThreshold: 3,
        };
        c.livenessProbe = {
          httpGet: { path: "/livez", port: "http", scheme: "HTTP" },
          initialDelaySeconds: 10,
          periodSeconds: 20,
          timeoutSeconds: 1,
          successThreshold: 1,
          failureThreshold: 3,
        };
      }
    }
    if (w.env) {
      c.env = w.env.map((e) =>
        e.secret
          ? { name: e.name, valueFrom: { secretKeyRef: { name: e.secret[0], key: e.secret[1] } } }
          : { name: e.name, value: e.value ?? "" },
      );
    }
    if (w.configMap) c.envFrom = [{ configMapRef: { name: w.configMap } }];
    if (w.claim) c.volumeMounts = [{ name: "data", mountPath: "/data" }];
    return c;
  }

  private template(w: WorkloadSpec, image: string, selector: Record<string, string>): Obj {
    const spec: Obj = {
      containers: [this.container(w, image)],
      restartPolicy: "Always",
      terminationGracePeriodSeconds: 30,
      dnsPolicy: w.name === "coredns" ? "Default" : "ClusterFirst",
      serviceAccountName: w.helm ?? w.name,
      schedulerName: "default-scheduler",
      securityContext:
        w.kind === "DaemonSet" ? {} : { runAsNonRoot: true, runAsUser: 1000, fsGroup: 1000 },
    };
    if (w.kind === "DaemonSet") {
      spec.tolerations = [{ operator: "Exists" }];
      spec.hostNetwork = true;
      spec.priorityClassName = "system-node-critical";
    }
    if (w.name === "storefront" || w.name === "search-api") {
      spec.volumes = [{ name: "cache", emptyDir: { sizeLimit: "512Mi" } }];
      const c = list(spec.containers)[0];
      if (c) c.volumeMounts = [{ name: "cache", mountPath: "/tmp/cache" }];
    }
    return { metadata: { labels: { ...w.labels, ...selector } }, spec };
  }

  private seedWorkload(w: WorkloadSpec) {
    const t0 = this.started;
    const created = t0 - w.ageDays * DAY;
    const selectorKeys = [
      "app.kubernetes.io/name",
      "app.kubernetes.io/instance",
      "k8s-app",
      "app.kubernetes.io/component",
    ];
    const selector = Object.fromEntries(
      Object.entries(w.labels).filter(([k]) => selectorKeys.includes(k)),
    );
    const labels: Record<string, string> = { ...w.labels };
    const annotations: Record<string, string> = {};
    if (w.helm) {
      labels["app.kubernetes.io/managed-by"] = "Helm";
      labels["app.kubernetes.io/instance"] ??= w.helm;
      annotations["meta.helm.sh/release-name"] = w.helm;
      annotations["meta.helm.sh/release-namespace"] = w.namespace;
    }
    const base: Obj = {
      metadata: {
        name: w.name,
        namespace: w.namespace,
        uid: this.uid(),
        creationTimestamp: iso(created),
        labels,
        annotations,
        generation: 1,
        resourceVersion: "",
      },
    };
    const template = this.template(w, w.image, selector);

    if (w.kind === "Deployment") {
      const revision = w.revision ?? 1;
      annotations["deployment.kubernetes.io/revision"] = String(revision);
      const d: Obj = {
        apiVersion: "apps/v1",
        kind: "Deployment",
        ...base,
        spec: {
          replicas: w.replicas ?? 1,
          selector: { matchLabels: selector },
          template,
          strategy: {
            type: "RollingUpdate",
            rollingUpdate: { maxSurge: "25%", maxUnavailable: "25%" },
          },
          revisionHistoryLimit: 10,
          progressDeadlineSeconds: 600,
        },
        status: {},
      };
      meta(d).generation = revision;
      this.objects(K.deployments).push(d);
      const deployedAt = t0 - (w.deployedMinutesAgo ?? w.ageDays * 24 * 60) * MINUTE;
      const broken = BROKEN_IMAGES.has(w.image);
      if (w.previousImage) {
        const oldTemplate = this.template(w, w.previousImage, selector);
        const oldAt = Math.max(created, deployedAt - 6 * DAY);
        this.makeReplicaSet(d, oldTemplate, revision - 1, oldAt, broken ? (w.replicas ?? 1) : 0);
      }
      this.makeReplicaSet(d, template, revision, deployedAt, broken ? 1 : (w.replicas ?? 1));
      this.rollouts.set(String(meta(d).uid), deployedAt);
    } else if (w.kind === "StatefulSet") {
      const s: Obj = {
        apiVersion: "apps/v1",
        kind: "StatefulSet",
        ...base,
        spec: {
          replicas: w.replicas ?? 1,
          serviceName: w.name,
          selector: { matchLabels: selector },
          template,
          updateStrategy: { type: "RollingUpdate", rollingUpdate: { partition: 0 } },
          podManagementPolicy: "OrderedReady",
          persistentVolumeClaimRetentionPolicy: { whenDeleted: "Retain", whenScaled: "Retain" },
          ...(w.claim
            ? {
                volumeClaimTemplates: [
                  {
                    apiVersion: "v1",
                    kind: "PersistentVolumeClaim",
                    metadata: { name: "data" },
                    spec: {
                      accessModes: ["ReadWriteOnce"],
                      storageClassName: w.claim.storageClass,
                      resources: { requests: { storage: w.claim.size } },
                    },
                  },
                ],
              }
            : {}),
        },
        status: {},
      };
      this.objects(K.statefulsets).push(s);
      for (let i = 0; i < (w.replicas ?? 1); i++) {
        // search-indexer-1 came back 6 minutes ago and found no room.
        const at = w.name === "search-indexer" && i === 1 ? t0 - 6 * MINUTE : created + i * 40_000;
        this.makePod(s, `${w.name}-${String(i)}`, template, at, {
          "statefulset.kubernetes.io/pod-name": `${w.name}-${String(i)}`,
          "controller-revision-hash": `${w.name}-${templateHash(template)}`,
          "apps.kubernetes.io/pod-index": String(i),
        });
      }
    } else {
      const ds: Obj = {
        apiVersion: "apps/v1",
        kind: "DaemonSet",
        ...base,
        spec: {
          selector: { matchLabels: selector },
          template,
          updateStrategy: {
            type: "RollingUpdate",
            rollingUpdate: { maxUnavailable: 1, maxSurge: 0 },
          },
        },
        status: {},
      };
      this.objects(K.daemonsets).push(ds);
      for (const n of this.objects(K.nodes)) {
        const nodeAt = Date.parse(String(meta(n).creationTimestamp));
        const p = this.makePod(
          ds,
          `${w.name}-${this.random.suffix(5)}`,
          template,
          Math.max(created, nodeAt) + 30_000,
          {
            "controller-revision-hash": templateHash(template).slice(0, 10),
          },
        );
        this.bind(p, nameOf(n));
      }
    }
  }

  private makeReplicaSet(
    d: Obj,
    template: Obj,
    revision: number,
    at: number,
    replicas: number,
  ): Obj {
    const hash = templateHash(template);
    const name = `${nameOf(d)}-${hash}`;
    const tpl = structuredClone(template);
    const tplMeta = child(tpl, "metadata");
    tplMeta.labels = { ...labelsOf(tpl, "metadata", "labels"), "pod-template-hash": hash };
    const rs: Obj = {
      apiVersion: "apps/v1",
      kind: "ReplicaSet",
      metadata: {
        name,
        namespace: namespaceOf(d),
        uid: this.uid(),
        creationTimestamp: iso(at),
        labels: { ...labelsOf(tpl, "metadata", "labels") },
        annotations: {
          "deployment.kubernetes.io/revision": String(revision),
          "deployment.kubernetes.io/desired-replicas": String(replicas),
          "deployment.kubernetes.io/max-replicas": String(replicas + 1),
        },
        ownerReferences: [ownerRef(d)],
        generation: 1,
        resourceVersion: "",
      },
      spec: {
        replicas,
        selector: {
          matchLabels: { ...matchLabels(child(d, "spec").selector), "pod-template-hash": hash },
        },
        template: tpl,
      },
      status: {},
    };
    this.objects(K.replicasets).push(rs);
    for (let i = 0; i < replicas; i++) {
      this.makePod(rs, `${name}-${this.random.suffix(5)}`, tpl, at + 5_000 + i * 12_000);
    }
    return rs;
  }

  /** A new pod, not yet scheduled. */
  makePod(
    owner: Obj,
    name: string,
    template: Obj,
    at: number,
    extraLabels: Record<string, string> = {},
  ): Obj {
    const spec = structuredClone(child(template, "spec"));
    const req = podRequests(spec);
    const limits = list(spec.containers).every(
      (c) => isObj(c.resources) && isObj(c.resources.limits) && c.resources.limits.cpu,
    );
    const pod: Obj = {
      apiVersion: "v1",
      kind: "Pod",
      metadata: {
        name,
        generateName: name.replace(/[^-]+$/, ""),
        namespace: namespaceOf(owner),
        uid: this.uid(),
        creationTimestamp: iso(at),
        labels: { ...labelsOf(template, "metadata", "labels"), ...extraLabels },
        ownerReferences: [ownerRef(owner)],
        resourceVersion: "",
      },
      spec: {
        ...spec,
        enableServiceLinks: true,
        preemptionPolicy: "PreemptLowerPriority",
        priority: owner.kind === "DaemonSet" ? 2000001000 : 0,
        tolerations: [
          ...list(spec.tolerations),
          {
            key: "node.kubernetes.io/not-ready",
            operator: "Exists",
            effect: "NoExecute",
            tolerationSeconds: 300,
          },
          {
            key: "node.kubernetes.io/unreachable",
            operator: "Exists",
            effect: "NoExecute",
            tolerationSeconds: 300,
          },
        ],
      },
      status: {
        phase: "Pending",
        qosClass:
          req.cpu === 0 && req.memory === 0 ? "BestEffort" : limits ? "Guaranteed" : "Burstable",
      },
    };
    if (owner.kind === "StatefulSet") {
      const claims = list(child(owner, "spec").volumeClaimTemplates);
      const vols = list(child(pod, "spec").volumes);
      for (const c of claims) {
        vols.push({
          name: nameOf(c),
          persistentVolumeClaim: { claimName: `${nameOf(c)}-${name}` },
        });
      }
      if (vols.length) child(pod, "spec").volumes = vols;
    }
    this.objects(K.pods).push(pod);
    this.sim.set(String(meta(pod).uid), { state: "unschedulable" });
    return pod;
  }

  private seedPodState(p: Obj) {
    if (this.simOf(p).state === "succeeded") return;
    const t0 = this.started;
    const created = Date.parse(String(meta(p).creationTimestamp));
    const node = text(child(p, "spec").nodeName);
    if (!node) {
      this.setPodState(p, "unschedulable", created + 1_000);
      return;
    }
    const image = text(list(child(p, "spec").containers)[0]?.image) ?? "";
    this.setPodState(p, "creating", created + 500);
    if (BROKEN_IMAGES.has(image)) {
      this.setPodState(p, "running", created + 4_000);
      this.crash(p, created + 6_000);
      // It has been at it since the rollout: 14 restarts, the last two minutes ago.
      const cs = list(child(p, "status").containerStatuses)[0];
      if (cs) {
        cs.restartCount = 14;
        const last =
          isObj(cs.lastState) && isObj(cs.lastState.terminated)
            ? cs.lastState.terminated
            : undefined;
        if (last) {
          last.startedAt = iso(t0 - 2 * MINUTE - 3_000);
          last.finishedAt = iso(t0 - 2 * MINUTE);
        }
      }
      this.simOf(p).due = t0 + CRASH_EVERY_MS - 2 * MINUTE;
      return;
    }
    this.setPodState(p, "running", created + 3_000 + (created % 7) * 1000);
    // A few long-running pods have restarted now and then.
    const name = nameOf(p);
    if (
      name.startsWith("kube-prometheus-stack-operator") ||
      name.startsWith("ingress-nginx-controller")
    ) {
      const cs = list(child(p, "status").containerStatuses)[0];
      if (cs) {
        cs.restartCount = name.startsWith("ingress") ? 1 : 2;
        cs.lastState = {
          terminated: {
            exitCode: 137,
            reason: "Error",
            startedAt: iso(created + 5_000),
            finishedAt: iso(t0 - 3 * DAY),
          },
        };
      }
    }
  }

  private seedCronJob() {
    const t0 = this.started;
    const cj: Obj = {
      apiVersion: "batch/v1",
      kind: "CronJob",
      metadata: {
        name: "ledger-reconcile",
        namespace: "payments",
        uid: this.uid(),
        creationTimestamp: iso(t0 - 61 * DAY),
        labels: {
          "app.kubernetes.io/name": "ledger-reconcile",
          "app.kubernetes.io/part-of": "payments",
        },
        resourceVersion: "",
      },
      spec: {
        schedule: "*/5 * * * *",
        concurrencyPolicy: "Forbid",
        successfulJobsHistoryLimit: 3,
        failedJobsHistoryLimit: 1,
        suspend: false,
        jobTemplate: {
          spec: {
            backoffLimit: 2,
            template: {
              metadata: { labels: { "app.kubernetes.io/name": "ledger-reconcile" } },
              spec: {
                restartPolicy: "OnFailure",
                containers: [
                  {
                    name: "reconcile",
                    image: "ghcr.io/acme/ledger-tools:2.4.0",
                    args: ["reconcile", "--since=15m"],
                    resources: {
                      requests: { cpu: "100m", memory: "128Mi" },
                      limits: { memory: "256Mi" },
                    },
                  },
                ],
              },
            },
          },
        },
      },
      status: {},
    };
    this.objects(K.cronjobs).push(cj);
    const last = Math.floor(t0 / CRON_EVERY_MS) * CRON_EVERY_MS;
    for (let i = 2; i >= 0; i--) this.runJob(cj, last - i * CRON_EVERY_MS, true);
  }

  private runJob(cj: Obj, at: number, finished: boolean): Obj {
    const name = `ledger-reconcile-${String(Math.floor(at / MINUTE))}`;
    const template = structuredClone(child(child(child(cj, "spec"), "jobTemplate"), "spec"));
    const job: Obj = {
      apiVersion: "batch/v1",
      kind: "Job",
      metadata: {
        name,
        namespace: namespaceOf(cj),
        uid: this.uid(),
        creationTimestamp: iso(at),
        labels: {
          "app.kubernetes.io/name": "ledger-reconcile",
          "batch.kubernetes.io/job-name": name,
        },
        ownerReferences: [ownerRef(cj)],
        resourceVersion: "",
      },
      spec: { ...template, completions: 1, parallelism: 1 },
      status: { startTime: iso(at), active: 1 },
    };
    this.objects(K.jobs).push(job);
    const tpl = child(template, "template");
    const pod = this.makePod(job, `${name}-${this.random.suffix(5)}`, tpl, at, {
      "batch.kubernetes.io/job-name": name,
    });
    this.simOf(pod).finishAt = at + CRON_RUN_MS;
    if (finished) {
      const node = this.pickNode(pod) ?? nameOf(this.objects(K.nodes)[0] ?? {});
      this.bind(pod, node);
      this.setPodState(pod, "succeeded", at + CRON_RUN_MS);
      this.finishJob(job, at + CRON_RUN_MS, true);
    }
    return job;
  }

  private finishJob(job: Obj, at: number, quiet = false) {
    const start = text(child(job, "status").startTime) ?? iso(at);
    job.status = {
      startTime: start,
      completionTime: iso(at),
      succeeded: 1,
      ready: 0,
      terminating: 0,
      uncountedTerminatedPods: {},
      conditions: [
        {
          type: "SuccessCriteriaMet",
          status: "True",
          lastProbeTime: iso(at),
          lastTransitionTime: iso(at),
        },
        { type: "Complete", status: "True", lastProbeTime: iso(at), lastTransitionTime: iso(at) },
      ],
    };
    if (!quiet) {
      this.put(K.jobs, job);
      this.record("Normal", "Completed", this.ref(job), "Job completed", "job-controller", at);
    }
  }

  /** Events that explain how the cluster got to where it starts. */
  private seedHistory() {
    const t0 = this.started;
    const pods = this.objects(K.pods);
    for (const p of pods) {
      const created = Date.parse(String(meta(p).creationTimestamp));
      if (t0 - created > 60 * MINUTE) continue;
      const node = text(child(p, "spec").nodeName);
      const c = list(child(p, "spec").containers)[0];
      const ref = this.ref(p);
      if (!node) {
        const e = this.record(
          "Warning",
          "FailedScheduling",
          ref,
          this.unschedulableMessage(p),
          "default-scheduler",
          created + 1_000,
        );
        e.count = Math.max(1, Math.floor((t0 - created) / RESCHEDULE_MS));
        e.time = iso(t0 - 20_000);
        continue;
      }
      this.record(
        "Normal",
        "Scheduled",
        ref,
        `Successfully assigned ${namespaceOf(p)}/${nameOf(p)} to ${node}`,
        "default-scheduler",
        created + 500,
      );
      this.record(
        "Normal",
        "Pulled",
        ref,
        `Container image "${text(c?.image) ?? ""}" already present on machine`,
        "kubelet",
        created + 1_500,
      );
      this.record(
        "Normal",
        "Created",
        ref,
        `Created container: ${text(c?.name) ?? ""}`,
        "kubelet",
        created + 2_000,
      );
      this.record(
        "Normal",
        "Started",
        ref,
        `Started container ${text(c?.name) ?? ""}`,
        "kubelet",
        created + 2_500,
      );
      if (BROKEN_IMAGES.has(text(c?.image) ?? "")) {
        const e = this.record(
          "Warning",
          "BackOff",
          ref,
          `Back-off restarting failed container ${text(c?.name) ?? ""} in pod ${nameOf(p)}_${namespaceOf(p)}(${String(meta(p).uid)})`,
          "kubelet",
          created + 30_000,
        );
        e.count = 96;
        e.time = iso(t0 - 40_000);
      }
    }
    const payments = this.find(K.deployments, "payments", "payments-api");
    if (payments) {
      const newest = this.replicaSetsOf(payments).at(-1);
      if (newest) {
        this.record(
          "Normal",
          "ScalingReplicaSet",
          this.ref(payments),
          `Scaled up replica set ${nameOf(newest)} from 0 to 1`,
          "deployment-controller",
          t0 - 25 * MINUTE,
        );
      }
    }
    for (const n of this.objects(K.nodes)) {
      const c = list(child(n, "status").conditions).find((x) => x.type === "MemoryPressure");
      if (c?.status === "True") {
        this.record(
          "Warning",
          "EvictionThresholdMet",
          this.ref(n),
          "Attempting to reclaim memory",
          "kubelet",
          t0 - 9 * MINUTE,
        );
        this.record(
          "Normal",
          "NodeHasInsufficientMemory",
          this.ref(n),
          `Node ${nameOf(n)} status is now: NodeHasInsufficientMemory`,
          "kubelet",
          t0 - 9 * MINUTE,
        );
      }
    }
    for (const j of this.objects(K.jobs)) {
      const at = Date.parse(String(meta(j).creationTimestamp));
      const cj = { kind: "CronJob", namespace: "payments", name: "ledger-reconcile" };
      this.record(
        "Normal",
        "SuccessfulCreate",
        cj,
        `Created job ${nameOf(j)}`,
        "cronjob-controller",
        at,
      );
      this.record(
        "Normal",
        "Completed",
        this.ref(j),
        "Job completed",
        "job-controller",
        at + CRON_RUN_MS,
      );
    }
    this.record(
      "Normal",
      "Issuing",
      { kind: "Certificate", namespace: "shop", name: "storefront-tls" },
      "The certificate has been successfully issued",
      "cert-manager-certificates-issuing",
      t0 - 52 * MINUTE,
    );
    this.events.sort((a, b) => Date.parse(b.time) - Date.parse(a.time));
  }

  // ---- Scheduling and the kubelet ----

  requestsOn(node: string): { cpu: number; memory: number; pods: number } {
    let cpu = 0;
    let memory = 0;
    let pods = 0;
    for (const p of this.objects(K.pods)) {
      if (child(p, "spec").nodeName !== node) continue;
      const phase = child(p, "status").phase;
      if (phase === "Succeeded" || phase === "Failed") continue;
      const r = podRequests(p.spec);
      cpu += r.cpu;
      memory += r.memory;
      pods++;
    }
    return { cpu, memory, pods };
  }

  /** The node with the most free memory that fits the pod, if any. */
  pickNode(pod: Obj): string | undefined {
    const need = podRequests(pod.spec);
    let best: { name: string; free: number } | undefined;
    for (const n of this.objects(K.nodes)) {
      if (child(n, "spec").unschedulable === true) continue;
      const alloc = child(child(n, "status"), "allocatable");
      const used = this.requestsOn(nameOf(n));
      const freeMem = quantity(alloc.memory) - used.memory;
      const freeCpu = quantity(alloc.cpu) - used.cpu;
      if (freeMem < need.memory || freeCpu < need.cpu || used.pods >= quantity(alloc.pods))
        continue;
      if (!best || freeMem > best.free) best = { name: nameOf(n), free: freeMem };
    }
    return best?.name;
  }

  unschedulableMessage(pod: Obj): string {
    const need = podRequests(pod.spec);
    const nodes = this.objects(K.nodes);
    let cordoned = 0;
    let cpu = 0;
    let memory = 0;
    for (const n of nodes) {
      if (child(n, "spec").unschedulable === true) {
        cordoned++;
        continue;
      }
      const alloc = child(child(n, "status"), "allocatable");
      const used = this.requestsOn(nameOf(n));
      if (quantity(alloc.cpu) - used.cpu < need.cpu) cpu++;
      if (quantity(alloc.memory) - used.memory < need.memory) memory++;
    }
    const why = [
      ...(cpu ? [`${String(cpu)} Insufficient cpu`] : []),
      ...(memory ? [`${String(memory)} Insufficient memory`] : []),
      ...(cordoned ? [`${String(cordoned)} node(s) were unschedulable`] : []),
    ].join(", ");
    const n = String(nodes.length);
    return `0/${n} nodes are available: ${why}. preemption: 0/${n} nodes are available: ${n} No preemption victims found for incoming pod.`;
  }

  private bind(pod: Obj, node: string) {
    child(pod, "spec").nodeName = node;
    const n = this.find(K.nodes, "", node);
    const ip = text(list(child(n ?? {}, "status").addresses)[0]?.address) ?? "10.0.0.1";
    this.ipCounter++;
    const status = child(pod, "status");
    status.hostIP = ip;
    status.hostIPs = [{ ip }];
    if (child(pod, "spec").hostNetwork === true) {
      status.podIP = ip;
    } else {
      const parts = ip.split(".");
      status.podIP = `${parts[0] ?? "10"}.${parts[1] ?? "0"}.${parts[2] ?? "0"}.${String(40 + ((this.ipCounter * 37) % 200))}`;
    }
    status.podIPs = [{ ip: status.podIP }];
  }

  simOf(pod: Obj): PodSim {
    const id = String(meta(pod).uid);
    let s = this.sim.get(id);
    if (!s) {
      s = { state: "running" };
      this.sim.set(id, s);
    }
    return s;
  }

  /** Rewrites a pod's status for a lifecycle step, as the kubelet would report it. */
  setPodState(pod: Obj, state: PodState, at: number) {
    const sim = this.simOf(pod);
    sim.state = state;
    const status = child(pod, "status");
    const spec = child(pod, "spec");
    const containers = list(spec.containers);
    const prev = list(status.containerStatuses);
    const restarts = (name: string) => Number(prev.find((c) => c.name === name)?.restartCount ?? 0);
    const t = iso(at);
    const created = String(meta(pod).creationTimestamp);
    const scheduled = {
      type: "PodScheduled",
      status: "True",
      lastProbeTime: null,
      lastTransitionTime: t,
    };
    const keep = (type: string) =>
      list(status.conditions).find((c) => c.type === type) ?? { ...scheduled, type };

    switch (state) {
      case "unschedulable":
        status.phase = "Pending";
        status.conditions = [
          {
            type: "PodScheduled",
            status: "False",
            reason: "Unschedulable",
            message: this.unschedulableMessage(pod),
            lastProbeTime: null,
            lastTransitionTime: created,
          },
        ];
        delete status.containerStatuses;
        delete status.startTime;
        break;
      case "creating":
        status.phase = "Pending";
        status.startTime = t;
        status.conditions = [
          {
            type: "PodReadyToStartContainers",
            status: "False",
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          { type: "Initialized", status: "True", lastProbeTime: null, lastTransitionTime: t },
          {
            type: "Ready",
            status: "False",
            reason: "ContainersNotReady",
            message: `containers with unready status: [${containers.map((c) => String(c.name)).join(" ")}]`,
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          {
            type: "ContainersReady",
            status: "False",
            reason: "ContainersNotReady",
            message: `containers with unready status: [${containers.map((c) => String(c.name)).join(" ")}]`,
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          scheduled,
        ];
        status.containerStatuses = containers.map((c) => ({
          name: c.name,
          image: c.image,
          imageID: "",
          ready: false,
          started: false,
          restartCount: 0,
          state: { waiting: { reason: "ContainerCreating" } },
          lastState: {},
        }));
        sim.due = at + START_MS;
        break;
      case "running":
        status.phase = "Running";
        status.startTime ??= t;
        status.conditions = [
          {
            type: "PodReadyToStartContainers",
            status: "True",
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          { ...keep("Initialized"), status: "True" },
          { type: "Ready", status: "True", lastProbeTime: null, lastTransitionTime: t },
          { type: "ContainersReady", status: "True", lastProbeTime: null, lastTransitionTime: t },
          { ...keep("PodScheduled"), status: "True" },
        ];
        status.containerStatuses = containers.map((c) => ({
          name: c.name,
          image: c.image,
          imageID: `${String(c.image).split(":")[0] ?? ""}@sha256:${templateHash(c.image).repeat(7).slice(0, 64)}`,
          containerID: `containerd://${templateHash([c.name, meta(pod).uid, at])
            .repeat(7)
            .slice(0, 64)}`,
          ready: true,
          started: true,
          restartCount: restarts(String(c.name)),
          state: { running: { startedAt: t } },
          lastState: prev.find((p) => p.name === c.name)?.lastState ?? {},
        }));
        delete sim.due;
        break;
      case "crashloop":
        this.crash(pod, at);
        break;
      case "succeeded":
        status.phase = "Succeeded";
        status.startTime ??= created;
        status.conditions = [
          {
            type: "PodReadyToStartContainers",
            status: "False",
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          {
            type: "Initialized",
            status: "True",
            reason: "PodCompleted",
            lastProbeTime: null,
            lastTransitionTime: created,
          },
          {
            type: "Ready",
            status: "False",
            reason: "PodCompleted",
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          {
            type: "ContainersReady",
            status: "False",
            reason: "PodCompleted",
            lastProbeTime: null,
            lastTransitionTime: t,
          },
          {
            type: "PodScheduled",
            status: "True",
            lastProbeTime: null,
            lastTransitionTime: created,
          },
        ];
        status.containerStatuses = containers.map((c) => ({
          name: c.name,
          image: c.image,
          imageID: "",
          ready: false,
          started: false,
          restartCount: 0,
          state: {
            terminated: { exitCode: 0, reason: "Completed", startedAt: created, finishedAt: t },
          },
          lastState: {},
        }));
        delete sim.due;
        break;
    }
  }

  /** The container started and fell over: Error, exit code 1, back off and retry. */
  private crash(pod: Obj, at: number) {
    const sim = this.simOf(pod);
    sim.state = "crashloop";
    sim.due = at + CRASH_EVERY_MS;
    const status = child(pod, "status");
    status.phase = "Running";
    const t = iso(at);
    const names = list(child(pod, "spec").containers).map((c) => String(c.name));
    status.conditions = list(status.conditions).map((c) =>
      c.type === "Ready" || c.type === "ContainersReady"
        ? {
            type: c.type,
            status: "False",
            reason: "ContainersNotReady",
            message: `containers with unready status: [${names.join(" ")}]`,
            lastProbeTime: null,
            lastTransitionTime: t,
          }
        : c,
    );
    status.containerStatuses = list(status.containerStatuses).map((c) => {
      const running = isObj(c.state) && isObj(c.state.running) ? c.state.running : undefined;
      return {
        ...c,
        ready: false,
        started: false,
        restartCount: Number(c.restartCount ?? 0) + (running ? 1 : 0),
        state: {
          waiting: {
            reason: "CrashLoopBackOff",
            message: `back-off 5m0s restarting failed container=${String(c.name)} pod=${nameOf(pod)}_${namespaceOf(pod)}(${String(meta(pod).uid)})`,
          },
        },
        lastState: {
          terminated: {
            exitCode: 1,
            reason: "Error",
            startedAt: text(running?.startedAt) ?? iso(at - 3_000),
            finishedAt: t,
            containerID: c.containerID,
          },
        },
      };
    });
  }

  private updateNodeConditions(initial = false) {
    const now = this.now();
    for (const n of this.objects(K.nodes)) {
      const status = child(n, "status");
      const old = list(status.conditions);
      const since = (type: string, value: string, fallback: number) => {
        const c = old.find((x) => x.type === type);
        return c?.status === value ? str(c.lastTransitionTime) : iso(fallback);
      };
      const created = Date.parse(String(meta(n).creationTimestamp));
      const memFull = this.nodeUsage(nameOf(n)).memory >= 0.88;
      const pressureSince = initial ? this.started - 9 * MINUTE : now;
      const conditions = [
        memFull
          ? {
              type: "MemoryPressure",
              status: "True",
              reason: "KubeletHasInsufficientMemory",
              message: "kubelet has insufficient memory available",
              lastHeartbeatTime: iso(now),
              lastTransitionTime: since("MemoryPressure", "True", pressureSince),
            }
          : {
              type: "MemoryPressure",
              status: "False",
              reason: "KubeletHasSufficientMemory",
              message: "kubelet has sufficient memory available",
              lastHeartbeatTime: iso(now),
              lastTransitionTime: since("MemoryPressure", "False", created),
            },
        {
          type: "DiskPressure",
          status: "False",
          reason: "KubeletHasNoDiskPressure",
          message: "kubelet has no disk pressure",
          lastHeartbeatTime: iso(now),
          lastTransitionTime: iso(created),
        },
        {
          type: "PIDPressure",
          status: "False",
          reason: "KubeletHasSufficientPID",
          message: "kubelet has sufficient PID available",
          lastHeartbeatTime: iso(now),
          lastTransitionTime: iso(created),
        },
        {
          type: "Ready",
          status: "True",
          reason: "KubeletReady",
          message: "kubelet is posting ready status",
          lastHeartbeatTime: iso(now),
          lastTransitionTime: iso(created + 40_000),
        },
      ];
      const was = old.find((x) => x.type === "MemoryPressure")?.status;
      status.conditions = conditions;
      if (!initial && was && was !== conditions[0]?.status) {
        this.put(K.nodes, n);
        if (!memFull)
          this.record(
            "Normal",
            "NodeHasSufficientMemory",
            this.ref(n),
            `Node ${nameOf(n)} status is now: NodeHasSufficientMemory`,
          );
      }
    }
  }

  /** A node's load as a share of allocatable: its usual load, moved by what was scheduled since. */
  nodeUsage(node: string, at = this.now()): { cpu: number; memory: number } {
    const spec = NODES.find((n) => n.name === node);
    const n = this.find(K.nodes, "", node);
    if (!n) return { cpu: 0, memory: 0 };
    const alloc = child(child(n, "status"), "allocatable");
    const base = this.baseline.get(node) ?? { cpu: 0, memory: 0 };
    const now = this.requestsOn(node);
    const seed = NODES.findIndex((x) => x.name === node) + 1;
    const wobble = (k: number) =>
      Math.sin((at - this.started) / 41_000 + seed * k) * 0.012 +
      Math.sin(at / 13_000 + seed * k * 3) * 0.006;
    const clamp = (v: number) => Math.min(0.99, Math.max(0.02, v));
    return {
      cpu: clamp(
        (spec?.cpu ?? 0.4) + ((now.cpu - base.cpu) * 0.7) / quantity(alloc.cpu) + wobble(1.7) * 2,
      ),
      memory: clamp(
        (spec?.memory ?? 0.5) +
          ((now.memory - base.memory) * 0.8) / quantity(alloc.memory) +
          wobble(2.9),
      ),
    };
  }

  // ---- Controllers ----

  replicaSetsOf(d: Obj): Obj[] {
    return this.objects(K.replicasets)
      .filter(
        (rs) =>
          namespaceOf(rs) === namespaceOf(d) &&
          controllerOf(rs)?.name === nameOf(d) &&
          controllerOf(rs)?.kind === "Deployment",
      )
      .sort((a, b) => revisionOf(a) - revisionOf(b));
  }

  podsOf(owner: Obj): Obj[] {
    return this.objects(K.pods).filter(
      (p) =>
        namespaceOf(p) === namespaceOf(owner) &&
        controllerOf(p)?.kind === owner.kind &&
        controllerOf(p)?.name === nameOf(owner),
    );
  }

  private isBroken(template: unknown): boolean {
    const spec = isObj(template) && isObj(template.spec) ? template.spec : {};
    return list(spec.containers).some((c) => BROKEN_IMAGES.has(text(c.image) ?? ""));
  }

  private reconcileDeployment(d: Obj, now: number) {
    const spec = child(d, "spec");
    const replicas = typeof spec.replicas === "number" ? spec.replicas : 1;
    const template = child(spec, "template");
    const hash = templateHash(template);
    const sets = this.replicaSetsOf(d);
    let current = sets.find(
      (rs) => labelsOf(rs, "metadata", "labels")["pod-template-hash"] === hash,
    );
    // The template the ReplicaSet was made from is the Deployment's, without the hash label.
    current ??= sets.find((rs) => {
      const t = structuredClone(child(child(rs, "spec"), "template"));
      const labels = labelsOf(t, "metadata", "labels");
      delete labels["pod-template-hash"];
      child(t, "metadata").labels = labels;
      return templateHash(t) === hash;
    });
    const revision = Math.max(0, ...sets.map(revisionOf)) + 1;
    const annotations = labelsOf(d, "metadata", "annotations");
    if (!current) {
      current = this.makeReplicaSet(d, template, revision, now, 0);
      for (const p of this.podsOf(current)) this.remove(K.pods, p);
      this.put(K.replicasets, current);
      annotations["deployment.kubernetes.io/revision"] = String(revision);
      this.rollouts.set(String(meta(d).uid), now);
      meta(d).annotations = annotations;
    } else {
      const rev = labelsOf(current, "metadata", "annotations")["deployment.kubernetes.io/revision"];
      if (rev && annotations["deployment.kubernetes.io/revision"] !== rev) {
        // Rolled back to an older ReplicaSet: it becomes the newest revision.
        child(current, "metadata").annotations = {
          ...labelsOf(current, "metadata", "annotations"),
          "deployment.kubernetes.io/revision": String(revision),
        };
        annotations["deployment.kubernetes.io/revision"] = String(revision);
        this.rollouts.set(String(meta(d).uid), now);
        meta(d).annotations = annotations;
      }
    }

    const broken = this.isBroken(template);
    const others = sets.filter((rs) => rs !== current);
    const lastGood = [...others]
      .reverse()
      .find(
        (rs) =>
          !this.isBroken(child(child(rs, "spec"), "template")) &&
          Number(child(rs, "spec").replicas) > 0,
      );
    const want = new Map<Obj, number>();
    if (broken && lastGood) {
      // The rollout can't finish: the new pods never become ready, so the old ones stay.
      want.set(current, Math.min(replicas, Math.max(1, Math.ceil(replicas * 0.25))));
      want.set(lastGood, replicas);
    } else {
      want.set(current, replicas);
    }
    for (const rs of [current, ...others]) {
      const n = want.get(rs) ?? 0;
      const rsSpec = child(rs, "spec");
      if (rsSpec.replicas !== n) {
        const from = Number(rsSpec.replicas ?? 0);
        rsSpec.replicas = n;
        child(rs, "metadata").annotations = {
          ...labelsOf(rs, "metadata", "annotations"),
          "deployment.kubernetes.io/desired-replicas": String(replicas),
        };
        this.put(K.replicasets, rs);
        this.record(
          "Normal",
          "ScalingReplicaSet",
          this.ref(d),
          `Scaled ${n > from ? "up" : "down"} replica set ${nameOf(rs)} from ${String(from)} to ${String(n)}`,
          "deployment-controller",
        );
      }
      this.reconcileReplicaSet(rs, now);
    }
  }

  private reconcileReplicaSet(rs: Obj, now: number) {
    const want = Number(child(rs, "spec").replicas ?? 0);
    const pods = this.podsOf(rs);
    const template = child(child(rs, "spec"), "template");
    for (let i = pods.length; i < want; i++) {
      const p = this.makePod(rs, `${nameOf(rs)}-${this.random.suffix(5)}`, template, now);
      this.record(
        "Normal",
        "SuccessfulCreate",
        this.ref(rs),
        `Created pod: ${nameOf(p)}`,
        "replicaset-controller",
      );
      this.put(K.pods, p);
    }
    // Scale down the least useful pods first: not scheduled, then not ready, then the newest.
    const rank = (p: Obj) => {
      const s = this.simOf(p).state;
      return s === "unschedulable" ? 0 : s === "crashloop" || s === "creating" ? 1 : 2;
    };
    const extra = [...pods].sort(
      (a, b) =>
        rank(a) - rank(b) ||
        String(meta(b).creationTimestamp).localeCompare(String(meta(a).creationTimestamp)),
    );
    for (let i = 0; i < pods.length - want; i++) {
      const p = extra[i];
      if (!p) break;
      this.record(
        "Normal",
        "SuccessfulDelete",
        this.ref(rs),
        `Deleted pod: ${nameOf(p)}`,
        "replicaset-controller",
      );
      this.killPod(p);
    }
  }

  private reconcileStatefulSet(s: Obj, now: number) {
    const spec = child(s, "spec");
    const replicas = typeof spec.replicas === "number" ? spec.replicas : 1;
    const template = child(spec, "template");
    const hash = templateHash(template);
    const pods = this.podsOf(s);
    for (let i = 0; i < replicas; i++) {
      const name = `${nameOf(s)}-${String(i)}`;
      const existing = pods.find((p) => nameOf(p) === name);
      if (existing) {
        if (
          labelsOf(existing, "metadata", "labels")["controller-revision-hash"] ===
          `${nameOf(s)}-${hash}`
        )
          continue;
        // The template changed: replace the pod, keeping its name.
        this.killPod(existing);
      }
      const p = this.makePod(s, name, template, now, {
        "statefulset.kubernetes.io/pod-name": name,
        "controller-revision-hash": `${nameOf(s)}-${hash}`,
        "apps.kubernetes.io/pod-index": String(i),
      });
      this.record(
        "Normal",
        "SuccessfulCreate",
        this.ref(s),
        `create Pod ${name} in StatefulSet ${nameOf(s)} successful`,
        "statefulset-controller",
      );
      this.put(K.pods, p);
    }
    for (const p of pods) {
      const idx = Number(/-(\d+)$/.exec(nameOf(p))?.[1] ?? 0);
      if (idx >= replicas && this.objects(K.pods).includes(p)) {
        this.record(
          "Normal",
          "SuccessfulDelete",
          this.ref(s),
          `delete Pod ${nameOf(p)} in StatefulSet ${nameOf(s)} successful`,
          "statefulset-controller",
        );
        this.killPod(p);
      }
    }
    // Claims from the volumeClaimTemplates, one per pod.
    for (const c of list(spec.volumeClaimTemplates)) {
      for (let i = 0; i < replicas; i++) {
        const claim = `${nameOf(c)}-${nameOf(s)}-${String(i)}`;
        if (!this.find(K.pvcs, namespaceOf(s), claim)) this.makeClaim(s, c, claim, now);
      }
    }
  }

  makeClaim(s: Obj, template: Obj, name: string, at: number) {
    const spec = structuredClone(child(template, "spec"));
    const pvc: Obj = {
      apiVersion: "v1",
      kind: "PersistentVolumeClaim",
      metadata: {
        name,
        namespace: namespaceOf(s),
        uid: this.uid(),
        creationTimestamp: iso(at),
        labels: labelsOf(s, "spec", "selector", "matchLabels"),
        annotations: { "volume.kubernetes.io/storage-provisioner": "ebs.csi.aws.com" },
        finalizers: ["kubernetes.io/pvc-protection"],
        resourceVersion: "",
      },
      spec: { ...spec, volumeMode: "Filesystem" },
      status: { phase: "Pending" },
    };
    this.objects(K.pvcs).push(pvc);
    this.bindClaims(at);
  }

  /** Claims bind once a pod that uses them is placed (WaitForFirstConsumer). */
  bindClaims(at: number) {
    for (const pvc of this.objects(K.pvcs)) {
      if (child(pvc, "status").phase === "Bound") continue;
      const used = this.objects(K.pods).find(
        (p) =>
          namespaceOf(p) === namespaceOf(pvc) &&
          child(p, "spec").nodeName &&
          list(child(p, "spec").volumes).some(
            (v) =>
              isObj(v.persistentVolumeClaim) && v.persistentVolumeClaim.claimName === nameOf(pvc),
          ),
      );
      if (!used) continue;
      const size = text(labelsOf(pvc, "spec", "resources", "requests").storage) ?? "1Gi";
      const pvName = `pvc-${String(meta(pvc).uid)}`;
      const node = this.find(K.nodes, "", String(child(used, "spec").nodeName));
      const zone =
        labelsOf(node ?? {}, "metadata", "labels")["topology.kubernetes.io/zone"] ?? "eu-west-1a";
      this.objects(K.pvs).push({
        apiVersion: "v1",
        kind: "PersistentVolume",
        metadata: {
          name: pvName,
          uid: this.uid(),
          creationTimestamp: meta(pvc).creationTimestamp,
          annotations: {
            "pv.kubernetes.io/provisioned-by": "ebs.csi.aws.com",
            "volume.kubernetes.io/provisioner-deletion-secret-name": "",
          },
          finalizers: [
            "external-provisioner.volume.kubernetes.io/finalizer",
            "kubernetes.io/pv-protection",
          ],
          resourceVersion: "",
        },
        spec: {
          accessModes: ["ReadWriteOnce"],
          capacity: { storage: size },
          claimRef: {
            apiVersion: "v1",
            kind: "PersistentVolumeClaim",
            name: nameOf(pvc),
            namespace: namespaceOf(pvc),
            uid: meta(pvc).uid,
          },
          csi: {
            driver: "ebs.csi.aws.com",
            fsType: "ext4",
            volumeHandle: `vol-0${templateHash(pvName).slice(0, 8)}${String(meta(pvc).uid).slice(0, 8)}`,
          },
          nodeAffinity: {
            required: {
              nodeSelectorTerms: [
                {
                  matchExpressions: [
                    { key: "topology.kubernetes.io/zone", operator: "In", values: [zone] },
                  ],
                },
              ],
            },
          },
          persistentVolumeReclaimPolicy: "Delete",
          storageClassName: child(pvc, "spec").storageClassName ?? "gp3",
          volumeMode: "Filesystem",
        },
        status: { phase: "Bound", lastPhaseTransitionTime: iso(at) },
      });
      child(pvc, "spec").volumeName = pvName;
      pvc.status = { phase: "Bound", accessModes: ["ReadWriteOnce"], capacity: { storage: size } };
      meta(pvc).annotations = {
        ...labelsOf(pvc, "metadata", "annotations"),
        "pv.kubernetes.io/bind-completed": "yes",
        "pv.kubernetes.io/bound-by-controller": "yes",
      };
      if (!this.seeding) {
        this.put(K.pvcs, pvc);
        this.record(
          "Normal",
          "ProvisioningSucceeded",
          this.ref(pvc),
          `Successfully provisioned volume ${pvName}`,
          "ebs.csi.aws.com_ebs-csi-controller",
        );
      }
    }
  }

  private reconcileDaemonSet(ds: Obj, now: number) {
    const template = child(child(ds, "spec"), "template");
    const hash = templateHash(template).slice(0, 10);
    const pods = this.podsOf(ds);
    for (const n of this.objects(K.nodes)) {
      const mine = pods.filter((p) => child(p, "spec").nodeName === nameOf(n));
      const current = mine.find(
        (p) => labelsOf(p, "metadata", "labels")["controller-revision-hash"] === hash,
      );
      if (current) continue;
      for (const p of mine) this.killPod(p);
      const p = this.makePod(ds, `${nameOf(ds)}-${this.random.suffix(5)}`, template, now, {
        "controller-revision-hash": hash,
      });
      this.bind(p, nameOf(n));
      this.setPodState(p, "creating", now);
      this.record(
        "Normal",
        "SuccessfulCreate",
        this.ref(ds),
        `Created pod: ${nameOf(p)}`,
        "daemonset-controller",
      );
      this.put(K.pods, p);
    }
    for (const p of pods) {
      if (
        !this.find(K.nodes, "", String(child(p, "spec").nodeName)) &&
        this.objects(K.pods).includes(p)
      )
        this.killPod(p);
    }
  }

  /** Places pods that are waiting for a node, when one has room now. */
  private schedule(now: number, retry: boolean) {
    for (const p of this.objects(K.pods)) {
      if (child(p, "spec").nodeName) continue;
      const node = this.pickNode(p);
      if (node) {
        this.bind(p, node);
        this.setPodState(p, "creating", now);
        this.record(
          "Normal",
          "Scheduled",
          this.ref(p),
          `Successfully assigned ${namespaceOf(p)}/${nameOf(p)} to ${node}`,
          "default-scheduler",
        );
        this.put(K.pods, p);
        this.bindClaims(now);
      } else if (
        retry ||
        this.simOf(p).state !== "unschedulable" ||
        !list(child(p, "status").conditions).length
      ) {
        this.setPodState(p, "unschedulable", now);
        this.record(
          "Warning",
          "FailedScheduling",
          this.ref(p),
          this.unschedulableMessage(p),
          "default-scheduler",
        );
        this.put(K.pods, p);
      }
    }
  }

  private updateStatuses(now: number, quiet: boolean) {
    const ready = (p: Obj) =>
      list(child(p, "status").conditions).some((c) => c.type === "Ready" && c.status === "True");
    const save = (key: string, o: Obj, status: Obj) => {
      if (JSON.stringify(o.status) === JSON.stringify(status)) return;
      o.status = status;
      if (!quiet) this.put(key, o);
    };
    for (const rs of this.objects(K.replicasets)) {
      const pods = this.podsOf(rs);
      const r = pods.filter(ready).length;
      save(K.replicasets, rs, {
        replicas: pods.length,
        fullyLabeledReplicas: pods.length,
        readyReplicas: r,
        availableReplicas: r,
        observedGeneration: meta(rs).generation ?? 1,
      });
    }
    for (const d of this.objects(K.deployments)) {
      const spec = child(d, "spec");
      const want = typeof spec.replicas === "number" ? spec.replicas : 1;
      const sets = this.replicaSetsOf(d);
      const rev = labelsOf(d, "metadata", "annotations")["deployment.kubernetes.io/revision"];
      const current =
        sets.find(
          (rs) =>
            labelsOf(rs, "metadata", "annotations")["deployment.kubernetes.io/revision"] === rev,
        ) ?? sets.at(-1);
      const pods = sets.flatMap((rs) => this.podsOf(rs));
      const updated = current ? this.podsOf(current) : [];
      const r = pods.filter(ready).length;
      const done =
        updated.length === want && updated.filter(ready).length === want && pods.length === want;
      const started =
        this.rollouts.get(String(meta(d).uid)) ?? Date.parse(String(meta(d).creationTimestamp));
      const stuck = !done && now - started > 600_000;
      const t = iso(done ? Math.min(now, started + 30_000) : started);
      save(K.deployments, d, {
        observedGeneration: meta(d).generation ?? 1,
        replicas: pods.length,
        updatedReplicas: updated.length,
        readyReplicas: r,
        availableReplicas: r,
        ...(pods.length > r ? { unavailableReplicas: pods.length - r } : {}),
        conditions: [
          {
            type: "Available",
            status: r >= want - Math.floor(want * 0.25) ? "True" : "False",
            reason:
              r >= want - Math.floor(want * 0.25)
                ? "MinimumReplicasAvailable"
                : "MinimumReplicasUnavailable",
            message:
              r >= want - Math.floor(want * 0.25)
                ? "Deployment has minimum availability."
                : "Deployment does not have minimum availability.",
            lastUpdateTime: String(meta(d).creationTimestamp),
            lastTransitionTime: String(meta(d).creationTimestamp),
          },
          stuck
            ? {
                type: "Progressing",
                status: "False",
                reason: "ProgressDeadlineExceeded",
                message: `ReplicaSet "${current ? nameOf(current) : ""}" has timed out progressing.`,
                lastUpdateTime: iso(started + 600_000),
                lastTransitionTime: iso(started + 600_000),
              }
            : done
              ? {
                  type: "Progressing",
                  status: "True",
                  reason: "NewReplicaSetAvailable",
                  message: `ReplicaSet "${current ? nameOf(current) : ""}" has successfully progressed.`,
                  lastUpdateTime: t,
                  lastTransitionTime: String(meta(d).creationTimestamp),
                }
              : {
                  type: "Progressing",
                  status: "True",
                  reason: "ReplicaSetUpdated",
                  message: `ReplicaSet "${current ? nameOf(current) : ""}" is progressing.`,
                  lastUpdateTime: t,
                  lastTransitionTime: t,
                },
        ],
      });
    }
    for (const s of this.objects(K.statefulsets)) {
      const pods = this.podsOf(s);
      const r = pods.filter(ready).length;
      const rev = `${nameOf(s)}-${templateHash(child(child(s, "spec"), "template"))}`;
      save(K.statefulsets, s, {
        observedGeneration: meta(s).generation ?? 1,
        replicas: pods.length,
        readyReplicas: r,
        currentReplicas: pods.length,
        updatedReplicas: pods.length,
        availableReplicas: r,
        currentRevision: rev,
        updateRevision: rev,
        collisionCount: 0,
      });
    }
    for (const ds of this.objects(K.daemonsets)) {
      const pods = this.podsOf(ds);
      const n = this.objects(K.nodes).length;
      const r = pods.filter(ready).length;
      save(K.daemonsets, ds, {
        observedGeneration: meta(ds).generation ?? 1,
        desiredNumberScheduled: n,
        currentNumberScheduled: pods.length,
        updatedNumberScheduled: pods.length,
        numberReady: r,
        numberAvailable: r,
        ...(n - r > 0 ? { numberUnavailable: n - r } : {}),
        numberMisscheduled: 0,
      });
    }
    for (const cj of this.objects(K.cronjobs)) {
      const jobs = this.objects(K.jobs).filter((j) => controllerOf(j)?.name === nameOf(cj));
      const active = jobs.filter((j) => !child(j, "status").completionTime);
      const last = jobs
        .map((j) => String(meta(j).creationTimestamp))
        .sort()
        .at(-1);
      const done = jobs
        .map((j) => text(child(j, "status").completionTime))
        .filter(Boolean)
        .sort()
        .at(-1);
      save(K.cronjobs, cj, {
        ...(active.length
          ? {
              active: active.map((j) => ({
                apiVersion: "batch/v1",
                kind: "Job",
                name: nameOf(j),
                namespace: namespaceOf(j),
                uid: meta(j).uid,
              })),
            }
          : {}),
        ...(last ? { lastScheduleTime: last } : {}),
        ...(done ? { lastSuccessfulTime: done } : {}),
      });
    }
  }

  /** Brings every workload's pods in line with its spec, places pods and updates statuses. */
  reconcileAll(quiet = false) {
    const now = this.now();
    this.transaction(() => {
      for (const d of [...this.objects(K.deployments)]) this.reconcileDeployment(d, now);
      for (const rs of [...this.objects(K.replicasets)]) {
        if (!controllerOf(rs)) this.reconcileReplicaSet(rs, now);
      }
      for (const s of [...this.objects(K.statefulsets)]) this.reconcileStatefulSet(s, now);
      for (const ds of [...this.objects(K.daemonsets)]) this.reconcileDaemonSet(ds, now);
      if (!quiet) this.schedule(now, false);
      this.updateStatuses(now, quiet);
    });
  }

  /** The kubelet stops the pod's containers and it's gone. */
  killPod(p: Obj) {
    if (!this.objects(K.pods).includes(p)) return;
    for (const c of list(child(p, "spec").containers)) {
      if (child(p, "status").phase === "Running") {
        this.record("Normal", "Killing", this.ref(p), `Stopping container ${String(c.name)}`);
      }
    }
    this.remove(K.pods, p);
  }

  /** Moves the simulation forward: containers start and crash, the CronJob runs, the scheduler retries. */
  tick() {
    const now = this.now();
    this.transaction(() => {
      for (const p of [...this.objects(K.pods)]) {
        const s = this.simOf(p);
        if (s.finishAt !== undefined && now >= s.finishAt && s.state === "running") {
          this.setPodState(p, "succeeded", s.finishAt);
          this.put(K.pods, p);
          const owner = controllerOf(p);
          const job = owner ? this.find(K.jobs, namespaceOf(p), owner.name) : undefined;
          if (job) this.finishJob(job, s.finishAt);
          continue;
        }
        if (s.due === undefined || now < s.due) continue;
        const c = list(child(p, "spec").containers)[0];
        const name = text(c?.name) ?? "";
        if (s.state === "creating") {
          this.record(
            "Normal",
            "Pulled",
            this.ref(p),
            `Container image "${text(c?.image) ?? ""}" already present on machine`,
          );
          this.record("Normal", "Created", this.ref(p), `Created container: ${name}`);
          this.record("Normal", "Started", this.ref(p), `Started container ${name}`);
          this.setPodState(p, "running", s.due);
          if (this.isBroken({ spec: child(p, "spec") })) this.crash(p, s.due + 3_000);
          this.put(K.pods, p);
        } else if (s.state === "crashloop") {
          // Tried again and crashed again.
          this.setPodState(p, "running", now - 3_000);
          this.crash(p, now);
          this.record(
            "Normal",
            "Pulled",
            this.ref(p),
            `Container image "${text(c?.image) ?? ""}" already present on machine`,
          );
          this.record(
            "Warning",
            "BackOff",
            this.ref(p),
            `Back-off restarting failed container ${name} in pod ${nameOf(p)}_${namespaceOf(p)}(${String(meta(p).uid)})`,
          );
          this.put(K.pods, p);
        }
      }

      if (now - this.lastSchedule >= RESCHEDULE_MS) {
        this.lastSchedule = now;
        this.schedule(now, true);
      }

      const slot = Math.floor(now / CRON_EVERY_MS) * CRON_EVERY_MS;
      if (slot > this.lastCron) {
        this.lastCron = slot;
        for (const cj of this.objects(K.cronjobs)) {
          if (child(cj, "spec").suspend === true) continue;
          const job = this.runJob(cj, now, false);
          this.put(K.jobs, job);
          this.record(
            "Normal",
            "SuccessfulCreate",
            this.ref(cj),
            `Created job ${nameOf(job)}`,
            "cronjob-controller",
          );
          const keep = Number(child(cj, "spec").successfulJobsHistoryLimit ?? 3);
          const jobs = this.objects(K.jobs).filter(
            (j) => controllerOf(j)?.name === nameOf(cj) && child(j, "status").completionTime,
          );
          jobs.sort((a, b) =>
            String(meta(a).creationTimestamp).localeCompare(String(meta(b).creationTimestamp)),
          );
          for (const old of jobs.slice(0, Math.max(0, jobs.length - keep))) {
            this.record(
              "Normal",
              "SawCompletedJob",
              this.ref(cj),
              `Saw completed job: ${nameOf(old)}, condition: Complete`,
              "cronjob-controller",
            );
            this.cascade(K.jobs, old);
          }
        }
        this.schedule(now, false);
      }

      this.updateNodeConditions();
      this.updateStatuses(now, false);
    });
  }

  // ---- Changes people make ----

  /** Deletes an object and what it owns, the way the garbage collector would. */
  deleteObject(key: string, obj: Obj) {
    this.transaction(() => {
      this.cascade(key, obj);
      this.reconcileAll();
    });
  }

  private cascade(key: string, obj: Obj) {
    const kind = text(obj.kind);
    if (key === K.pods) this.killPod(obj);
    else this.remove(key, obj);
    const owned = (k: string) =>
      this.objects(k).filter(
        (o) =>
          namespaceOf(o) === namespaceOf(obj) &&
          controllerOf(o)?.kind === kind &&
          controllerOf(o)?.name === nameOf(obj),
      );
    if (key === K.namespaces) {
      for (const [k, objs] of this.store) {
        for (const o of [...objs]) if (namespaceOf(o) === nameOf(obj)) this.remove(k, o);
      }
    }
    if (key === K.nodes) {
      for (const p of this.objects(K.pods).filter((p) => child(p, "spec").nodeName === nameOf(obj)))
        this.killPod(p);
    }
    for (const k of [K.replicasets, K.jobs]) for (const o of owned(k)) this.cascade(k, o);
    for (const p of owned(K.pods)) this.killPod(p);
  }

  scale(key: string, obj: Obj, replicas: number) {
    const spec = child(obj, "spec");
    const from = Number(spec.replicas ?? 1);
    spec.replicas = replicas;
    meta(obj).generation = Number(meta(obj).generation ?? 1) + 1;
    this.transaction(() => {
      this.put(key, obj);
      if (obj.kind === "StatefulSet" && replicas !== from) {
        this.record(
          "Normal",
          replicas > from ? "SuccessfulCreate" : "SuccessfulDelete",
          this.ref(obj),
          `Scaled ${nameOf(obj)} from ${String(from)} to ${String(replicas)}`,
          "statefulset-controller",
        );
      }
      this.reconcileAll();
    });
  }

  restart(key: string, obj: Obj) {
    const template = child(child(obj, "spec"), "template");
    const m = child(template, "metadata");
    m.annotations = {
      ...labelsOf(template, "metadata", "annotations"),
      "kubectl.kubernetes.io/restartedAt": iso(this.now()),
    };
    meta(obj).generation = Number(meta(obj).generation ?? 1) + 1;
    this.transaction(() => {
      this.put(key, obj);
      this.reconcileAll();
    });
  }

  cordon(node: Obj, on: boolean) {
    const spec = child(node, "spec");
    if (on) {
      spec.unschedulable = true;
      spec.taints = [
        {
          key: "node.kubernetes.io/unschedulable",
          effect: "NoSchedule",
          timeAdded: iso(this.now()),
        },
      ];
    } else {
      delete spec.unschedulable;
      delete spec.taints;
    }
    this.transaction(() => {
      this.put(K.nodes, node);
      this.record(
        "Normal",
        on ? "NodeNotSchedulable" : "NodeSchedulable",
        this.ref(node),
        `Node ${nameOf(node)} status is now: ${on ? "NodeNotSchedulable" : "NodeSchedulable"}`,
      );
      if (!on) this.schedule(this.now(), true);
    });
  }

  /** Pods a drain would evict, and the ones it leaves (DaemonSet pods, finished pods). */
  drainPlan(node: string) {
    const evict: { namespace: string; name: string; localData?: boolean }[] = [];
    const skip: { namespace: string; name: string; reason: string }[] = [];
    for (const p of this.objects(K.pods)) {
      if (child(p, "spec").nodeName !== node) continue;
      const base = { namespace: namespaceOf(p), name: nameOf(p) };
      const phase = child(p, "status").phase;
      if (controllerOf(p)?.kind === "DaemonSet")
        skip.push({ ...base, reason: "Managed by a DaemonSet, which runs it on every node" });
      else if (phase === "Succeeded" || phase === "Failed")
        skip.push({ ...base, reason: "Already finished" });
      else {
        const local = list(child(p, "spec").volumes).some((v) => isObj(v.emptyDir));
        evict.push(local ? { ...base, localData: true } : base);
      }
    }
    return { evict, skip };
  }

  drain(node: Obj) {
    const plan = this.drainPlan(nameOf(node));
    this.transaction(() => {
      if (child(node, "spec").unschedulable !== true) this.cordon(node, true);
      for (const e of plan.evict) {
        const p = this.find(K.pods, e.namespace, e.name);
        if (!p) continue;
        this.record("Normal", "Evicted", this.ref(p), "Evicted by a node drain", "glowpod");
        this.killPod(p);
      }
      this.reconcileAll();
    });
    return {
      evicted: plan.evict,
      blocked: [],
      failed: [],
      skipped: plan.skip.map(({ namespace, name, reason }) => ({ namespace, name, reason })),
    };
  }

  /** Saves an edited object, keeping what the server owns (status, uid, timestamps). */
  replace(key: string, current: Obj, next: Obj) {
    const m = meta(next);
    const old = meta(current);
    for (const f of ["uid", "creationTimestamp", "ownerReferences", "generateName", "finalizers"]) {
      if (old[f] !== undefined) m[f] = old[f];
      else Reflect.deleteProperty(m, f);
    }
    if ("status" in current) next.status = current.status;
    if (JSON.stringify(next.spec) !== JSON.stringify(current.spec)) {
      m.generation = Number(old.generation ?? 1) + 1;
    } else if (old.generation !== undefined) {
      m.generation = old.generation;
    }
    delete m.managedFields;
    const objs = this.objects(key);
    const i = objs.indexOf(current);
    if (i >= 0) objs[i] = next;
    this.transaction(() => {
      this.put(key, next);
      if (key === K.pods) {
        // A pod's spec can't change much in place; the image can, and the kubelet restarts it.
        const sim = this.simOf(next);
        if (sim.state === "running" && this.isBroken({ spec: child(next, "spec") }))
          this.crash(next, this.now());
      }
      this.reconcileAll();
    });
    return next;
  }

  helmReleases() {
    return HELM_RELEASES;
  }
}
