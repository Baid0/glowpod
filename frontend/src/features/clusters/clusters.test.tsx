import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { samePage } from "../../app/navigation";
import { clusterHref, pathInCluster } from "../../lib/clusterPaths";
import type { ClusterInfo, ClusterSummary } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

const clusters: ClusterInfo[] = [
  { id: "kind-lab", name: "kind-lab", server: "127.0.0.1:6443", current: true },
  { id: "prod-eu-1", name: "prod-eu-1", server: "192.0.2.10:6443" },
  { id: "old-staging", name: "old-staging", server: "203.0.113.5:6443" },
  { id: "expired", name: "expired", server: "198.51.100.7:6443" },
];

function summary(id: string): ClusterSummary {
  const o = fx.overview();
  return {
    cluster: { ...o.cluster, id, name: id, nodes: 3, nodesReady: id === "prod-eu-1" ? 2 : 3 },
    gauges: { cpu: o.gauges.cpu, memory: o.gauges.memory, pods: o.gauges.pods },
    health: o.health,
    problemCount: id === "prod-eu-1" ? 4 : 0,
    thresholds: o.thresholds,
    generatedAt: o.generatedAt,
  };
}

const fail = (status: number, code: string, message: string) =>
  Response.json({ error: { code, message } }, { status });

/** Four clusters: two answer, one is down, one refuses the kubeconfig's credentials. */
function respond(url: URL): Response | undefined {
  if (url.pathname === "/api/v1/clusters") return Response.json({ clusters });
  const m = /^\/api\/v1\/clusters\/([^/]+)\/(.*)$/.exec(url.pathname);
  if (!m) return undefined;
  const [, id = "", rest] = m;
  if (id === "old-staging")
    return fail(
      503,
      "cluster_unreachable",
      "Can't reach the cluster. Check that it's running and your network.",
    );
  if (id === "expired")
    return fail(
      401,
      "cluster_unauthorized",
      "The cluster didn't accept your credentials. Sign in again.",
    );
  if (rest === "summary") return Response.json(summary(id));
  if (rest === "overview")
    return Response.json(fx.overview(url.searchParams.get("namespace") ?? ""));
  if (rest === "namespaces") return Response.json({ namespaces: fx.namespaces });
  if (rest === "events") return Response.json({ events: fx.events() });
  return undefined;
}

const row = (name: string) => {
  const link = screen.getByRole("link", { name });
  const tr = link.closest("tr");
  if (!tr) throw new Error("no row for " + name);
  return tr;
};

describe("Clusters page", () => {
  it("shows every cluster with its state, version, nodes and problems", async () => {
    renderApp("/", respond, { home: true });
    expect(await screen.findByRole("heading", { name: "Clusters" })).toBeInTheDocument();
    await screen.findByRole("link", { name: "kind-lab" });
    expect(screen.queryByRole("button", { name: /^Namespace:/ })).not.toBeInTheDocument();

    await waitFor(() => {
      expect(within(row("prod-eu-1")).getByText("Connected")).toBeInTheDocument();
    });
    expect(row("prod-eu-1")).toHaveTextContent("v1.34.2");
    expect(row("prod-eu-1")).toHaveTextContent("2 of 3 ready");
    expect(row("prod-eu-1")).toHaveTextContent("4");
    expect(within(row("prod-eu-1")).getAllByRole("meter")).toHaveLength(3);
    expect(row("kind-lab")).toHaveTextContent("current");
    expect(row("kind-lab")).toHaveTextContent("None");
    expect(row("kind-lab")).toHaveTextContent("3 ready");
    await waitFor(() => {
      expect(within(row("old-staging")).getByText("Unreachable")).toBeInTheDocument();
    });
    expect(within(row("old-staging")).queryAllByRole("meter")).toHaveLength(0);
    await waitFor(() => {
      expect(within(row("expired")).getByText("Credentials refused")).toBeInTheDocument();
    });
    expect(screen.getByRole("link", { name: "kind-lab" })).toHaveAttribute("href", "/c/kind-lab/");
  });

  it("filters by name and opens a cluster from its row", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/", respond, { home: true });
    await screen.findByRole("link", { name: "kind-lab" });

    await user.type(screen.getByRole("searchbox", { name: "Filter clusters" }), "prod");
    expect(screen.queryByRole("link", { name: "kind-lab" })).not.toBeInTheDocument();
    await user.click(within(row("prod-eu-1")).getByText("192.0.2.10:6443"));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/c/prod-eu-1/");
    });
    // Inside a cluster, the rail leads back to all of them.
    expect(await screen.findByRole("link", { name: "All clusters" })).toHaveAttribute("href", "/");
  });

  it("goes straight into the only cluster", async () => {
    const { router } = renderApp("/", undefined, { home: true });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/c/default/");
    });
    // One cluster: no way back to a list of one.
    expect(await screen.findByRole("button", { name: /^Cluster: prod-eu-1/ })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "All clusters" })).not.toBeInTheDocument();
  });
});

