import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ClusterInfo, HubSession, Session } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

function hubSession(hub: HubSession, user = "glowpod:admin"): Session {
  return { ...fx.session, mode: "in-cluster", method: "password", user: { username: user }, hub };
}

interface Sent {
  method: string;
  path: string;
  body: unknown;
}

/** A Hub with the local cluster, a connected one and two whose agents aren't. */
function hub(session: Session, extra: Partial<Record<string, () => Response>> = {}) {
  const clusters: ClusterInfo[] = [
    { id: "local", name: "local", server: "10.0.0.1:6443" },
    {
      id: "prod-eu",
      name: "prod-eu",
      server: "via glowpod-agent",
      state: "connected",
      since: ago(60),
    },
    {
      id: "staging",
      name: "staging",
      server: "via glowpod-agent",
      state: "disconnected",
      since: ago(4),
    },
    {
      id: "new-one",
      name: "new-one",
      server: "via glowpod-agent",
      state: "pending",
      since: ago(1),
    },
  ];
  const sent: Sent[] = [];
  const app = renderApp(
    "/",
    (url, init) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        sent.push({
          method,
          path: url.pathname,
          body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
        });
      }
      const handler = extra[`${method} ${url.pathname}`];
      if (handler) return handler();
      if (url.pathname === "/api/v1/session") return Response.json(session);
      if (url.pathname === "/api/v1/clusters") return Response.json({ clusters });
      const m = /^\/api\/v1\/clusters\/([^/]+)\/summary$/.exec(url.pathname);
      if (m) {
        const o = fx.overview();
        return Response.json({
          cluster: { ...o.cluster, id: m[1], name: m[1], nodesReady: 3 },
          gauges: { cpu: o.gauges.cpu, memory: o.gauges.memory, pods: o.gauges.pods },
          health: o.health,
          problemCount: 0,
          problems: [],
          thresholds: o.thresholds,
          generatedAt: o.generatedAt,
        });
      }
      return undefined;
    },
    { home: true },
  );
  return { ...app, sent, clusters };
}

const row = (name: string) => {
  const tr = screen.getByRole("link", { name }).closest("tr");
  if (!tr) throw new Error("no row " + name);
  return tr;
};

