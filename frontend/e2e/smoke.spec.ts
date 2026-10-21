import { expect, test, type Page } from "@playwright/test";

import * as fx from "../src/test/fixtures";

/** Answers the API like the Go server and keeps the stream open. */
async function fakeBackend(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const ns = url.searchParams.get("namespace") ?? "";
    const body: Record<string, unknown> = {
      "/api/v1/session": fx.session,
      "/api/v1/clusters/default/namespaces": { namespaces: fx.namespaces },
      "/api/v1/clusters/default/overview": fx.overview(ns),
      "/api/v1/clusters/default/events": { events: fx.events() },
    };
    const json = body[url.pathname];
    await (json === undefined
      ? route.fulfill({ status: 404, json: { error: { code: "not_found", message: "nope" } } })
      : route.fulfill({ json }));
  });
  await page.routeWebSocket(/\/api\/v1\/clusters\/default\/stream/, (ws) => {
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
  await page.getByRole("combobox", { name: "Search pages and commands" }).fill("nodes");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1, name: "Nodes" })).toBeVisible();

  expect(errors).toEqual([]);
});
