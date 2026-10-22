import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { DrainPlan, DrainResult, Gauge, NodeDetail } from "../../lib/types";
import { renderApp } from "../../test/renderApp";
import { conditionWord } from "./words";

const BASE = "/api/v1/clusters/default";
const ago = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
const gauge = (value: number, max: number, unit: Gauge["unit"]) => ({
  state: "live" as const,
  value,
  max,
  unit,
});

function node(over: Partial<NodeDetail> = {}): NodeDetail {
  return {
    name: "w1",
    ready: true,
    unschedulable: false,
    roles: ["worker"],
    createdAt: ago(60 * 24 * 3),
    conditions: [
      { type: "Ready", status: "True", since: ago(600), problem: false },
      {
        type: "MemoryPressure",
        status: "True",
        message: "kubelet has insufficient memory available",
        since: ago(5),
        problem: true,
      },
      { type: "DiskPressure", status: "False", since: ago(600), problem: false },
    ],
    taints: [],
    addresses: [{ type: "InternalIP", address: "192.0.2.21" }],
    info: {
      kubeletVersion: "v1.36.5",
      osImage: "Ubuntu 26.04",
      kernelVersion: "6.14",
      containerRuntime: "containerd://2.1",
      architecture: "arm64",
    },
    gauges: {
      cpu: { ...gauge(1, 4, "cores"), requested: 2 },
      memory: { ...gauge(6 * 2 ** 30, 8 * 2 ** 30, "bytes"), requested: 4 * 2 ** 30 },
      pods: gauge(12, 110, "pods"),
    },
    pods: [
      {
        namespace: "shop",
        name: "web-1",
        phase: "Running",
        health: "running",
        ready: "1/1",
        restarts: 0,
        node: "w1",
        createdAt: ago(30),
      },
    ],
    events: [],
    labels: { zone: "a" },
    ...over,
  };
}

const plan: DrainPlan = {
  evict: [
    { namespace: "shop", name: "web-1" },
    { namespace: "shop", name: "cache-1", localData: true },
  ],
  skip: [{ namespace: "kube-system", name: "proxy-x", reason: "Runs on every node (DaemonSet)." }],
};
const result: DrainResult = {
  evicted: [{ namespace: "shop", name: "web-1" }],
  blocked: [
    { namespace: "shop", name: "cache-1", reason: "A disruption budget doesn't allow it." },
  ],
  failed: [],
  skipped: plan.skip,
};

function setup(allowed = [true, true]) {
  const writes: { path: string; body: unknown }[] = [];
  const app = renderApp("/nodes/w1", (url, init) => {
    if (init?.method && init.method !== "GET") {
      writes.push({
        path: url.pathname,
        body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
      });
    }
    switch (url.pathname) {
      case `${BASE}/nodes/w1`:
        return Response.json(node());
      case `${BASE}/nodes/w1/cordon`:
        return new Response(null, { status: 204 });
      case `${BASE}/nodes/w1/drain`:
        return Response.json(init?.method === "POST" ? result : plan);
      case `${BASE}/access`: {
        const checks = (JSON.parse(init?.body as string) as { checks: unknown[] }).checks;
        // Node actions ask two questions; the generic bar asks five.
        return Response.json({
          allowed: checks.length === 2 ? allowed : [false, false, false, false, false],
        });
      }
      default:
        return undefined;
    }
  });
  return { ...app, writes };
}

describe("node page", () => {
  it("shows load, health and machine facts", async () => {
    setup();
    const load = await screen.findByRole("region", { name: "Node load" });
    expect(within(load).getByRole("meter", { name: "CPU" })).toBeInTheDocument();
    expect(within(load).getByRole("meter", { name: "Pods" })).toHaveAttribute(
      "aria-valuenow",
      "12",
    );
    expect(screen.getByText("of 110 this node can run")).toBeInTheDocument();
    expect(screen.getByText("Memory pressure")).toBeInTheDocument();
    expect(screen.getByText(/insufficient memory/)).toBeInTheDocument();
    expect(screen.getByText("No disk pressure")).toBeInTheDocument();
    expect(screen.getByText("arm64")).toBeInTheDocument();
  });

  it("cordons in one click", async () => {
    const { writes } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Cordon" }));
    expect(await screen.findByText(/Cordoned w1/)).toBeInTheDocument();
    expect(writes).toContainEqual({ path: `${BASE}/nodes/w1/cordon`, body: { cordon: true } });
  });

  it("hides node actions without the right", async () => {
    setup([false, false]);
    await screen.findByRole("region", { name: "Node load" });
    expect(screen.queryByRole("button", { name: "Cordon" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Drain" })).not.toBeInTheDocument();
  });

  it("previews a drain, needs the name, then reports what happened", async () => {
    const { writes } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Drain" }));
    const dialog = screen.getByRole("dialog", { name: "Drain w1?" });
    expect(await within(dialog).findByText(/pods move/)).toBeInTheDocument();
    expect(within(dialog).getByText(/temporary volume \(emptyDir\)/)).toBeInTheDocument();
    expect(within(dialog).getByText("kube-system/proxy-x")).toBeInTheDocument();

    const confirm = within(dialog).getByRole("button", { name: "Drain 2 pods" });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type w1 to confirm/), "w1");
    await userEvent.click(confirm);

    const done = await screen.findByRole("dialog", { name: "Drained w1" });
    expect(done).toHaveTextContent("1 pod is moving. 1 blocked by disruption budgets.");
    expect(within(done).getByRole("region", { name: "Blocked for now" })).toHaveTextContent(
      "shop/cache-1",
    );
    expect(writes.some((w) => w.path === `${BASE}/nodes/w1/drain`)).toBe(true);
  });

  it("lists the node's pods with their namespace", async () => {
    setup();
    await userEvent.click(await screen.findByRole("tab", { name: /Pods/ }));
    const row = await screen.findByRole("row", { name: /web-1/ });
    expect(row).toHaveTextContent("shop");
  });
});

describe("conditionWord", () => {
  it.each([
    ["Ready", "True", "Ready"],
    ["Ready", "Unknown", "Not ready"],
    ["MemoryPressure", "True", "Memory pressure"],
    ["PIDPressure", "False", "No PID pressure"],
    ["PIDPressure", "True", "PID pressure"],
    ["NetworkUnavailable", "False", "No network unavailable"],
  ])("%s=%s", (type, status, want) => {
    expect(conditionWord(type, status)).toBe(want);
  });
});
