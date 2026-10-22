import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as fx from "../../test/fixtures";
import { FakeWebSocket, renderApp } from "../../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const POD = "/api/v1/clusters/default/pods/payments/payments-api-7d9f6b-x2kq9";
const PATH = "/workloads/pods/payments/payments-api-7d9f6b-x2kq9";
const withPod = (url: URL) => (url.pathname === POD ? Response.json(fx.podDetail()) : undefined);

function logSockets() {
  return FakeWebSocket.instances.filter((w) => w.url.includes("/logs/payments/"));
}

async function openLogs(query = "?tab=logs") {
  renderApp(PATH + query, withPod);
  const viewer = await screen.findByRole("region", { name: "Logs" });
  await waitFor(() => {
    expect(logSockets().length).toBeGreaterThan(0);
  });
  const ws = logSockets().at(-1);
  if (!ws) throw new Error("no log socket");
  return { viewer, ws };
}

function send(ws: FakeWebSocket, ...msgs: unknown[]) {
  act(() => {
    for (const m of msgs) ws.send(m);
  });
}

describe("Log viewer", () => {
  it("tails the struggling container and shows lines as they arrive", async () => {
    const { viewer, ws } = await openLogs();
    expect(ws.url).toContain(
      "/logs/payments/payments-api-7d9f6b-x2kq9?container=app&tailLines=1000",
    );

    send(
      ws,
      { type: "ready" },
      {
        type: "lines",
        lines: [
          { t: "2026-10-22T03:00:00Z", x: "starting payments-api v3" },
          { t: "2026-10-22T03:00:01Z", x: "\u001b[31mERROR\u001b[0m missing env DB_URL" },
        ],
      },
    );
    expect(await within(viewer).findByText("starting payments-api v3")).toBeInTheDocument();
    const err = within(viewer).getByText("ERROR missing env DB_URL");
    expect(err.closest("[data-level]")).toHaveAttribute("data-level", "error");
    expect(within(viewer).getByRole("status")).toHaveTextContent("Streaming");
  });

  it("searches, highlighting matches", async () => {
    const user = userEvent.setup();
    const { viewer, ws } = await openLogs();
    send(
      ws,
      { type: "ready" },
      { type: "lines", lines: [{ x: "db connect" }, { x: "http ok" }, { x: "DB timeout" }] },
    );
    await within(viewer).findByText("http ok");

    await user.type(within(viewer).getByRole("searchbox", { name: "Search the log" }), "db");
    expect(within(viewer).queryByText("http ok")).not.toBeInTheDocument();
    expect(within(viewer).getAllByText(/^db$/i, { selector: "mark" })).toHaveLength(2);
    expect(within(viewer).getByRole("status")).toHaveTextContent("2 of 3 lines");
  });

  it("stops at the end of the log without reconnecting", async () => {
    const { viewer, ws } = await openLogs();
    send(
      ws,
      { type: "ready" },
      { type: "end", message: "The container stopped, so the log ended." },
    );
    expect(
      await within(viewer).findByText("The container stopped, so the log ended."),
    ).toBeInTheDocument();
    expect(within(viewer).getByRole("status")).toHaveTextContent("Ended");
    expect(ws.closed).toBe(true);
  });

  it("shows why logs aren't available", async () => {
    const { viewer, ws } = await openLogs();
    send(ws, {
      type: "error",
      code: "not_started",
      message: "This container hasn't started yet, so there are no logs.",
    });
    expect(
      await within(viewer).findByText("This container hasn't started yet, so there are no logs."),
    ).toBeInTheDocument();
    expect(within(viewer).getByRole("status")).toHaveTextContent("Stopped");
  });

  it("says when it's reconnecting", async () => {
    const { viewer, ws } = await openLogs();
    send(ws, { type: "ready" });
    act(() => {
      ws.drop();
    });
    expect(await within(viewer).findByText(/Reconnecting/)).toBeInTheDocument();
  });

  it("Deep: previous run, container picker and download", async () => {
    const user = userEvent.setup();
    const { viewer } = await openLogs();
    expect(within(viewer).getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      "/api/v1/clusters/default/logs/payments/payments-api-7d9f6b-x2kq9/download?container=app",
    );

    await user.click(within(viewer).getByRole("checkbox", { name: "Previous run" }));
    await waitFor(() => {
      expect(logSockets().at(-1)?.url).toContain("container=app&previous=true");
    });
    expect(within(viewer).getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      expect.stringContaining("previous=true"),
    );

    await user.selectOptions(within(viewer).getByRole("combobox", { name: "Container" }), "proxy");
    await waitFor(() => {
      expect(logSockets().at(-1)?.url).toContain("container=proxy&tailLines");
    });
    // proxy never restarted: no previous run to show.
    expect(within(viewer).getByRole("checkbox", { name: "Previous run" })).toBeDisabled();
  });

  it("Glance: offers the previous run of a restarting container", async () => {
    const user = userEvent.setup();
    window.localStorage.setItem("glowpod.mode", "glance");
    const { viewer } = await openLogs();
    expect(within(viewer).queryByRole("link", { name: "Download" })).not.toBeInTheDocument();
    expect(viewer).toHaveTextContent("app has restarted 14 times");
    await user.click(within(viewer).getByRole("button", { name: "Show the previous run" }));
    await waitFor(() => {
      expect(logSockets().at(-1)?.url).toContain("previous=true");
    });
  });

  it("the suggested fix links straight to the last crash's log", async () => {
    const user = userEvent.setup();
    renderApp(PATH, withPod);
    await user.click(await screen.findByRole("link", { name: "Open the last crash's log" }));
    expect(screen.getByRole("tab", { name: "Logs" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => {
      expect(logSockets().at(-1)?.url).toContain("container=app&previous=true");
    });
  });
});
