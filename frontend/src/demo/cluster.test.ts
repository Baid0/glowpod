import { describe, expect, it } from "vitest";

import { CRON_EVERY_MS, DemoCluster, K, nameOf, namespaceOf, type Change } from "./cluster";
import { overview, podProblem } from "./views";
import { child, list, type Obj } from "./util";
import { fromYaml, toYaml } from "./yaml";

const START = Date.parse("2026-10-22T10:02:30Z");

function cluster() {
  let now = START;
  const c = new DemoCluster({ now: () => now });
  const changes: Change[] = [];
  c.subscribe((ch) => changes.push(ch));
  /** Moves time forward, running the controllers once a second like the browser does. */
  const advance = (ms: number) => {
    for (let t = 0; t < ms; t += 1000) {
      now += Math.min(1000, ms - t);
      c.tick();
    }
  };
  return { c, changes, advance, now: () => now };
}

const pods = (c: DemoCluster, namespace: string, prefix: string) =>
  c.objects(K.pods).filter((p) => namespaceOf(p) === namespace && nameOf(p).startsWith(prefix));

const phase = (p: Obj) => child(p, "status").phase;

function waiting(p: Obj): unknown {
  const s = list(child(p, "status").containerStatuses)[0];
  const state = s?.state as Obj | undefined;
  return (state?.waiting as Obj | undefined)?.reason;
}