describe("Hub first start", () => {
  it("sends admin to set a password and the server URL first", async () => {
    const user = userEvent.setup();
    let session = hubSession({
      role: "admin",
      setupRequired: true,
      serverURL: "https://hub.example.com",
    });
    const { router, sent } = hub(session, {
      "POST /api/v1/hub/setup": () => {
        session = hubSession({ role: "admin" });
        return new Response(null, { status: 204 });
      },
      "GET /api/v1/session": () => Response.json(session),
    });
    expect(await screen.findByRole("heading", { name: "Set up Glowpod Hub" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/setup");
    expect(screen.getByLabelText("Server URL")).toHaveValue("https://hub.example.com");

    await user.type(screen.getByLabelText("New password"), "a long password");
    await user.type(screen.getByLabelText("The same password again"), "a long passwore");
    await user.click(screen.getByRole("button", { name: "Save and continue" }));
    expect(screen.getByRole("alert")).toHaveTextContent("don't match");
    expect(sent).toHaveLength(0);

    await user.clear(screen.getByLabelText("The same password again"));
    await user.type(screen.getByLabelText("The same password again"), "a long password");
    await user.click(screen.getByRole("button", { name: "Save and continue" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
    expect(sent).toEqual([
      {
        method: "POST",
        path: "/api/v1/hub/setup",
        body: { password: "a long password", serverURL: "https://hub.example.com" },
      },
    ]);
    expect(await screen.findByRole("heading", { name: "Clusters" })).toBeInTheDocument();
  });
});

describe("Hub clusters", () => {
  it("shows each agent's state and asks only connected clusters for numbers", async () => {
    const { fetched } = hub(hubSession({ role: "admin" }));
    await screen.findByRole("link", { name: "prod-eu" });
    await waitFor(() => {
      expect(within(row("prod-eu")).getByText("Connected")).toBeInTheDocument();
    });
    expect(within(row("staging")).getByText("Disconnected · 4 min")).toBeInTheDocument();
    expect(within(row("new-one")).getByText("Waiting for agent")).toBeInTheDocument();
    expect(fetched("/clusters/staging/summary")).toHaveLength(0);
    expect(within(row("staging")).queryAllByRole("meter")).toHaveLength(0);
    expect(fetched("/clusters/new-one/summary")).toHaveLength(0);
    // The Hub's own cluster can't be removed or reconnected.
    expect(within(row("local")).queryByRole("button")).not.toBeInTheDocument();
    expect(within(row("staging")).getByRole("button", { name: "Connect" })).toBeInTheDocument();
    expect(
      within(row("prod-eu")).queryByRole("button", { name: "Connect" }),
    ).not.toBeInTheDocument();
  });

  it("imports a cluster and shows the command", async () => {
    const user = userEvent.setup();
    const { sent } = hub(hubSession({ role: "admin" }), {
      "POST /api/v1/hub/clusters": () =>
        Response.json({
          cluster: { id: "edge", name: "edge", server: "via glowpod-agent", state: "pending" },
          command: "kubectl apply -f https://hub.example.com/v3/import/edge.secret.yaml",
          insecureCommand:
            "curl --insecure -sfL https://hub.example.com/v3/import/edge.secret.yaml | kubectl apply -f -",
          expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        }),
    });
    await user.click(await screen.findByRole("button", { name: "Import" }));
    const dialog = screen.getByRole("dialog", { name: "Import a cluster" });
    expect(within(dialog).getByRole("checkbox", { name: /Make me an owner/ })).toBeChecked();
    await user.type(within(dialog).getByLabelText("Name"), "edge");
    await user.click(within(dialog).getByRole("checkbox", { name: /Make me an owner/ }));
    await user.click(within(dialog).getByRole("button", { name: "Import" }));

    expect(await within(dialog).findByLabelText("Install command")).toHaveTextContent(
      "kubectl apply -f https://hub.example.com/v3/import/edge.secret.yaml",
    );
    expect(within(dialog).getByText("Waiting for agent")).toBeInTheDocument();
    expect(sent).toContainEqual({
      method: "POST",
      path: "/api/v1/hub/clusters",
      body: { name: "edge", owner: false },
    });
  });

  it("removes a cluster after its name is typed, and says how to clean up", async () => {
    const user = userEvent.setup();
    const { sent } = hub(hubSession({ role: "admin" }), {
      "DELETE /api/v1/hub/clusters/staging": () =>
        Response.json({ cleanup: "kubectl delete namespace glowpod-system" }),
    });
    await screen.findByRole("link", { name: "staging" });
    await user.click(within(row("staging")).getByRole("button", { name: "Remove" }));
    const dialog = screen.getByRole("dialog", { name: "Remove staging from the Hub?" });
    const remove = within(dialog).getByRole("button", { name: "Remove" });
    expect(remove).toBeDisabled();
    await user.type(within(dialog).getByLabelText(/to confirm/), "staging");
    await user.click(remove);
    expect(await within(dialog).findByLabelText("Clean-up command")).toHaveTextContent(
      "kubectl delete namespace glowpod-system",
    );
    expect(sent).toContainEqual({
      method: "DELETE",
      path: "/api/v1/hub/clusters/staging",
      body: undefined,
    });
  });

  it("gives people who aren't administrators no Import and no actions", async () => {
    hub(hubSession({ role: "user" }, "oidc:ann@example.com"));
    await screen.findByRole("link", { name: "prod-eu" });
    expect(screen.queryByRole("button", { name: "Import" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Actions" })).not.toBeInTheDocument();
    expect(screen.getByText("The clusters where you have access.")).toBeInTheDocument();
  });
});

describe("Hub sign-in", () => {
  it("signs in with a Glowpod account", async () => {
    const user = userEvent.setup();
    let signedIn = false;
    const posted: unknown[] = [];
    renderApp("/sign-in", (url, init) => {
      if (url.pathname === "/api/v1/auth/methods") {
        return Response.json({
          methods: [
            { type: "oidc", name: "Okta" },
            { type: "password", name: "Glowpod" },
          ],
        });
      }
      if (url.pathname === "/api/v1/auth/password") {
        posted.push(JSON.parse(init?.body as string));
        signedIn = true;
        return Response.json({});
      }
      if (url.pathname === "/api/v1/session") {
        return signedIn
          ? Response.json(hubSession({ role: "admin" }))
          : Response.json(
              { error: { code: "unauthenticated", message: "Sign in" } },
              { status: 401 },
            );
      }
      if (url.pathname === "/api/v1/clusters") return Response.json({ clusters: fx.clusters });
      return undefined;
    });
    expect(await screen.findByRole("link", { name: "Sign in with Okta" })).toBeInTheDocument();
    // The only username method: its form shows at once, titled.
    const form = screen.getByRole("form", { name: "Sign in with a Glowpod account" });
    expect(within(form).getByText("Glowpod account")).toBeInTheDocument();
    expect(screen.getByText(/bootstrap password/)).toBeInTheDocument();
    await user.type(within(form).getByLabelText("Username"), "admin");
    await user.type(within(form).getByLabelText("Password"), "the bootstrap one");
    await user.click(within(form).getByRole("button", { name: "Sign in" }));
    await waitFor(() => {
      expect(posted).toEqual([{ username: "admin", password: "the bootstrap one" }]);
    });
  });
});

describe("kubeconfig from the Hub", () => {
  it("downloads one, lists it and revokes it", async () => {
    const user = userEvent.setup();
    let tokens = [{ id: "a1b2c3", user: "glowpod:admin", created: ago(90), expires: ago(-390) }];
    const created: string[] = [];
    const blobs = vi.spyOn(URL, "createObjectURL").mockImplementation((b: Blob | MediaSource) => {
      created.push(b instanceof Blob ? b.type : "");
      return "blob:kubeconfig";
    });
    const clicks = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      created.push(this.download);
    });
    try {
      const { sent } = hub(hubSession({ role: "admin" }), {
        "POST /api/v1/hub/kubeconfig": () =>
          new Response("apiVersion: v1\nkind: Config\n", {
            headers: { "Content-Type": "application/yaml", "X-Glowpod-Token-Expires": ago(-480) },
          }),
        "GET /api/v1/hub/kubeconfig/tokens": () => Response.json({ tokens, ttlSeconds: 28800 }),
        "DELETE /api/v1/hub/kubeconfig/tokens/a1b2c3": () => {
          tokens = [];
          return new Response(null, { status: 204 });
        },
      });
      await user.click(await screen.findByRole("button", { name: /signed in as glowpod:admin/i }));
      await user.click(screen.getByRole("button", { name: "Kubeconfig" }));
      const dialog = await screen.findByRole("dialog", { name: "Use kubectl" });
      expect(within(dialog).getByText(/for 8 hours/)).toBeInTheDocument();
      expect(
        await within(dialog).findByText(/Made 1 h ago · works 6 h more · not used yet/),
      ).toBeInTheDocument();

      await user.click(within(dialog).getByRole("button", { name: "Download kubeconfig" }));
      expect(await within(dialog).findByText(/Saved glowpod-kubeconfig.yaml/)).toBeInTheDocument();
      expect(created).toEqual(["application/yaml", "glowpod-kubeconfig.yaml"]);
      expect(sent).toContainEqual({ method: "POST", path: "/api/v1/hub/kubeconfig", body: {} });

      await user.click(
        within(dialog).getByRole("button", { name: /Revoke the kubeconfig made 1 h ago/ }),
      );
      expect(
        await within(dialog).findByText("You have no kubeconfigs that still work."),
      ).toBeInTheDocument();
      expect(sent.some((s) => s.method === "DELETE")).toBe(true);
    } finally {
      blobs.mockRestore();
      clicks.mockRestore();
    }
  });

  it("isn't offered outside a Hub", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.click(await screen.findByRole("button", { name: /signed in as/i }));
    expect(screen.queryByRole("button", { name: "Kubeconfig" })).not.toBeInTheDocument();
  });
});
