// The demo's stand-in for the Go server: answers the same REST routes from the in-memory cluster.
// Nothing here talks to a network.

import type { ApplyItem, ApplyResult, CreateResult, Session, UpdateResult } from "../lib/types";
import { DemoCluster, DemoError, K, meta, nameOf, resourceKey, type Clock } from "./cluster";
import { crashLines, logKindFor, logLine, startLines } from "./logs";
import { child, isObj, list, Random, text, type Obj } from "./util";
import {
  appDetail,
  apps,
  CLUSTER_INFO,
  events,
  helmRelease,
  helmReleases,
  nodeDetail,
  objectView,
  overview,
  podDetail,
  resourceMap,
  resourceTypes,
  table,
  typeFor,
} from "./views";
import { fromYaml, toYaml, YamlError } from "./yaml";

export const DEMO_USER = "demo";

export const session: Session = {
  version: "0.1.4",
  mode: "local",
  method: "kubeconfig",
  csrfToken: "demo",
  user: { username: DEMO_USER, groups: ["demo:viewers-and-tinkerers"] },
  clusters: [CLUSTER_INFO],
};

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function fail(status: number, code: string, message: string, detail?: string): Response {
  return json({ error: { code, message, ...(detail ? { detail } : {}) } }, status);
}

/** The log a container has written so far: a tail of lines with timestamps. */
export function logTail(
  c: DemoCluster,
  namespace: string,
  pod: string,
  container: string,
  previous: boolean,
  count: number,
): { lines: { t: string; x: string }[]; ended: boolean } {
  const p = c.find(K.pods, namespace, pod);
  if (!p) throw new DemoError(404, "not_found", `There's no pod called ${pod} in ${namespace}.`);
  const ctrs = list(child(p, "spec").containers);
  const ctr = ctrs.find((x) => x.name === container) ?? ctrs[0];
  const status = list(child(p, "status").containerStatuses).find((s) => s.name === ctr?.name);
  const image = text(ctr?.image) ?? "";
  const state = isObj(status?.state) ? status.state : {};
  const last =
    isObj(status?.lastState) && isObj(status.lastState.terminated)
      ? status.lastState.terminated
      : undefined;
  if (previous) {
    if (!last)
      throw new DemoError(400, "no_previous", "There's no previous run of this container yet.");
    const end = Date.parse(String(last.finishedAt));
    const lines = crashOrRun(image, end - 35, end, count);
    return { lines, ended: true };
  }
  if (!status || (isObj(state.waiting) && state.waiting.reason !== "CrashLoopBackOff")) {
    throw new DemoError(
      400,
      "not_started",
      "This container hasn't started yet, so there are no logs.",
    );
  }
  if (isObj(state.waiting) && last) {
    // Crash-looping: what the last run wrote before it fell over.
    const end = Date.parse(String(last.finishedAt));
    return { lines: crashOrRun(image, end - 35, end, count), ended: true };
  }
  const running = isObj(state.running)
    ? state.running
    : isObj(state.terminated)
      ? state.terminated
      : {};
  const started = Date.parse(String(running.startedAt ?? meta(p).creationTimestamp));
  const end = isObj(state.terminated) ? Date.parse(String(state.terminated.finishedAt)) : c.now();
  return { lines: crashOrRun(image, started, end, count), ended: isObj(state.terminated) };
}

function crashOrRun(image: string, from: number, to: number, count: number) {
  if (image.endsWith("payments-api:1.8.0")) {
    return crashLines(from).map((x, i) => ({ t: new Date(from + i * 7).toISOString(), x }));
  }
  const kind = logKindFor(image);
  const first = startLines(kind, from).map((x, i) => ({
    t: new Date(from + i * 200).toISOString(),
    x,
  }));
  // Lines are spread over the run; a seed from the image keeps a refresh showing the same past.
  const r = new Random(
    Array.from(image).reduce((h, ch) => (h * 31 + ch.charCodeAt(0)) | 0, 7) ^
      Math.floor(from / 1000),
  );
  const span = Math.max(0, to - from - 1000);
  const n = Math.min(count, Math.max(0, Math.floor(span / 900)));
  const lines: { t: string; x: string }[] = [];
  for (let i = 0; i < n; i++) {
    const at = to - Math.floor(((n - i) * span) / (n + 1)) - r.int(400);
    lines.push({ t: new Date(at).toISOString(), x: logLine(kind, at, r) });
  }
  return (n < count ? first : []).concat(lines).slice(-count);
}

