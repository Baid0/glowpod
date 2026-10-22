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

function withPod(res: () => Response = () => Response.json(fx.podDetail())) {
  return (url: URL) => (url.pathname === POD ? res() : undefined);
}

describe("Pod page (Deep)", () => {
  it("tells the story, suggests a fix and shows containers in detail", async () => {
    renderApp(PATH, withPod());

    expect(
      await screen.findByRole("heading", { level: 2, name: "payments-api-7d9f6b-x2kq9" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Pod" })).toBeInTheDocument();
    const main = screen.getByRole("main");
    expect(
      await within(main).findByRole("link", { name: "Deployment payments-api" }),
    ).toHaveAttribute("href", "/resources/apps/v1/deployments/payments/payments-api");
    expect(within(main).getByRole("link", { name: "ip-10-0-1-21" })).toHaveAttribute(
      "href",
      "/nodes/ip-10-0-1-21",
    );

    // Story with the suggested fix on top.
    const story = within(main).getByRole("region", { name: "What happened" });
    expect(story).toHaveTextContent("Keeps restarting (CrashLoopBackOff)");
    expect(story).toHaveTextContent("Suggested fix: Check the logs from the last crash.");
    const steps = within(story).getAllByRole("listitem");
    expect(steps).toHaveLength(5);
    expect(steps[2]).toHaveTextContent(
      "Problem: Back-off restarting a failed container (BackOff) ×14",
    );
    expect(steps[3]).toHaveTextContent("missing env DB_URL");

    // Containers: state, image, usage against the limit, probes, ports.
    const containers = within(main).getByRole("region", { name: "Containers" });
    expect(within(containers).getByText("CrashLoopBackOff")).toBeInTheDocument();
    expect(containers).toHaveTextContent("Restarted 14 times · last exit code 1");
    expect(containers).toHaveTextContent("ghcr.io/acme/payments:v3");
    expect(within(containers).getByRole("meter", { name: "app Memory" })).toHaveAttribute(
      "aria-valuetext",
      "app Memory 78 percent, High",
    );
    expect(containers).toHaveTextContent("of 512 MiB limit");
    expect(containers).toHaveTextContent("of 250m request");
    expect(containers).toHaveTextContent("readiness GET /ready on port http, every 10s");
    expect(containers).toHaveTextContent("Ports: 8080/TCP (http)");
    expect(within(containers).getByText("sidecar")).toBeInTheDocument();

    // Conditions and volumes.
    expect(within(main).getByRole("region", { name: "Conditions" })).toHaveTextContent(
      "QoS class: Burstable",
    );
    expect(within(main).getByRole("link", { name: "payments-config" })).toHaveAttribute(
      "href",
      "/resources/core/v1/configmaps/payments/payments-config",
    );
  });

  it("switches tabs by click, keyboard and URL", async () => {
    const user = userEvent.setup();
    renderApp(PATH, withPod());
    await screen.findByRole("region", { name: "What happened" });

    await user.click(screen.getByRole("tab", { name: /Events/ }));
    expect(screen.getByRole("tab", { name: /Events/ })).toHaveAttribute("aria-selected", "true");
    const table = screen.getByRole("table", { name: "Events for this pod" });
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(table).toHaveTextContent("Back-off restarting failed container app");

    screen.getByRole("tab", { name: /Events/ }).focus();
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "YAML" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "YAML" })).toHaveFocus();
    expect(screen.getByRole("tabpanel")).toHaveTextContent(
      "The YAML view arrives in the next build step.",
    );
  });

  it("opens straight to a tab from the URL", async () => {
    renderApp(`${PATH}?tab=events`, withPod());
    expect(await screen.findByRole("table", { name: "Events for this pod" })).toBeInTheDocument();
  });

  it("watches only this pod and refreshes when it changes", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { fetched } = renderApp(PATH, withPod());
      await screen.findByRole("region", { name: "What happened" });
      const ws = FakeWebSocket.instances.find((w) => w.url.includes("/watch/core/v1/pods"));
      if (!ws) throw new Error("no pod watch");
      expect(ws.url).toContain("namespace=payments");
      expect(ws.url).toContain("fieldSelector=metadata.name%3Dpayments-api-7d9f6b-x2kq9");

      const before = fetched("/pods/payments/").length;
      act(() => {
        ws.send({ type: "ready" });
        ws.send({
          type: "changed",
          op: "modified",
          ref: { kind: "Pod", namespace: "payments", name: "payments-api-7d9f6b-x2kq9" },
        });
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(600);
      });
      await waitFor(() => {
        expect(fetched("/pods/payments/").length).toBe(before + 1);
      });
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("Pod page (Glance)", () => {
  it("keeps to the story and container states", async () => {
    window.localStorage.setItem("glowpod.mode", "glance");
    renderApp(PATH, withPod());
    const containers = await screen.findByRole("region", { name: "Containers" });
    expect(containers).toHaveTextContent("Restarted 14 times");
    expect(containers).not.toHaveTextContent("ghcr.io/acme/payments:v3");
    expect(screen.queryByRole("region", { name: "Conditions" })).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "What happened" })).toHaveTextContent(
      "Suggested fix",
    );
  });
});

describe("Pod page errors", () => {
  it("explains a pod that's gone", async () => {
    renderApp(
      PATH,
      withPod(() => Response.json({ error: { code: "not_found", message: "x" } }, { status: 404 })),
    );
    expect(await screen.findByRole("heading", { name: "This pod is gone" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "See pods in payments" })).toHaveAttribute(
      "href",
      "/workloads/pods",
    );
  });

  it("offers Retry for other errors", async () => {
    renderApp(
      PATH,
      withPod(() =>
        Response.json(
          { error: { code: "cluster_unreachable", message: "Can't reach the cluster." } },
          { status: 503 },
        ),
      ),
    );
    expect(await screen.findByText("Can't reach the cluster.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});
