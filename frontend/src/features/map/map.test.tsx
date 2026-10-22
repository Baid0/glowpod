import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { ResourceMap } from "../../lib/types";
import { renderApp } from "../../test/renderApp";
import { fit, layout, nodePath } from "./layout";

const map: ResourceMap = {
  namespace: "shop",
  truncated: false,
  nodes: [
    {
      id: "Ingress/front",
      column: 0,
      kind: "Ingress",
      namespace: "shop",
      name: "front",
      status: "neutral",
      detail: "shop.example.com",
    },
    {
      id: "Service/web",
      column: 1,
      kind: "Service",
      namespace: "shop",
      name: "web",
      status: "neutral",
      detail: "ClusterIP",
    },
    {
      id: "Service/ghost",
      column: 1,
      kind: "Service",
      namespace: "shop",
      name: "ghost",
      status: "critical",
      detail: "doesn't exist",
      missing: true,
    },
    {
      id: "Deployment/api",
      column: 2,
      kind: "Deployment",
      namespace: "shop",
      name: "api",
      status: "ok",
      detail: "2/2 ready",
    },
    {
      id: "Deployment/web",
      column: 2,
      kind: "Deployment",
      namespace: "shop",
      name: "web",
      status: "critical",
      detail: "0/1 ready",
    },
    {
      id: "Secret/db",
      column: 3,
      kind: "Secret",
      namespace: "shop",
      name: "db",
      status: "neutral",
    },
  ],
  edges: [
    { from: "Ingress/front", to: "Service/web" },
    { from: "Ingress/front", to: "Service/ghost" },
    { from: "Service/web", to: "Deployment/web" },
    { from: "Deployment/web", to: "Secret/db" },
  ],
};

function node(id: string) {
  const n = map.nodes.find((x) => x.id === id);
  if (!n) throw new Error("no node " + id);
  return n;
}

describe("layout", () => {
  it("puts columns left to right and services next to their workloads", () => {
    const { nodes, width, height } = layout(map);
    const at = (id: string) => nodes.find((n) => n.id === id);
    expect(at("Ingress/front")?.x).toBe(0);
    expect(at("Deployment/web")?.x).toBeGreaterThan(at("Service/web")?.x ?? Infinity);
    // web's service sits level with the web deployment (row 1), above the dangling one.
    expect(at("Service/web")?.y).toBeLessThan(at("Service/ghost")?.y ?? -1);
    expect(width).toBe(4 * 216 + 3 * 72);
    expect(height).toBe(28 + 2 * 56 + 16);
  });

  it("drops empty columns", () => {
    const { nodes, width } = layout({
      ...map,
      nodes: map.nodes.filter((n) => n.column === 2),
      edges: [],
    });
    expect(nodes.every((n) => n.x === 0)).toBe(true);
    expect(width).toBe(216);
  });
});

describe("helpers", () => {
  it("links to pages, but not for things that don't exist", () => {
    expect(nodePath(node("Deployment/api"))).toBe("/workloads/deployments/shop/api");
    expect(nodePath(node("Service/web"))).toBe("/resources/core/v1/services/shop/web");
    expect(nodePath(node("Service/ghost"))).toBeUndefined();
  });

  it("shortens long names in the middle", () => {
    expect(fit("short", 10)).toBe("short");
    expect(fit("payments-api-7d9f6b-x2kq9", 12)).toBe("paymen…x2kq9");
  });
});

describe("map page", () => {
  it("asks for a namespace first", () => {
    renderApp("/network/map");
    expect(screen.getByText(/Pick a namespace in the top bar/)).toBeInTheDocument();
  });

  it("draws the namespace, highlights links on focus and opens pages", async () => {
    localStorage.setItem("glowpod.namespace", "shop");
    const { router } = renderApp("/network/map", (url) =>
      url.pathname === "/api/v1/clusters/default/map" ? Response.json(map) : undefined,
    );
    const svg = await screen.findByRole("group", { name: "Map of shop" });
    const web = within(svg).getByRole("link", { name: "Deployment web, 0/1 ready" });
    expect(
      within(svg).getByRole("img", { name: "Service ghost, doesn't exist" }),
    ).toBeInTheDocument();

    act(() => {
      web.focus();
    });
    expect(within(svg).getByRole("link", { name: /Deployment api/ })).toHaveAttribute(
      "data-dim",
      "true",
    );
    expect(within(svg).getByRole("link", { name: /Secret db/ })).toHaveAttribute(
      "data-dim",
      "false",
    );

    await userEvent.keyboard("{Enter}");
    expect(router.state.location.pathname).toBe("/workloads/deployments/shop/web");
  });
});
