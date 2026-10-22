import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderApp } from "../test/renderApp";
import { nameOf } from "./cluster";
import { DemoBanner } from "./DemoBanner";
import { installDemo } from "./install";
import { DemoServer } from "./server";

afterEach(() => {
  vi.unstubAllGlobals();
});

/** The real app, answered by the demo's API instead of the Go server. */
function renderDemo(path: string) {
  const server = new DemoServer();
  return renderApp(path, (url, init) =>
    server.handle(
      init?.method ?? "GET",
      url,
      typeof init?.body === "string" ? init.body : undefined,
    ),
  );
}

describe("the website demo", () => {
  it("shows the real Overview with the demo cluster's story", async () => {
    renderDemo("/");
    const page = screen.getByRole("main");
    expect(
      await within(page).findByText(/Kubernetes v1\.34 · 4 nodes · 9 namespaces/),
    ).toBeInTheDocument();
    expect(within(page).getByText("3 things need attention")).toBeInTheDocument();
    expect(within(page).getByRole("meter", { name: "CPU" })).toBeInTheDocument();
    expect(within(page).getByRole("meter", { name: "Network" })).toBeInTheDocument();
    expect((await within(page).findAllByText("CrashLoopBackOff")).length).toBeGreaterThan(0);
  });

  it("opens a crash-looping pod's page", async () => {
    const server = new DemoServer();
    const pod = server.cluster
      .objects("/pods")
      .find((p) => JSON.stringify(p).includes("CrashLoopBackOff"));
    const name = nameOf(pod ?? {});
    renderApp(`/workloads/pods/payments/${name}`, (url, init) =>
      server.handle(init?.method ?? "GET", url),
    );
    expect(await screen.findByRole("heading", { name })).toBeInTheDocument();
    expect((await screen.findAllByText("CrashLoopBackOff")).length).toBeGreaterThan(0);
  });

  it("answers the app's fetch calls in the page, without the network", async () => {
    const network = vi.fn(() => Promise.resolve(new Response("network")));
    vi.stubGlobal("fetch", network);
    const server = installDemo(new DemoServer());
    server.stop();
    const res = await fetch("/api/v1/session");
    expect(await res.json()).toMatchObject({ user: { username: "demo" } });
    expect(network).not.toHaveBeenCalled();
    expect(new WebSocket("ws://localhost/api/v1/clusters/default/stream").constructor.name).toBe(
      "DemoSocket",
    );
  });
});

describe("DemoBanner", () => {
  it("says it's sample data and links to the install steps", async () => {
    render(<DemoBanner />);
    const banner = screen.getByRole("complementary", { name: "About this demo" });
    expect(banner).toHaveTextContent(/Sample data in your browser/);
    expect(within(banner).getByRole("link", { name: /Install/ })).toHaveAttribute(
      "href",
      "/#install",
    );
    await userEvent.tab();
    expect(within(banner).getByRole("link", { name: "glowpod.dev" })).toHaveFocus();
  });
});
