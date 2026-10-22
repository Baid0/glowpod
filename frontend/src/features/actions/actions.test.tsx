import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderApp } from "../../test/renderApp";
import { deleteConsequence } from "./consequence";

const BASE = "/api/v1/clusters/default";
const deployType = {
  group: "apps",
  version: "v1",
  resource: "deployments",
  kind: "Deployment",
  namespaced: true,
  verbs: ["get", "list", "update", "patch", "delete"],
  subresources: ["scale", "status"],
};
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

interface Sent {
  method: string;
  path: string;
  body: unknown;
  csrf: string | null;
}

function setup(opts: { allowed?: boolean[]; scaleError?: boolean } = {}) {
  const sent: Sent[] = [];
  const app = renderApp("/workloads/deployments/shop/web", (url, init) => {
    const method = init?.method ?? "GET";
    if (method !== "GET") {
      sent.push({
        method,
        path: url.pathname + url.search,
        body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
        csrf: new Headers(init?.headers).get("X-CSRF-Token"),
      });
    }
    switch (url.pathname) {
      case `${BASE}/resources`:
        return Response.json({ types: [deployType] });
      case `${BASE}/resources/apps/v1/deployments/web`:
        return method === "DELETE" ? new Response(null, { status: 204 }) : Response.json(web);
      case `${BASE}/access`:
        return Response.json({ allowed: opts.allowed ?? [true, true, true, true, false] });
      case `${BASE}/resources/apps/v1/deployments/web/scale`:
        return opts.scaleError
          ? Response.json(
              {
                error: {
                  code: "forbidden",
                  message: "You don't have access to this. Ask an admin for access.",
                  detail: 'admission webhook "quota" denied the request: too many replicas',
                },
              },
              { status: 403 },
            )
          : new Response(null, { status: 204 });
      case `${BASE}/resources/apps/v1/deployments/web/restart`:
        return new Response(null, { status: 204 });
      default:
        return undefined;
    }
  });
  return { ...app, sent };
}

describe("resource actions", () => {
  it("shows only what the user may do", async () => {
    setup({ allowed: [false, false, true, false, false] });
    const bar = await screen.findByRole("group", { name: "Actions" });
    expect(within(bar).getByRole("button", { name: "Restart" })).toBeInTheDocument();
    expect(within(bar).queryByRole("button", { name: "Scale" })).not.toBeInTheDocument();
    expect(within(bar).queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("asks the cluster about this exact object", async () => {
    const { sent } = setup();
    await screen.findByRole("group", { name: "Actions" });
    const access = sent.find((s) => s.path.endsWith("/access"));
    expect(access?.body).toEqual({
      checks: expect.arrayContaining([
        { verb: "delete", group: "apps", resource: "deployments", namespace: "shop", name: "web" },
        {
          verb: "patch",
          group: "apps",
          resource: "deployments",
          subresource: "scale",
          namespace: "shop",
          name: "web",
        },
      ]) as unknown,
    });
    expect(access?.csrf).toBe("test-csrf");
  });

  it("scales with the CSRF token and says so", async () => {
    const { sent } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Scale" }));
    const dialog = screen.getByRole("dialog", { name: "Scale web" });
    expect(within(dialog).getByText("It runs 2 replicas now.")).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "One more" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Scale to 3" }));

    expect(await screen.findByText("Scaled web to 3 replicas.")).toBeInTheDocument();
    const scale = sent.find((s) => s.path.includes("/scale"));
    expect(scale).toMatchObject({ method: "PUT", body: { replicas: 3 }, csrf: "test-csrf" });
    expect(scale?.path).toContain("namespace=shop");
  });

  it("explains scaling to zero and refuses nonsense", async () => {
    setup();
    await userEvent.click(await screen.findByRole("button", { name: "Scale" }));
    const input = screen.getByLabelText("Replicas");
    await userEvent.clear(input);
    await userEvent.type(input, "0");
    expect(screen.getByText(/Scaling to 0 stops every pod/)).toBeInTheDocument();
    await userEvent.clear(input);
    await userEvent.type(input, "two");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(
      within(screen.getByRole("dialog")).getByRole("button", { name: "Scale" }),
    ).toBeDisabled();
  });

  it("shows the cluster's reason inside the dialog when it refuses", async () => {
    setup({ scaleError: true });
    await userEvent.click(await screen.findByRole("button", { name: "Scale" }));
    await userEvent.click(screen.getByRole("button", { name: "One more" }));
    await userEvent.click(screen.getByRole("button", { name: "Scale to 3" }));
    const alert = await within(screen.getByRole("dialog")).findByRole("alert");
    expect(alert).toHaveTextContent("too many replicas");
  });

  it("restarts after a confirmation", async () => {
    const { sent } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Restart" }));
    await userEvent.click(
      within(screen.getByRole("dialog", { name: "Restart web?" })).getByRole("button", {
        name: "Restart",
      }),
    );
    expect(await screen.findByText(/Restarting web/)).toBeInTheDocument();
    expect(sent.some((s) => s.method === "POST" && s.path.includes("/restart"))).toBe(true);
  });

  it("deletes only after the name is typed, then goes to the list", async () => {
    const { sent, router } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    const dialog = screen.getByRole("dialog", { name: "Delete deployment web?" });
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type web to confirm/), "we");
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type web to confirm/), "b");
    await userEvent.click(confirm);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/c/default/workloads/deployments");
    });
    expect(sent.some((s) => s.method === "DELETE")).toBe(true);
    expect(await screen.findByText("Deleted deployment web.")).toBeInTheDocument();
  });
});

describe("deleteConsequence", () => {
  it.each([
    ["pods", "ReplicaSet", /ReplicaSet starts a replacement/],
    ["pods", undefined, /Nothing will replace it/],
    ["statefulsets", undefined, /volumes .* stay/],
    ["namespaces", undefined, /Everything inside it/],
    ["configmaps", undefined, /can't be undone/],
  ])("%s owned by %s", (resource, owner, want) => {
    expect(deleteConsequence(resource, owner)).toMatch(want);
  });
});
