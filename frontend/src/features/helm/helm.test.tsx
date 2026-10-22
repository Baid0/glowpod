import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { HelmRelease, HelmReleaseDetail } from "../../lib/types";
import { renderApp } from "../../test/renderApp";
import { helmTone, helmWord, objectPath } from "./status";

const BASE = "/api/v1/clusters/default";
const web: HelmRelease = {
  name: "web",
  namespace: "shop",
  revision: 3,
  status: "deployed",
  chart: "web",
  chartVersion: "1.2.0",
  appVersion: "2.4.1",
  updated: new Date(Date.now() - 3_600_000).toISOString(),
  description: "Upgrade complete",
};

describe("helm pages", () => {
  it("lists releases with status and chart", async () => {
    renderApp("/extensions/helm", (url) =>
      url.pathname === `${BASE}/helm`
        ? Response.json({
            releases: [web, { ...web, name: "backup", namespace: "ops", status: "failed" }],
          })
        : undefined,
    );
    const row = await screen.findByRole("row", { name: /^web shop/ });
    expect(row).toHaveTextContent("Deployed");
    expect(row).toHaveTextContent("web-1.2.0");
    expect(within(row).getByRole("link", { name: "web" })).toHaveAttribute(
      "href",
      "/c/default/extensions/helm/shop/web",
    );
    expect(screen.getByRole("row", { name: /backup/ })).toHaveTextContent("Failed");
  });

  it("explains that releases need Secret access", async () => {
    renderApp("/extensions/helm", (url) =>
      url.pathname === `${BASE}/helm`
        ? Response.json({ error: { code: "forbidden", message: "x" } }, { status: 403 })
        : undefined,
    );
    expect(
      await screen.findByText("You can't read this. Ask an admin for access."),
    ).toBeInTheDocument();
  });

  it("shows a release's objects and history, never values", async () => {
    const detail: HelmReleaseDetail = {
      ...web,
      history: [web, { ...web, revision: 2, status: "superseded", chartVersion: "1.1.0" }],
      objects: [
        { apiVersion: "apps/v1", kind: "Deployment", namespace: "shop", name: "web" },
        { apiVersion: "networking.k8s.io/v1", kind: "Ingress", namespace: "shop", name: "web" },
      ],
    };
    renderApp("/extensions/helm/shop/web", (url) =>
      url.pathname === `${BASE}/helm/shop/web` ? Response.json(detail) : undefined,
    );
    const objects = await screen.findByRole("region", { name: "Installed 2 objects" });
    expect(within(objects).getAllByRole("link")[1]).toHaveAttribute(
      "href",
      "/c/default/resources/networking.k8s.io/v1/ingresses/shop/web",
    );
    expect(screen.getByRole("region", { name: "History" })).toHaveTextContent("Superseded");
    expect(screen.getByText(/Values aren.t shown/)).toBeInTheDocument();
  });
});

describe("helpers", () => {
  it.each([
    ["deployed", "ok", "Deployed"],
    ["failed", "critical", "Failed"],
    ["pending-upgrade", "high", "Pending upgrade"],
    ["superseded", "neutral", "Superseded"],
  ])("%s", (status, tone, word) => {
    expect(helmTone(status)).toBe(tone);
    expect(helmWord(status)).toBe(word);
  });

  it.each([
    [
      { apiVersion: "apps/v1", kind: "StatefulSet", namespace: "a", name: "db" },
      "/workloads/statefulsets/a/db",
    ],
    [
      { apiVersion: "v1", kind: "ConfigMap", namespace: "a", name: "c" },
      "/resources/core/v1/configmaps/a/c",
    ],
    [
      { apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", namespace: "a", name: "p" },
      "/resources/networking.k8s.io/v1/networkpolicies/a/p",
    ],
    [
      { apiVersion: "rbac.authorization.k8s.io/v1", kind: "ClusterRole", name: "r" },
      "/resources/rbac.authorization.k8s.io/v1/clusterroles/_/r",
    ],
    [
      { apiVersion: "gateway.networking.k8s.io/v1", kind: "Gateway", namespace: "a", name: "g" },
      "/resources/gateway.networking.k8s.io/v1/gateways/a/g",
    ],
  ])("objectPath %o", (o, want) => {
    expect(objectPath(o)).toBe(want);
  });
});
