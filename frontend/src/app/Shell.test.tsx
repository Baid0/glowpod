import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as fx from "../test/fixtures";
import { FakeWebSocket, renderApp } from "../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const rail = () => screen.getByRole("navigation", { name: "Sections" });
const railLabels = () =>
  within(rail())
    .getAllByRole("link")
    .map((l) => l.getAttribute("aria-label"));

describe("Shell (Deep mode)", () => {
  it("shows the rail, panel counts, health card and top bar", async () => {
    renderApp("/");

    expect(railLabels()).toEqual([
      "Glowpod overview",
      "Cluster",
      "Workloads",
      "Network",
      "Storage",
      "Config",
      "Extensions",
      "Settings",
    ]);
    expect(within(rail()).getByRole("link", { name: "Cluster" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();

    const panel = screen.getByRole("complementary", { name: "Cluster panel" });
    expect(await within(panel).findByLabelText("1 of 3 nodes need attention")).toHaveTextContent(
      "1 hot · 3",
    );
    expect(within(panel).getByLabelText("5 namespaces")).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: /Overview/ })).toHaveAttribute(
      "aria-current",
      "page",
    );

    const health = within(panel).getByRole("region", { name: "Cluster health" });
    expect(health).toHaveTextContent("290 running");
    expect(health).toHaveTextContent("4 pending");
    expect(health).toHaveTextContent("3 failing");
    expect(within(health).getByRole("meter", { name: "CPU" })).toHaveAttribute(
      "aria-valuetext",
      "CPU 62 percent, OK",
    );
    expect(within(health).getByRole("meter", { name: "Memory" })).toHaveAttribute(
      "aria-valuetext",
      "Memory 78 percent, High",
    );

    expect(
      await screen.findByRole("button", { name: "Signed in as kubernetes-admin" }),
    ).toHaveTextContent("KA");
    expect(await screen.findByText(/updated \d+ s ago/)).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /Cluster: prod-eu-1/ })).toBeInTheDocument();
  });

  it("collapses the panel into the rail and remembers it", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.click(screen.getByRole("button", { name: "Collapse panel" }));
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    expect(window.localStorage.getItem("glowpod.panelCollapsed")).toBe("true");

    await user.click(within(rail()).getByRole("button", { name: "Expand panel" }));
    expect(screen.getByRole("complementary", { name: "Cluster panel" })).toBeInTheDocument();
  });

  it("follows the section of the current page", () => {
    renderApp("/workloads/deployments");
    expect(within(rail()).getByRole("link", { name: "Workloads" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("complementary", { name: "Workloads panel" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Deployments" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "deployments" })).toBeInTheDocument();
  });
});

describe("Glance / Deep toggle", () => {
  it("switches the rail and panel, keeps the gauges, and remembers the mode", async () => {
    const user = userEvent.setup();
    renderApp("/");
    const modes = screen.getByRole("group", { name: "View mode" });
    expect(within(modes).getByRole("button", { name: "Deep" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    await user.click(within(modes).getByRole("button", { name: "Glance" }));

    expect(within(modes).getByRole("button", { name: "Glance" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(railLabels()).toEqual([
      "Glowpod overview",
      "Overview",
      "Apps",
      "Logs",
      "Events",
      "Settings",
    ]);
    const panel = screen.getByRole("complementary", { name: "Overview panel" });
    const apps = await within(panel).findByRole("region", { name: "Your apps" });
    expect(within(apps).getByRole("link", { name: "payments-api, failing" })).toBeInTheDocument();
    expect(within(apps).getByRole("link", { name: "checkout-web, healthy" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Cluster health" })).toBeInTheDocument();
    expect(window.localStorage.getItem("glowpod.mode")).toBe("glance");
  });

  it("keeps Deep-only pages reachable in Glance mode", () => {
    window.localStorage.setItem("glowpod.mode", "glance");
    renderApp("/nodes");
    expect(screen.getByRole("heading", { level: 1, name: "Nodes" })).toBeInTheDocument();
    expect(within(rail()).queryByRole("link", { current: "page" })).not.toBeInTheDocument();
  });
});

describe("Namespace picker", () => {
  it("reloads the Overview for the chosen namespace and remembers it", async () => {
    const user = userEvent.setup();
    const { fetched } = renderApp("/");
    const picker = screen.getByRole("combobox", { name: "Namespace" });
    await screen.findByRole("option", { name: "payments" });

    await user.selectOptions(picker, "payments");

    await waitFor(() => {
      expect(fetched("/overview?namespace=payments")).toHaveLength(1);
    });
    expect(window.localStorage.getItem("glowpod.namespace")).toBe("payments");
    await waitFor(() => {
      expect(FakeWebSocket.latest().url).toContain("/stream?namespace=payments");
    });
  });
});

describe("Command palette", () => {
  it("opens with ⌘K and goes to a page", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("{Meta>}k{/Meta}");

    const input = await screen.findByRole("combobox", { name: "Search pages and commands" });
    await user.type(input, "nodes");
    expect(screen.getByRole("option", { name: /Nodes/ })).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{Enter}");

    expect(await screen.findByRole("heading", { level: 1, name: "Nodes" })).toBeInTheDocument();
    expect(
      screen.queryByRole("combobox", { name: "Search pages and commands" }),
    ).not.toBeInTheDocument();
  });

  it("switches mode and namespace from the keyboard", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await screen.findByRole("option", { name: "search" }); // namespaces loaded

    await user.click(screen.getByRole("button", { name: "Search and commands" }));
    await user.type(screen.getByRole("combobox", { name: "Search pages and commands" }), "glance");
    await user.keyboard("{Enter}");
    expect(screen.getByRole("button", { name: "Glance" })).toHaveAttribute("aria-pressed", "true");

    await user.keyboard("{Control>}k{/Control}");
    await user.type(
      screen.getByRole("combobox", { name: "Search pages and commands" }),
      "namespace sea",
    );
    await user.keyboard("{ArrowDown}{ArrowUp}{Enter}");
    expect(screen.getByRole("combobox", { name: "Namespace" })).toHaveValue("search");
  });

  it("says when nothing matches", async () => {
    const user = userEvent.setup();
    renderApp("/");
    await user.keyboard("{Meta>}k{/Meta}");
    await user.type(screen.getByRole("combobox", { name: "Search pages and commands" }), "zzzz");
    expect(screen.getByText("Nothing matches “zzzz”.")).toBeInTheDocument();
  });
});

describe("Live stream", () => {
  it("goes live, shows new warnings and reconnects after a drop", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      renderApp("/");
      const panel = screen.getByRole("complementary", { name: "Cluster panel" });
      await screen.findByRole("button", { name: /Cluster: prod-eu-1$/ });

      act(() => {
        FakeWebSocket.latest().send({ type: "ready" });
      });
      expect(
        await screen.findByRole("button", { name: "Cluster: prod-eu-1, live" }),
      ).toBeInTheDocument();

      // The fixture already has one warning; a live one makes two.
      expect(await within(panel).findByLabelText("1 warning in the last hour")).toBeInTheDocument();
      act(() => {
        FakeWebSocket.latest().send({
          type: "event",
          op: "added",
          event: fx.event("w1", "Warning", 0),
        });
      });
      expect(
        await within(panel).findByLabelText("2 warnings in the last hour"),
      ).toBeInTheDocument();

      const first = FakeWebSocket.latest();
      act(() => {
        first.drop();
      });
      expect(
        await within(screen.getByRole("banner")).findByText(/Reconnecting/),
      ).toBeInTheDocument();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1_100);
      });
      expect(FakeWebSocket.latest()).not.toBe(first);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("Sign-in and missing pages", () => {
  it("asks to sign in when the session is rejected", async () => {
    renderApp("/", (url) =>
      url.pathname === "/api/v1/session"
        ? Response.json(
            {
              error: {
                code: "unauthenticated",
                message: "Open Glowpod from the link in your terminal to sign in.",
              },
            },
            { status: 401 },
          )
        : undefined,
    );
    expect(await screen.findByRole("heading", { name: "Sign in to Glowpod" })).toBeInTheDocument();
    // Local mode has no sign-in methods: the launch link is the way in.
    expect(await screen.findByText(/link printed where you ran/)).toBeInTheDocument();
  });

  it("asks to sign in again when the server forgets the session", async () => {
    let sessionCalls = 0;
    const { router } = renderApp("/workloads/pods", (url) =>
      url.pathname === "/api/v1/session" && sessionCalls++ > 0
        ? Response.json(
            { error: { code: "unauthenticated", message: "Sign in to continue." } },
            { status: 401 },
          )
        : url.pathname.endsWith("/overview")
          ? Response.json(
              {
                error: {
                  code: "unauthenticated",
                  message: "Open Glowpod from the link in your terminal to sign in.",
                },
              },
              { status: 401 },
            )
          : undefined,
    );
    expect(await screen.findByRole("heading", { name: "Sign in to Glowpod" })).toBeInTheDocument();
    expect(router.state.location.pathname + router.state.location.search).toBe(
      "/sign-in?return=%2Fworkloads%2Fpods",
    );
  });

  it("shows a plain 404 inside the shell", () => {
    renderApp("/no/such/page");
    expect(screen.getByText("Nothing at this address")).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Sections" })).toBeInTheDocument();
  });
});