export class DemoServer {
  readonly cluster: DemoCluster;
  private timer: ReturnType<typeof setInterval> | undefined;

  constructor(clock: Clock = { now: () => Date.now() }) {
    this.cluster = new DemoCluster(clock);
  }

  /** Runs the cluster's controllers once a second. */
  start() {
    this.timer ??= setInterval(() => {
      this.cluster.tick();
    }, 1000);
  }

  stop() {
    clearInterval(this.timer);
    this.timer = undefined;
  }

  handle(method: string, url: URL, body?: string): Response {
    try {
      return this.route(method, url, body);
    } catch (err) {
      if (err instanceof DemoError) return fail(err.status, err.code, err.message, err.detail);
      if (err instanceof YamlError)
        return fail(400, "bad_manifest", `This isn't valid YAML: ${err.message}`);
      return fail(
        500,
        "demo_error",
        "Something went wrong in the demo.",
        err instanceof Error ? err.message : undefined,
      );
    }
  }

  private route(method: string, url: URL, body?: string): Response {
    const c = this.cluster;
    const q = url.searchParams;
    const ns = q.get("namespace") ?? "";
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const input = (): Obj => {
      try {
        const v: unknown = JSON.parse(body ?? "{}");
        return isObj(v) ? v : {};
      } catch {
        return {};
      }
    };

    if (path === "/session") return json(session);
    if (path === "/auth/methods") return json({ methods: [] });
    if (path === "/auth/logout") return new Response(null, { status: 204 });

    const m = /^\/clusters\/([^/]+)\/(.*)$/.exec(path);
    if (!m) return fail(404, "not_found", "That doesn't exist.");
    if (m[1] !== "default") return fail(404, "not_found", "There's no such cluster.");
    const parts = (m[2] ?? "").split("/").map(decodeURIComponent);
    const [head, a = "", b = "", d = "", e = "", f = ""] = parts;

    switch (head) {
      case "namespaces":
        return json({ namespaces: c.objects(K.namespaces).map(nameOf).sort() });
      case "overview":
        return json(overview(c, ns));
      case "events": {
        const limit = Number(q.get("limit") ?? 100);
        return json({
          events: events(c, ns, limit, q.get("kind") ?? undefined, q.get("name") ?? undefined),
        });
      }
      case "apps":
        if (a) return json(appDetail(c, a, b));
        return json({ apps: apps(c, ns) });
      case "pods":
        return json(podDetail(c, a, b));
      case "helm":
        if (a) return json(helmRelease(c, a, b));
        return json({ releases: helmReleases(c, ns) });
      case "map":
        return json(resourceMap(c, ns));
      case "access": {
        const checks = list(input().checks);
        return json({ allowed: checks.map(() => true) });
      }
      case "nodes":
        return this.nodes(method, a, b, input);
      case "apply": {
        if (method !== "POST") break;
        const yaml = text(input().yaml) ?? "";
        return json(this.apply(ns, yaml, q.get("dryRun") === "true"));
      }
      case "logs": {
        if (parts.at(-1) !== "download") break;
        const container = q.get("container") ?? "";
        const { lines } = logTail(c, a, b, container, q.get("previous") === "true", 2000);
        return new Response(lines.map((l) => l.x).join("\n") + "\n", {
          headers: { "Content-Type": "text/plain; charset=utf-8" },
        });
      }
      case "resources": {
        if (!a) return json({ types: resourceTypes(c) });
        const group = a === "core" ? "" : a;
        const resource = d;
        if (!e && method === "POST") {
          const yaml = text(input().yaml) ?? "";
          return json(this.create(group, b, resource, ns, yaml, q.get("dryRun") === "true"), 201);
        }
        if (!e) {
          return json(
            table(c, group, resource, {
              namespace: ns,
              labelSelector: q.get("labelSelector") ?? "",
              fieldSelector: q.get("fieldSelector") ?? "",
              limit: Number(q.get("limit") ?? 0),
              continue: q.get("continue") ?? "",
            }),
          );
        }
        return this.object(method, group, b, resource, e, f, ns, q, input);
      }
    }
    return fail(404, "not_found", "That doesn't exist.");
  }