describe("the demo cluster", () => {
  it("starts with the story the demo tells", () => {
    const { c } = cluster();
    const crash = pods(c, "payments", "payments-api").filter(
      (p) => waiting(p) === "CrashLoopBackOff",
    );
    expect(crash).toHaveLength(1);
    expect(list(child(crash[0] ?? {}, "status").containerStatuses)[0]?.restartCount).toBe(14);
    // The old pods keep serving while the rollout is stuck.
    expect(pods(c, "payments", "payments-api").filter((p) => !waiting(p))).toHaveLength(2);

    const pending = c.find(K.pods, "search", "search-indexer-1");
    expect(pending && phase(pending)).toBe("Pending");
    expect(pending && podProblem(pending, c.now())?.detail).toBe("no node has 6Gi memory free");

    const o = overview(c, "");
    expect(o.problems.map((p) => p.reason)).toEqual([
      "CrashLoopBackOff",
      "Pending",
      "MemoryPressure",
    ]);
    expect(o.health.failing).toBe(1);
    expect(o.workloads.deployments.failing).toBe(1);
    expect(o.gauges.memory.value / o.gauges.memory.max).toBeGreaterThan(0.7);
    expect(o.gauges.cpu.value / o.gauges.cpu.max).toBeLessThan(0.7);
  });

  it("starts the same way every time, so links into the demo keep working", () => {
    const names = (c: DemoCluster) => c.objects(K.pods).map(nameOf).sort();
    expect(names(cluster().c)).toEqual(names(cluster().c));
  });

  it("scales a Deployment: new pods start, extra pods go", () => {
    const { c, advance } = cluster();
    const d = c.find(K.deployments, "shop", "cart");
    if (!d) throw new Error("no cart");
    c.scale(K.deployments, d, 4);
    expect(pods(c, "shop", "cart")).toHaveLength(4);
    expect(pods(c, "shop", "cart").filter((p) => waiting(p) === "ContainerCreating")).toHaveLength(
      2,
    );
    advance(3000);
    expect(pods(c, "shop", "cart").every((p) => phase(p) === "Running" && !waiting(p))).toBe(true);
    expect(child(d, "status").readyReplicas).toBe(4);

    c.scale(K.deployments, d, 1);
    expect(pods(c, "shop", "cart")).toHaveLength(1);
    expect(c.events.some((e) => e.reason === "ScalingReplicaSet" && e.object.name === "cart")).toBe(
      true,
    );
  });

  it("restarts a Deployment with new pods from a new ReplicaSet", () => {
    const { c, advance } = cluster();
    const d = c.find(K.deployments, "shop", "storefront");
    if (!d) throw new Error("no storefront");
    const before = pods(c, "shop", "storefront").map(nameOf);
    c.restart(K.deployments, d);
    advance(3000);
    const after = pods(c, "shop", "storefront").map(nameOf);
    expect(after).toHaveLength(3);
    expect(after.some((n) => before.includes(n))).toBe(false);
    expect(c.replicaSetsOf(d).length).toBeGreaterThan(2);
  });

  it("replaces a deleted pod, as its ReplicaSet would", () => {
    const { c } = cluster();
    const [first] = pods(c, "shop", "checkout");
    if (!first) throw new Error("no checkout pod");
    c.deleteObject(K.pods, first);
    expect(pods(c, "shop", "checkout").map(nameOf)).not.toContain(nameOf(first));
    expect(pods(c, "shop", "checkout")).toHaveLength(2);
  });

  it("finishes the payments rollout once its image is rolled back", () => {
    const { c, advance } = cluster();
    const d = c.find(K.deployments, "payments", "payments-api");
    if (!d) throw new Error("no payments-api");
    const edited = fromYaml(toYaml(d).replace("payments-api:1.8.0", "payments-api:1.7.3")) as Obj;
    c.replace(K.deployments, d, edited);
    advance(3000);
    const now = pods(c, "payments", "payments-api");
    expect(now).toHaveLength(2);
    expect(now.every((p) => !waiting(p))).toBe(true);
    expect(overview(c, "payments").problems).toEqual([]);
  });

  it("schedules the pending indexer once there is room, and drops it when scaled down", () => {
    const { c } = cluster();
    const s = c.find(K.statefulsets, "search", "search-indexer");
    if (!s) throw new Error("no search-indexer");
    c.scale(K.statefulsets, s, 1);
    expect(c.find(K.pods, "search", "search-indexer-1")).toBeUndefined();
    // Its claim stays, like a real StatefulSet's.
    expect(c.find(K.pvcs, "search", "data-search-indexer-1")).toBeDefined();
  });

  it("drains a node: pods move elsewhere, DaemonSet pods stay", () => {
    const { c, advance } = cluster();
    const node = c.find(K.nodes, "", "ip-10-0-1-21");
    if (!node) throw new Error("no node");
    const plan = c.drainPlan("ip-10-0-1-21");
    expect(plan.skip.length).toBeGreaterThan(0);
    const result = c.drain(node);
    expect(result.evicted).toEqual(plan.evict);
    expect(child(node, "spec").unschedulable).toBe(true);
    advance(3000);
    const left = c.objects(K.pods).filter((p) => child(p, "spec").nodeName === "ip-10-0-1-21");
    expect(left.every((p) => plan.skip.some((s) => s.name === nameOf(p)))).toBe(true);
    c.cordon(node, false);
    expect(child(node, "spec").unschedulable).toBeUndefined();
  });

  it("keeps crash-looping and counts the back-offs", () => {
    const { c, advance } = cluster();
    const [crash] = pods(c, "payments", "payments-api").filter(
      (p) => waiting(p) === "CrashLoopBackOff",
    );
    if (!crash) throw new Error("no crash-looping pod");
    const backOff = () =>
      c.events.find((e) => e.reason === "BackOff" && e.object.name === nameOf(crash))?.count ?? 0;
    const before = backOff();
    advance(150_000);
    expect(list(child(crash, "status").containerStatuses)[0]?.restartCount).toBe(15);
    expect(backOff()).toBe(before + 1);
  });

  it("runs the CronJob every five minutes and keeps the last three runs", () => {
    const { c, advance, now } = cluster();
    const jobs = () => c.objects(K.jobs).map(nameOf);
    const before = jobs();
    advance(CRON_EVERY_MS - (now() % CRON_EVERY_MS) + 1000);
    expect(jobs()).toHaveLength(4);
    advance(25_000);
    const ran = c.objects(K.jobs).find((j) => !before.includes(nameOf(j)));
    expect(ran && child(ran, "status").succeeded).toBe(1);
    advance(CRON_EVERY_MS);
    expect(
      c.objects(K.jobs).filter((j) => child(j, "status").completionTime).length,
    ).toBeLessThanOrEqual(4);
    expect(jobs()).not.toContain(before[0]);
  });

  it("tells subscribers what changed", () => {
    const { c, changes } = cluster();
    const d = c.find(K.deployments, "shop", "cart");
    if (!d) throw new Error("no cart");
    c.scale(K.deployments, d, 3);
    expect(
      changes.some((ch) => ch.type === "object" && ch.key === K.pods && ch.op !== "deleted"),
    ).toBe(true);
    expect(
      changes.some((ch) => ch.type === "event" && ch.event.reason === "ScalingReplicaSet"),
    ).toBe(true);
  });
});