describe("links between clusters", () => {
  it("opens old links on the current cluster", async () => {
    const { router } = renderApp("/c/x", respond);
    await router.navigate("/workloads/pods?sort=age");
    await waitFor(() => {
      expect(router.state.location.pathname + router.state.location.search).toBe(
        "/c/kind-lab/workloads/pods?sort=age",
      );
    });
  });

  it("says so for a cluster Glowpod doesn't know", async () => {
    renderApp("/c/nope/", respond);
    expect(
      await screen.findByRole("heading", { name: "Glowpod isn't connected to that cluster" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "See all clusters" })).toHaveAttribute("href", "/");
  });

  it("switches cluster from the picker and stays on the same kind of page", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/c/kind-lab/workloads/pods/shop/web-1", respond);
    await user.click(await screen.findByRole("button", { name: /^Cluster: kind-lab/ }));
    await user.click(screen.getByRole("button", { name: "prod-eu-1" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/c/prod-eu-1/workloads/pods");
    });
  });

  it("keeps the picked namespaces per cluster", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/c/kind-lab/", respond);
    await user.click(await screen.findByRole("button", { name: /^Namespace:/ }));
    await user.click(
      within(screen.getByRole("listbox", { name: "Namespaces" })).getByRole("option", {
        name: "payments",
      }),
    );
    expect(screen.getByRole("button", { name: /^Namespace:/ })).toHaveAccessibleName(
      "Namespace: payments",
    );
    await router.navigate("/c/prod-eu-1/");
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /^Namespace:/ })).toHaveAccessibleName(
        "Namespace: All namespaces",
      );
    });
    expect(localStorage.getItem("glowpod.namespaces")).toBe('{"kind-lab":"payments"}');
  });
});

describe("cluster paths", () => {
  it.each([
    ["kind-lab", "/workloads/pods", "/c/kind-lab/workloads/pods"],
    ["kind-lab", "/", "/c/kind-lab/"],
    ["kind-lab", "/c/prod/", "/c/prod/"],
    ["kind-lab", "/sign-in?return=%2F", "/sign-in?return=%2F"],
    ["kind-lab", "/api/v1/clusters", "/api/v1/clusters"],
    [undefined, "/workloads/pods", "/workloads/pods"],
    ["a b", "/nodes", "/c/a%20b/nodes"],
  ])("on %s, %s is %s", (cluster, path, want) => {
    expect(clusterHref(cluster, path)).toBe(want);
  });

  it.each([
    ["/c/kind-lab/workloads/pods", "/workloads/pods"],
    ["/c/kind-lab", "/"],
    ["/c/kind-lab/", "/"],
    ["/sign-in", "/sign-in"],
  ])("%s is %s within the cluster", (path, want) => {
    expect(pathInCluster(path)).toBe(want);
  });

  it.each([
    ["/workloads/pods/shop/web-1", "/workloads/pods"],
    ["/workloads/deployments", "/workloads/deployments"],
    ["/nodes/worker-1", "/nodes"],
    ["/apps/shop/web", "/apps"],
    ["/", "/"],
  ])("switching from %s opens %s", (path, want) => {
    expect(samePage(path)).toBe(want);
  });
});