  private nodes(method: string, name: string, action: string, input: () => Obj): Response {
    const c = this.cluster;
    const node = c.find(K.nodes, "", name);
    if (!node) throw new DemoError(404, "not_found", `There's no node called ${name}.`);
    if (!action) return json(nodeDetail(c, name));
    if (action === "cordon" && method === "POST") {
      c.cordon(node, input().cordon === true);
      return new Response(null, { status: 204 });
    }
    if (action === "drain") {
      if (method === "POST") return json(c.drain(node));
      return json(c.drainPlan(name));
    }
    return fail(404, "not_found", "That doesn't exist.");
  }

  private object(
    method: string,
    group: string,
    version: string,
    resource: string,
    name: string,
    sub: string,
    ns: string,
    q: URLSearchParams,
    input: () => Obj,
  ): Response {
    const c = this.cluster;
    const type = typeFor(c, group, resource);
    const key = resourceKey(group, resource);
    const namespace = type.namespaced ? ns : "";
    if (method === "GET")
      return json(objectView(c, group, resource, namespace, name, q.get("reveal") === "true"));
    const obj = c.find(key, namespace, name);
    if (!obj) throw new DemoError(404, "not_found", "That doesn't exist anymore.");

    if (method === "DELETE") {
      c.deleteObject(key, obj);
      return new Response(null, { status: 204 });
    }
    if (sub === "scale" && method === "PUT") {
      const replicas = Number(input().replicas);
      if (!Number.isInteger(replicas) || replicas < 0)
        throw new DemoError(400, "bad_request", "Replicas must be a whole number, 0 or more.");
      if (!type.subresources?.includes("scale"))
        throw new DemoError(400, "not_scalable", `A ${type.kind} can't be scaled.`);
      c.scale(key, obj, replicas);
      return new Response(null, { status: 204 });
    }
    if (sub === "restart" && method === "POST") {
      if (group !== "apps" || resource === "replicasets")
        throw new DemoError(400, "not_restartable", `A ${type.kind} can't be restarted.`);
      c.restart(key, obj);
      return new Response(null, { status: 204 });
    }
    if (method === "PUT" && !sub) {
      const yaml = text(input().yaml) ?? "";
      return json(
        this.update(key, obj, type, namespace, name, version, yaml, q.get("dryRun") === "true"),
      );
    }
    return fail(405, "method_not_allowed", "That can't be done to this object.");
  }

