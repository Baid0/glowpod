import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Members, Projects } from "../../lib/types";
import { renderApp } from "../../test/renderApp";

const BASE = "/api/v1/clusters/default";

interface Sent {
  method: string;
  path: string;
  body: unknown;
}

function setup(path: string, allowed: boolean, data: { members?: Members; projects?: Projects }) {
  const sent: Sent[] = [];
  renderApp(path, (url, init) => {
    const method = init?.method ?? "GET";
    if (method !== "GET" && url.pathname !== `${BASE}/access`) {
      sent.push({
        method,
        path: url.pathname + url.search,
        body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
      });
      return new Response(null, { status: 204 });
    }
    switch (url.pathname) {
      case `${BASE}/access`: {
        const body = typeof init?.body === "string" ? init.body : "{}";
        const checks = JSON.parse(body) as { checks: unknown[] };
        return Response.json({ allowed: checks.checks.map(() => allowed) });
      }
      case `${BASE}/members`:
        return Response.json(data.members);
      case `${BASE}/projects`:
        return Response.json(data.projects);
      default:
        return undefined;
    }
  });
  return sent;
}

const members: Members = {
  members: [
    {
      kind: "User",
      name: "oidc:ann",
      role: "read-only",
      clusterRole: "view",
      binding: "glowpod-member-1",
    },
  ],
  others: [
    { kind: "Group", name: "ldap:ops", clusterRole: "cluster-admin", binding: "ops-admins" },
  ],
};

describe("cluster members", () => {
  it("lists members and other access, and adds a member", async () => {
    const user = userEvent.setup();
    const sent = setup("/members", true, { members });
    const table = await screen.findByRole("region", { name: "Cluster members" });
    expect(await within(table).findByText("oidc:ann")).toBeInTheDocument();
    expect(within(table).getByText("Read-only")).toBeInTheDocument();
    const others = screen.getByRole("region", { name: "Other access" });
    expect(within(others).getByText("ops-admins")).toBeInTheDocument();

    await user.click(within(table).getByRole("button", { name: "Add member" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a cluster member" });
    await user.click(within(dialog).getByRole("radio", { name: "Group" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Group" }), "oidc:payments");
    await user.click(within(dialog).getByRole("radio", { name: /Custom/ }));
    await user.type(within(dialog).getByRole("textbox", { name: "ClusterRole" }), "edit");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(sent).toContainEqual({
        method: "PUT",
        path: `${BASE}/members`,
        body: { kind: "Group", name: "oidc:payments", role: "custom", clusterRole: "edit" },
      });
    });
  });

  it("offers no changes to someone the cluster wouldn't let make them", async () => {
    setup("/members", false, { members });
    const table = await screen.findByRole("region", { name: "Cluster members" });
    await within(table).findByText("oidc:ann");
    expect(within(table).queryByRole("button", { name: "Add member" })).not.toBeInTheDocument();
    expect(within(table).queryByRole("button", { name: /Remove/ })).not.toBeInTheDocument();
  });
});

describe("projects", () => {
  const projects: Projects = {
    projects: [
      {
        name: "payments",
        namespaces: ["payments-api", "payments-db"],
        members: [
          { kind: "Group", name: "oidc:payments", role: "member", missing: ["payments-db"] },
        ],
      },
    ],
    unassigned: ["shop"],
  };

  it("shows a project, a missing binding, and applies it", async () => {
    const user = userEvent.setup();
    const sent = setup("/projects", true, { projects });
    const card = await screen.findByRole("region", { name: "payments" });
    expect(within(card).getByRole("list", { name: "Namespaces in payments" })).toHaveTextContent(
      "payments-api",
    );
    expect(within(card).getByText(/Not in payments-db/)).toBeInTheDocument();
    await user.click(within(card).getByRole("button", { name: "Apply there too" }));
    await waitFor(() => {
      expect(sent).toContainEqual({
        method: "PUT",
        path: `${BASE}/projects/payments/members`,
        body: { kind: "Group", name: "oidc:payments", role: "member" },
      });
    });
    expect(screen.getByRole("list", { name: "Namespaces not in a project" })).toHaveTextContent(
      "shop",
    );
  });

  it("makes a new project from namespaces not in one", async () => {
    const user = userEvent.setup();
    const sent = setup("/projects", true, { projects });
    await user.click(await screen.findByRole("button", { name: "New project" }));
    const dialog = await screen.findByRole("dialog", { name: "New project" });
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "shop");
    await user.click(within(dialog).getByRole("checkbox", { name: "shop" }));
    await user.click(within(dialog).getByRole("button", { name: "Make project" }));
    await waitFor(() => {
      expect(sent).toContainEqual({
        method: "POST",
        path: `${BASE}/projects`,
        body: { name: "shop", namespaces: ["shop"] },
      });
    });
  });
});
