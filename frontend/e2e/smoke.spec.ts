import { expect, test, type Page } from "@playwright/test";

import * as fx from "../src/test/fixtures";

/** Answers the API like the Go server and keeps the stream open. */
async function fakeBackend(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const ns = url.searchParams.get("namespace") ?? "";
    const body: Record<string, unknown> = {
      "/api/v1/session": fx.session,
      "/api/v1/clusters": { clusters: fx.clusters },
      "/api/v1/clusters/default/namespaces": { namespaces: fx.namespaces },
      "/api/v1/clusters/default/overview": fx.overview(ns),
      "/api/v1/clusters/default/events": { events: fx.events() },
      "/api/v1/clusters/default/search": fx.search(url.searchParams.get("q") ?? ""),
    };
    // Any resource list gets a small table, so list pages render too.
    const json =
      body[url.pathname] ??
      (url.pathname.startsWith("/api/v1/clusters/default/resources/") ? fx.podTable() : undefined);
    await (json === undefined
      ? route.fulfill({ status: 404, json: { error: { code: "not_found", message: "nope" } } })
      : route.fulfill({ json }));
  });
  // The live stream and every list watch.
  await page.routeWebSocket(/\/api\/v1\/clusters\/default\/(stream|watch\/)/, (ws) => {
    ws.send(JSON.stringify({ type: "ready" }));
  });
}

test("Overview renders, switches to Glance and navigates with ⌘K", async ({ page }) => {
  const errors: string[] = [];
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  page.on("pageerror", (e) => errors.push(e.message));
  await fakeBackend(page);

  await page.goto("/");
  await expect(page).toHaveTitle("Overview · Glowpod");

  // Gauges, tiles, problems and events from the fixture cluster.
  const load = page.getByRole("region", { name: "Cluster load" });
  await expect(load.getByRole("meter", { name: "CPU" })).toHaveAttribute(
    "aria-valuetext",
    "CPU 62 percent, OK",
  );
  await expect(load.getByRole("meter", { name: "Network" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Deployments" })).toContainText("22 / 24 ready");
  await expect(
    page.getByRole("region", { name: "Needs attention" }).getByRole("listitem"),
  ).toHaveCount(4);
  await expect(page.getByRole("region", { name: "Recent events" })).toContainText("Live");
  await expect(page.getByRole("button", { name: "Cluster: prod-eu-1, live" })).toBeVisible();

  // Glance mode keeps the gauges and swaps the rest.
  await page.getByRole("button", { name: "Glance" }).click();
  await expect(page.getByRole("main").getByRole("region", { name: "Your apps" })).toBeVisible();
  await expect(load.getByRole("meter", { name: "CPU" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Node load" })).toHaveCount(0);

  // ⌘K palette.
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByRole("combobox", { name: "Search pages, commands and objects" }).fill("nodes");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1, name: "Nodes" })).toBeVisible();

  // ⌘K finds objects by name too, and opens them.
  await page.keyboard.press("ControlOrMeta+k");
  await page
    .getByRole("combobox", { name: "Search pages, commands and objects" })
    .fill("payments-api");
  await page.getByRole("option", { name: "payments-api, payments, Deployment" }).click();
  await expect(page).toHaveURL(/\/c\/default\/workloads\/deployments\/payments\/payments-api$/);

  // A live list page: kubectl's columns and status pills.
  await page.goto("/workloads/pods");
  const pods = page.getByRole("region", { name: "pods" });
  await expect(pods.getByRole("link", { name: "payments-api-7d9f6b-x2kq9" })).toBeVisible();
  await expect(pods.getByText("CrashLoopBackOff")).toBeVisible();
  await expect(pods.getByText("Live")).toBeVisible();

  expect(errors).toEqual([]);
});

test("Scale a deployment: the dialog, the CSRF header and the toast", async ({ page }) => {
  await fakeBackend(page);
  const web = {
    object: {
      apiVersion: "apps/v1",
      kind: "Deployment",
      metadata: { name: "web", namespace: "shop", resourceVersion: "5" },
      spec: { replicas: 2 },
    },
    yaml: "apiVersion: apps/v1\nkind: Deployment\n",
    redacted: false,
  };
  let scaled: { body: unknown; csrf: string | undefined } | undefined;
  // Registered last, so these win over the generic fake backend.
  await page.route("**/api/v1/clusters/default/resources", (route) =>
    route.fulfill({
      json: {
        types: [
          {
            group: "apps",
            version: "v1",
            resource: "deployments",
            kind: "Deployment",
            namespaced: true,
            verbs: ["get", "list", "update", "patch", "delete"],
            subresources: ["scale"],
          },
        ],
      },
    }),
  );
  await page.route("**/api/v1/clusters/default/access", (route) =>
    route.fulfill({ json: { allowed: [true, true, true, true, false] } }),
  );
  await page.route("**/resources/apps/v1/deployments/web?**", (route) =>
    route.fulfill({ json: web }),
  );
  await page.route("**/resources/apps/v1/deployments/web/scale?**", async (route) => {
    scaled = {
      body: route.request().postDataJSON() as unknown,
      csrf: await route
        .request()
        .headerValue("x-csrf-token")
        .then((v) => v ?? undefined),
    };
    await route.fulfill({ status: 204 });
  });

  await page.goto("/workloads/deployments/shop/web");
  await page.getByRole("button", { name: "Scale" }).click();
  const dialog = page.getByRole("dialog", { name: "Scale web" });
  await dialog.getByLabel("Replicas").fill("5");
  await dialog.getByRole("button", { name: "Scale to 5" }).click();

  await expect(page.getByText("Scaled web to 5 replicas.")).toBeVisible();
  await expect(dialog).toBeHidden();
  expect(scaled).toEqual({ body: { replicas: 5 }, csrf: "test-csrf" });
});