  private update(
    key: string,
    current: Obj,
    type: { group: string; version: string; kind: string; namespaced: boolean },
    namespace: string,
    name: string,
    version: string,
    yaml: string,
    dryRun: boolean,
  ): UpdateResult {
    const c = this.cluster;
    if (yaml.split(/^---\s*$/m).filter((d) => d.trim()).length !== 1) {
      throw new DemoError(400, "bad_manifest", "Send exactly one object.");
    }
    const parsed = fromYaml(yaml);
    if (!isObj(parsed)) throw new DemoError(400, "bad_manifest", "This YAML isn't an object.");
    const apiVersion = type.group ? `${type.group}/${version}` : version;
    const m = isObj(parsed.metadata) ? parsed.metadata : {};
    if (parsed.apiVersion !== apiVersion)
      throw new DemoError(400, "identity_changed", `apiVersion must stay ${apiVersion}.`);
    if (parsed.kind !== type.kind)
      throw new DemoError(400, "identity_changed", `kind must stay ${type.kind}.`);
    if (m.name !== name)
      throw new DemoError(
        400,
        "identity_changed",
        `metadata.name must stay ${name}. To rename, create a copy instead.`,
      );
    if (type.namespaced && m.namespace && m.namespace !== namespace)
      throw new DemoError(400, "identity_changed", `metadata.namespace must stay ${namespace}.`);
    if (!m.resourceVersion)
      throw new DemoError(
        400,
        "no_resource_version",
        "Keep metadata.resourceVersion so Glowpod can tell if someone else changed this.",
      );
    if (type.namespaced) m.namespace = namespace;
    parsed.metadata = m;
    if (key === K.secrets) {
      const data = isObj(parsed.data) ? parsed.data : {};
      const old = isObj(current.data) ? current.data : {};
      if (Object.keys(old).length > 0 && Object.keys(data).every((k) => data[k] === "")) {
        throw new DemoError(
          400,
          "secret_hidden",
          "Reveal this Secret's values before editing it, or the saved values would be blank.",
        );
      }
    }
    const replicas = isObj(parsed.spec) ? parsed.spec.replicas : undefined;
    if (
      replicas !== undefined &&
      (typeof replicas !== "number" || replicas < 0 || !Number.isInteger(replicas))
    ) {
      throw new DemoError(
        422,
        "invalid",
        "The cluster didn't accept this. Fix the fields below and try again.",
        `${type.kind}.apps "${name}" is invalid: spec.replicas: Invalid value: ${JSON.stringify(replicas)}: must be a whole number, 0 or more`,
      );
    }

    if (dryRun) {
      const after = structuredClone(parsed);
      const am = meta(after);
      const cm = meta(current);
      for (const f of [
        "uid",
        "creationTimestamp",
        "ownerReferences",
        "generateName",
        "finalizers",
        "resourceVersion",
      ]) {
        if (cm[f] !== undefined) am[f] = cm[f];
      }
      if ("status" in current) after.status = current.status;
      const hide = (o: Obj) => {
        if (key !== K.secrets) return o;
        const copy = structuredClone(o);
        if (isObj(copy.data)) for (const k of Object.keys(copy.data)) copy.data[k] = "";
        return copy;
      };
      return { dryRun: true, before: toYaml(hide(current)), after: toYaml(hide(after)) };
    }
    c.replace(key, current, parsed);
    return {
      dryRun: false,
      object: objectView(c, type.group, resourceNameOf(key), namespace, name, false),
    };
  }

  private create(
    group: string,
    version: string,
    resource: string,
    namespace: string,
    yaml: string,
    dryRun: boolean,
  ): CreateResult {
    const c = this.cluster;
    const type = typeFor(c, group, resource);
    if (yaml.split(/^---\s*$/m).filter((d) => d.trim()).length !== 1) {
      throw new DemoError(400, "bad_manifest", "Send exactly one object.");
    }
    const obj = newObject(c, { ...type, version }, this.parse(yaml), namespace);
    const ns = text(meta(obj).namespace) ?? "";
    const key = resourceKey(type.group, resource);
    if (c.find(key, ns, nameOf(obj))) {
      throw new DemoError(409, "already_exists", "Something with that name already exists.");
    }
    if (dryRun) {
      const shown = structuredClone(obj);
      if (key === K.secrets) for (const f of ["data", "stringData"]) hideValues(shown, f);
      return {
        dryRun: true,
        object: { object: shown, yaml: toYaml(shown), redacted: key === K.secrets },
      };
    }
    c.put(key, obj);
    return {
      dryRun: false,
      object: objectView(c, type.group, resource, type.namespaced ? ns : "", nameOf(obj), false),
    };
  }

