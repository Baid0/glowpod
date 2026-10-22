import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { FakeWebSocket, renderApp } from "../../test/renderApp";
import { eventObjectPath } from "./tone";

const rowsOf = () =>
  within(screen.getByRole("list", { name: "Events, newest first" })).getAllByRole("listitem");

describe("events page", () => {
  it("lists events newest first with pills and links", async () => {
    const { fetched } = renderApp("/events");
    const list = await screen.findByRole("list", { name: "Events, newest first" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    const [, second] = rows;
    if (!second) throw new Error("missing row");
    expect(rows[0]).toHaveTextContent("Problem");
    expect(rows[0]).toHaveTextContent("Back-off restarting failed container app");
    expect(within(second).getByRole("link", { name: "deploy/checkout-web" })).toHaveAttribute(
      "href",
      "/c/default/workloads/deployments/checkout/checkout-web",
    );
    expect(fetched("/events").some((u) => u.includes("limit=200"))).toBe(true);
  });

  it("filters by kind and searches", async () => {
    renderApp("/events");
    await screen.findByRole("list", { name: "Events, newest first" });
    await userEvent.click(screen.getByRole("radio", { name: /Problems/ }));
    expect(rowsOf()).toHaveLength(1);

    await userEvent.click(screen.getByRole("radio", { name: /All/ }));
    await userEvent.type(screen.getByRole("searchbox", { name: "Search events" }), "backup");
    expect(rowsOf()).toHaveLength(1);
    expect(screen.getByText("Job completed")).toBeInTheDocument();

    await userEvent.type(screen.getByRole("searchbox", { name: "Search events" }), "zzz");
    expect(screen.getByText(/No events match/)).toBeInTheDocument();
  });

  it("adds events from the live stream", async () => {
    renderApp("/events");
    await screen.findByRole("list", { name: "Events, newest first" });
    act(() => {
      FakeWebSocket.latest().send({ type: "ready" });
      FakeWebSocket.latest().send({
        type: "event",
        event: {
          id: "live-1",
          type: "Warning",
          reason: "FailedScheduling",
          message: "0/3 nodes are available",
          object: { kind: "Pod", namespace: "shop", name: "web-9" },
          count: 1,
          time: new Date().toISOString(),
        },
      });
    });
    expect(await screen.findByText("0/3 nodes are available")).toBeInTheDocument();
    expect(rowsOf()[0]).toHaveTextContent("0/3 nodes are available");
  });
});

describe("eventObjectPath", () => {
  it.each([
    [{ kind: "Pod", namespace: "a", name: "p" }, "/workloads/pods/a/p"],
    [{ kind: "Node", name: "w1" }, "/nodes/w1"],
    [{ kind: "StatefulSet", namespace: "a", name: "db" }, "/workloads/statefulsets/a/db"],
    [{ kind: "Service", namespace: "a", name: "web" }, "/resources/core/v1/services/a/web"],
    [{ kind: "PersistentVolume", name: "pv1" }, "/resources/core/v1/persistentvolumes/_/pv1"],
    [{ kind: "Widget", namespace: "a", name: "w" }, undefined],
  ])("%o", (o, want) => {
    expect(eventObjectPath(o)).toBe(want);
  });
});
