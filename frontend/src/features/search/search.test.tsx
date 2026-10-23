import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { ClusterInfo } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";
import { hitPath } from "./search";

const clusters: ClusterInfo[] = [
  { id: "default", name: "prod-eu-1", server: "192.0.2.10:6443", current: true },
  { id: "lab", name: "lab", server: "127.0.0.1:6443" },
  { id: "down", name: "old-staging", server: "203.0.113.5:6443" },
  { id: "gone", name: "gone", server: "", state: "disconnected" },
  { id: "team", name: "team-cluster", server: "198.51.100.7:6443" },
];

/** Three clusters answer search; one doesn't; one's agent is away. */
function respond(url: URL): Response | undefined {
  if (url.pathname === "/api/v1/clusters") return Response.json({ clusters });
  const m = /^\/api\/v1\/clusters\/([^/]+)\/search$/.exec(url.pathname);
  if (!m) return undefined;
  if (m[1] === "down") {
    return Response.json(
      { error: { code: "cluster_unreachable", message: "Can't reach the cluster." } },
      { status: 503 },
    );
  }
  if (m[1] === "team" && !url.searchParams.get("namespace")) {
    return Response.json({ hits: [], denied: true });
  }
  const res = fx.search(url.searchParams.get("q") ?? "");
  return Response.json(m[1] === "lab" ? { ...res, more: true } : res);
}

const combobox = () => screen.getByRole("combobox", { name: "Search pages, commands and objects" });

describe("search across clusters", () => {
  it("finds objects in every cluster, the one on screen first, and opens one", async () => {
    const user = userEvent.setup();
    const { router, fetched } = renderApp("/", respond);
    await user.keyboard("{Meta>}k{/Meta}");
    await user.type(combobox(), "payments-api");

    const list = screen.getByRole("listbox", { name: "Results" });
    await waitFor(() => {
      expect(within(list).getAllByRole("option")).toHaveLength(6);
    });
    expect(
      within(list)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual([
      "payments-api-7d9f6b-x2kq9 payments · prod-eu-1Pod",
      "payments-api payments · prod-eu-1Deployment",
      "payments-api payments · prod-eu-1Service",
      "payments-api-7d9f6b-x2kq9 payments · labPod",
      "payments-api payments · labDeployment",
      "payments-api payments · labService",
    ]);
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Couldn't search old-staging. To search team-cluster, open it and pick your namespace first. More matches in lab: type more of the name.",
    );
    // A cluster whose agent is away isn't asked.
    expect(fetched("/clusters/gone/search")).toHaveLength(0);
    // Typing is debounced: one request per cluster, for the whole word.
    expect(fetched("/clusters/lab/search").map((u) => new URL(u, "http://x").search)).toEqual([
      "?q=payments-api",
    ]);

    await user.click(
      within(list).getByRole("option", { name: "payments-api, payments · lab, Deployment" }),
    );
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        "/c/lab/workloads/deployments/payments/payments-api",
      );
    });
  });

  it("looks in the namespaces picked for a cluster", async () => {
    const user = userEvent.setup();
    window.localStorage.setItem("glowpod.namespaces", JSON.stringify({ team: "payments" }));
    const { fetched } = renderApp("/", respond);
    await user.keyboard("{Meta>}k{/Meta}");
    await user.type(combobox(), "payments-db");
    expect(
      await screen.findByRole("option", { name: "payments-db, payments · team-cluster, Secret" }),
    ).toBeInTheDocument();
    expect(fetched("/clusters/team/search")).toEqual([
      "/api/v1/clusters/team/search?q=payments-db&namespace=payments",
    ]);
  });

  it("waits for two letters and says when nothing matches", async () => {
    const user = userEvent.setup();
    const { fetched } = renderApp("/", respond);
    await user.keyboard("{Meta>}k{/Meta}");
    await user.type(combobox(), "z");
    await new Promise((r) => setTimeout(r, 300));
    expect(fetched("/search")).toHaveLength(0);

    await user.type(combobox(), "zz");
    expect(await screen.findByText("Nothing matches “zzz”.")).toBeInTheDocument();
  });

  it("works on the Clusters page, with clusters and objects", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/", respond, { home: true });
    await screen.findByRole("link", { name: "lab" });
    await user.click(screen.getByRole("button", { name: "Search and commands" }));
    expect(combobox()).toHaveAttribute("placeholder", "Find a pod, service, secret… or a cluster");

    await user.type(combobox(), "lab");
    const list = screen.getByRole("listbox", { name: "Results" });
    // Pages need a cluster, so the Clusters page offers none.
    expect(within(list).queryByRole("option", { name: /Nodes/ })).not.toBeInTheDocument();
    await user.click(within(list).getByRole("option", { name: "lab, Cluster" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/c/lab/");
    });
  });
});

describe("hitPath", () => {
  it.each([
    [
      { group: "", version: "v1", resource: "pods", kind: "Pod", namespace: "a", name: "web-1" },
      "/c/lab/workloads/pods/a/web-1",
    ],
    [
      {
        group: "batch",
        version: "v1",
        resource: "cronjobs",
        kind: "CronJob",
        namespace: "a",
        name: "nightly",
      },
      "/c/lab/workloads/cronjobs/a/nightly",
    ],
    [{ group: "", version: "v1", resource: "nodes", kind: "Node", name: "w1" }, "/c/lab/nodes/w1"],
    [
      { group: "", version: "v1", resource: "secrets", kind: "Secret", namespace: "a", name: "db" },
      "/c/lab/resources/core/v1/secrets/a/db",
    ],
    [
      {
        group: "networking.k8s.io",
        version: "v1",
        resource: "ingresses",
        kind: "Ingress",
        namespace: "a",
        name: "web",
      },
      "/c/lab/resources/networking.k8s.io/v1/ingresses/a/web",
    ],
    [
      { group: "", version: "v1", resource: "namespaces", kind: "Namespace", name: "a" },
      "/c/lab/resources/core/v1/namespaces/_/a",
    ],
  ])("%o → %s", (hit, want) => {
    expect(hitPath("lab", hit)).toBe(want);
  });
});