  private apply(namespace: string, yaml: string, dryRun: boolean): ApplyResult {
    const c = this.cluster;
    const docs = yaml
      .split(/^---\s*$/m)
      .filter((d) => d.split("\n").some((l) => l.trim() && !l.trim().startsWith("#")));
    if (docs.length === 0) throw new DemoError(400, "bad_manifest", "There's nothing to apply.");
    if (docs.length > 50) {
      throw new DemoError(400, "too_many_objects", "Apply up to 50 objects at a time.");
    }
    const items = docs.map((doc): ApplyItem => {
      let parsed: unknown;
      try {
        parsed = fromYaml(doc);
      } catch (err) {
        const message = err instanceof YamlError ? err.message : "This isn't valid YAML.";
        return {
          result: "failed",
          code: "bad_manifest",
          message: `This isn't valid YAML: ${message}`,
        };
      }
      if (!isObj(parsed))
        return { result: "failed", code: "bad_manifest", message: "This YAML isn't an object." };
      const kind = text(parsed.kind) ?? "";
      const apiVersion = text(parsed.apiVersion) ?? "";
      const m = isObj(parsed.metadata) ? parsed.metadata : {};
      const item: ApplyItem = { apiVersion, kind, name: text(m.name) ?? "", result: "failed" };
      const [g, v] = apiVersion.includes("/") ? apiVersion.split("/") : ["", apiVersion];
      const type = resourceTypes(c).find((t) => t.group === g && t.kind === kind);
      if (!type || !v) {
        return {
          ...item,
          code: "unknown_kind",
          message: `This cluster doesn't have ${apiVersion} ${kind}.`,
        };
      }
      if (!item.name) {
        return { ...item, code: "name_required", message: "Give it a name: metadata.name." };
      }
      item.namespace = type.namespaced ? orDefault(text(m.namespace), namespace) : "";
      Object.assign(item, {
        group: type.group || "core",
        version: type.version,
        resource: type.resource,
      });
      const key = resourceKey(type.group, type.resource);
      const current = c.find(key, item.namespace, item.name);
      if (!current) {
        if (!dryRun) {
          const obj = newObject(
            c,
            type,
            { ...parsed, apiVersion: type.group ? `${type.group}/${type.version}` : type.version },
            item.namespace,
          );
          c.put(key, obj);
        }
        return { ...item, result: "created" };
      }
      const next = { ...structuredClone(current), ...parsed, metadata: { ...meta(current), ...m } };
      const same = JSON.stringify(next) === JSON.stringify(current);
      if (!dryRun && !same) c.replace(key, current, next);
      return { ...item, result: same ? "unchanged" : "configured" };
    });
    return { dryRun, items };
  }

  private parse(yaml: string): unknown {
    try {
      return fromYaml(yaml);
    } catch (err) {
      const message = err instanceof YamlError ? err.message : "This isn't valid YAML.";
      throw new DemoError(400, "bad_manifest", `This isn't valid YAML: ${message}`);
    }
  }
}

/** The object's own namespace, else the page's, else "default"; "" counts as unset. */
function orDefault(own: string | undefined, page: string): string {
  if (own) return own;
  return page ? page : "default";
}

function hideValues(obj: Obj, field: string) {
  const values = obj[field];
  if (isObj(values)) for (const k of Object.keys(values)) values[k] = "";
}

/** The demo's create: one object of the URL's type, like the real API. */
function newObject(
  c: DemoCluster,
  type: { group: string; version: string; kind: string; namespaced: boolean },
  parsed: unknown,
  namespace: string,
): Obj {
  if (!isObj(parsed)) throw new DemoError(400, "bad_manifest", "This YAML isn't an object.");
  const apiVersion = type.group ? `${type.group}/${type.version}` : type.version;
  if (parsed.apiVersion !== apiVersion || parsed.kind !== type.kind) {
    throw new DemoError(
      400,
      "wrong_kind",
      `This creates a ${type.kind}: use apiVersion: ${apiVersion} and kind: ${type.kind}, or use Apply YAML for other kinds.`,
    );
  }
  const m = isObj(parsed.metadata) ? parsed.metadata : {};
  if (!text(m.name)) throw new DemoError(400, "name_required", "Give it a name: metadata.name.");
  if (type.namespaced) m.namespace = orDefault(text(m.namespace), namespace);
  else Reflect.deleteProperty(m, "namespace");
  for (const f of ["resourceVersion", "uid", "creationTimestamp", "generation", "managedFields"]) {
    Reflect.deleteProperty(m, f);
  }
  m.uid = c.uid();
  m.creationTimestamp = new Date(c.now()).toISOString().replace(/\.\d+Z$/, "Z");
  m.generation = 1;
  parsed.metadata = m;
  Reflect.deleteProperty(parsed, "status");
  return parsed;
}

function resourceNameOf(key: string): string {
  return key.slice(key.indexOf("/") + 1);
}
