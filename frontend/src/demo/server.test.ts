import { describe, expect, it } from "vitest";

import type {
  ApplyResult,
  CreateResult,
  HelmReleaseDetail,
  NodeDetail,
  Overview,
  PodDetail,
  ResourceMap,
  ResourceObject,
  ResourceTable,
  UpdateResult,
} from "../lib/types";
import { DemoServer } from "./server";

const START = Date.parse("2026-10-22T10:02:30Z");

function server() {
  let now = START;
  const s = new DemoServer({ now: () => now });
  // The caller names the body it expects, like apiGet<T> does.
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-parameters
  const call = async <T>(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<{ status: number; body: T; text: string }> => {
    const res = s.handle(
      method,
      new URL(path, "https://glowpod.dev"),
      body === undefined ? undefined : JSON.stringify(body),
    );
    const text = await res.text();
    const json = res.headers.get("Content-Type")?.includes("json") ?? false;
    return { status: res.status, body: (json ? JSON.parse(text) : undefined) as T, text };
  };
  const get = async <T>(path: string): Promise<T> => (await call<T>("GET", path)).body;
  const later = (ms: number) => {
    now += ms;
  };
  return { s, call, get, later };
}

const API = "/api/v1/clusters/default";

describe("the demo API", () => {
  it("is signed in as the demo user, with no sign-in methods", async () => {
    const { get } = server();
    expect(await get("/api/v1/session")).toMatchObject({
      user: { username: "demo" },
      mode: "local",
    });
    expect(await get("/api/v1/auth/methods")).toEqual({ methods: [] });
  });

  it("answers the Overview, with gauges that move a little over time", async () => {
    const { get, later } = server();
    const first = await get<Overview>(`${API}/overview`);
    later(60_000);
    const next = await get<Overview>(`${API}/overview`);
    for (const g of [first.gauges.cpu, first.gauges.memory, first.gauges.network]) {
      expect(g?.state).toBe("live");
      expect(g?.value).toBeGreaterThan(0);
      expect(g?.value).toBeLessThan(g?.max ?? 0);
    }
    expect(next.gauges.cpu.value).not.toBe(first.gauges.cpu.value);
    expect(
      Math.abs(next.gauges.cpu.value - first.gauges.cpu.value) / first.gauges.cpu.max,
    ).toBeLessThan(0.05);
    expect(first.cluster.name).toBe("demo-cluster");
  });

  it("scopes the Overview to the picked namespaces", async () => {
    const { get } = server();
    const shop = await get<Overview>(`${API}/overview?namespace=shop`);
    expect(shop.problems).toEqual([]);
    expect(shop.apps.every((a) => a.namespace === "shop")).toBe(true);
  });

  it("lists any resource as a Table, page by page, with label selectors", async () => {
    const { get } = server();
    const page = await get<ResourceTable>(`${API}/resources/core/v1/pods?limit=10`);
    expect(page.columns.map((c) => c.name).slice(0, 5)).toEqual([
      "Name",
      "Ready",
      "Status",
      "Restarts",
      "Age",
    ]);
    expect(page.rows).toHaveLength(10);
    expect(page.continue).toBe("10");
    const next = await get<ResourceTable>(`${API}/resources/core/v1/pods?limit=10&continue=10`);
    expect(next.rows[0]?.object.name).not.toBe(page.rows[0]?.object.name);

    const cart = await get<ResourceTable>(
      `${API}/resources/core/v1/pods?namespace=shop&labelSelector=${encodeURIComponent("app.kubernetes.io/name=cart")}`,
    );
    expect(cart.rows.map((r) => r.object.name).every((n) => n.startsWith("cart-"))).toBe(true);
    expect(cart.rows).toHaveLength(2);

    const crds = await get<ResourceTable>(
      `${API}/resources/cert-manager.io/v1/certificates?namespace=shop`,
    );
    expect(crds.rows.map((r) => r.cells[1])).toEqual(["True"]);
  });

  it("hides Secret values until they're revealed", async () => {
    const { get } = server();
    const path = `${API}/resources/core/v1/secrets/payments-db?namespace=payments&format=yaml`;
    const hidden = await get<ResourceObject>(path);
    expect(hidden.redacted).toBe(true);
    expect(hidden.object.data).toEqual({ database: "", password: "", username: "" });
    const shown = await get<ResourceObject>(path + "&reveal=true");
    expect(shown.redacted).toBe(false);
    expect(atob(shown.object.data?.username ?? "")).toBe("payments");
  });

  it("dry-runs and applies YAML edits, and refuses renames and bad YAML", async () => {
    const { call, get } = server();
    const path = `${API}/resources/apps/v1/deployments/cart?namespace=shop`;
    const { yaml } = await get<ResourceObject>(path + "&format=yaml");

    const dry = await call<UpdateResult>("PUT", path + "&dryRun=true", {
      yaml: yaml.replace("replicas: 2", "replicas: 3"),
    });
    expect(dry.status).toBe(200);
    expect(dry.body.before).toContain("replicas: 2");
    expect(dry.body.after).toContain("replicas: 3");
    expect(
      (
        await get<ResourceTable>(
          `${API}/resources/core/v1/pods?namespace=shop&labelSelector=app.kubernetes.io/name=cart`,
        )
      ).rows,
    ).toHaveLength(2);

    const saved = await call<UpdateResult>("PUT", path, {
      yaml: yaml.replace("replicas: 2", "replicas: 3"),
    });
    expect(saved.body.object?.yaml).toContain("replicas: 3");
    expect(
      (
        await get<ResourceTable>(
          `${API}/resources/core/v1/pods?namespace=shop&labelSelector=app.kubernetes.io/name=cart`,
        )
      ).rows,
    ).toHaveLength(3);

    const renamed = await call<{ error: { code: string } }>("PUT", path, {
      yaml: yaml.replace(/^ {2}name: cart$/m, "  name: basket"),
    });
    expect(renamed.status).toBe(400);
    expect(renamed.body.error.code).toBe("identity_changed");

    const broken = await call<{ error: { code: string; message: string } }>("PUT", path, {
      yaml: "kind: [oops\n",
    });
    expect(broken.body.error.code).toBe("bad_manifest");
  });

  it("creates objects after a dry run, and applies multi-document YAML", async () => {
    const { call, get } = server();
    const cm = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: flags\ndata:\n  beta: 'on'\n";
    const list = async () =>
      (await get<ResourceTable>(`${API}/resources/core/v1/configmaps?namespace=shop`)).rows.map(
        (r) => r.object.name,
      );

    const dry = await call<CreateResult>(
      "POST",
      `${API}/resources/core/v1/configmaps?namespace=shop&dryRun=true`,
      { yaml: cm },
    );
    expect(dry.status).toBe(201);
    expect(dry.body.dryRun).toBe(true);
    expect(await list()).not.toContain("flags");

    const made = await call<CreateResult>(
      "POST",
      `${API}/resources/core/v1/configmaps?namespace=shop`,
      { yaml: cm },
    );
    expect(made.body.object.object.metadata).toMatchObject({ name: "flags", namespace: "shop" });
    expect(await list()).toContain("flags");

    const again = await call<{ error: { code: string } }>(
      "POST",
      `${API}/resources/core/v1/configmaps?namespace=shop`,
      { yaml: cm },
    );
    expect(again.status).toBe(409);
    const wrong = await call<{ error: { code: string } }>(
      "POST",
      `${API}/resources/apps/v1/deployments?namespace=shop`,
      { yaml: cm },
    );
    expect(wrong.body.error.code).toBe("wrong_kind");

    const manifest = `${cm}---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: more\n---\napiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w\n`;
    const plan = await call<ApplyResult>("POST", `${API}/apply?namespace=shop&dryRun=true`, {
      yaml: manifest,
    });
    expect(plan.body.items.map((it) => `${it.name ?? ""}:${it.result}`)).toEqual([
      "flags:unchanged",
      "more:created",
      "w:failed",
    ]);
    expect(await list()).not.toContain("more");

    const applied = await call<ApplyResult>("POST", `${API}/apply?namespace=shop`, {
      yaml: manifest.replace("'on'", "'off'"),
    });
    expect(applied.body.items.map((it) => `${it.name ?? ""}:${it.result}`)).toEqual([
      "flags:configured",
      "more:created",
      "w:failed",
    ]);
    expect(await list()).toEqual(expect.arrayContaining(["flags", "more"]));
  });

  it("won't save a Secret whose values are still hidden", async () => {
    const { call, get } = server();
    const path = `${API}/resources/core/v1/secrets/payments-db?namespace=payments`;
    const { yaml } = await get<ResourceObject>(path + "&format=yaml");
    const res = await call<{ error: { code: string } }>("PUT", path, { yaml });
    expect(res.body.error.code).toBe("secret_hidden");
  });

  it("scales, restarts and deletes", async () => {
    const { call, get } = server();
    const deploy = `${API}/resources/apps/v1/deployments/storefront`;
    expect((await call("PUT", `${deploy}/scale?namespace=shop`, { replicas: 5 })).status).toBe(204);
    expect((await call("POST", `${deploy}/restart?namespace=shop`)).status).toBe(204);
    expect(
      (
        await call(
          "PUT",
          `${API}/resources/core/v1/configmaps/storefront-config/scale?namespace=shop`,
          { replicas: 2 },
        )
      ).status,
    ).toBe(400);
    expect((await call("DELETE", `${deploy}?namespace=shop`)).status).toBe(204);
    const pods = await get<ResourceTable>(`${API}/resources/core/v1/pods?namespace=shop`);
    expect(pods.rows.some((r) => r.object.name.startsWith("storefront-"))).toBe(false);
    expect((await call("GET", `${deploy}?namespace=shop&format=yaml`)).status).toBe(404);
  });

  it("answers pod, node, app, Helm and map pages", async () => {
    const { get, call } = server();
    const pod = await get<PodDetail>(`${API}/pods/search/search-indexer-1`);
    expect(pod.pod.health).toBe("pending");
    expect(pod.story.map((s) => s.title)).toContain("Waiting for a node");

    const node = await get<NodeDetail>(`${API}/nodes/ip-10-0-2-14`);
    expect(node.conditions.find((c) => c.type === "MemoryPressure")?.problem).toBe(true);
    expect(node.pods.length).toBeGreaterThan(0);

    const helm = await get<HelmReleaseDetail>(`${API}/helm/payments/payments`);
    expect(helm.status).toBe("failed");
    expect(helm.history[0]?.status).toBe("failed");
    expect(helm.objects.map((o) => o.name)).toContain("payments-api");

    const map = await get<ResourceMap>(`${API}/map?namespace=shop`);
    expect(map.edges).toContainEqual({ from: "Ingress/storefront", to: "Service/storefront" });
    expect(map.edges).toContainEqual({ from: "Service/storefront", to: "Deployment/storefront" });
    expect(map.edges).toContainEqual({
      from: "Deployment/storefront",
      to: "ConfigMap/storefront-config",
    });

    expect((await call("GET", `${API}/pods/shop/nope`)).status).toBe(404);
  });

  it("lets the demo user do everything, and downloads logs as text", async () => {
    const { call, get } = server();
    const access = await call<{ allowed: boolean[] }>("POST", `${API}/access`, {
      checks: [
        { verb: "delete", group: "apps", resource: "deployments" },
        { verb: "get", group: "", resource: "secrets" },
      ],
    });
    expect(access.body.allowed).toEqual([true, true]);
    const pods = await get<ResourceTable>(
      `${API}/resources/core/v1/pods?namespace=search&labelSelector=app.kubernetes.io/name=search-api`,
    );
    const name = pods.rows[0]?.object.name ?? "";
    const log = await call("GET", `${API}/logs/search/${name}/download?container=api`);
    expect(log.status).toBe(200);
    expect(log.text.split("\n").length).toBeGreaterThan(50);
    expect(log.text).toMatch(/"msg":"search"/);
  });
});
