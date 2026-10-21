import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Overview } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const main = () => screen.getByRole("main");

function withOverview(change: (o: Overview) => Overview) {
  return (url: URL) =>
    url.pathname.endsWith("/overview") ? Response.json(change(fx.overview())) : undefined;
}

describe("Overview (Deep)", () => {
  it("matches the design board: cluster line, gauges, tiles, problems, events, nodes", async () => {
    renderApp("/");
    const page = main();

    // Cluster line
    expect(
      await within(page).findByText(/Kubernetes v1\.34 · 3 nodes · 5 namespaces/),
    ).toBeInTheDocument();
    expect(within(page).getByText("4 things need attention")).toBeInTheDocument();

    // Gauges: Network shows because Prometheus was found; Storage shows capacity only.
    const load = within(page).getByRole("region", { name: "Cluster load" });
    expect(within(load).getByRole("meter", { name: "CPU" })).toHaveAttribute(
      "aria-valuetext",
      "CPU 62 percent, OK",
    );
    expect(within(load).getByText("24.8 of 40 cores")).toBeInTheDocument();
    expect(within(load).getByText(/75% requested/)).toBeInTheDocument();
    expect(within(load).getByText(/4% in 1h/)).toBeInTheDocument();
    expect(within(load).getByRole("meter", { name: "Memory" })).toHaveAttribute(
      "aria-valuetext",
      "Memory 78 percent, High",
    );
    expect(within(load).getByText("49.9 of 64 GiB")).toBeInTheDocument();
    expect(within(load).getByText("297 of 660 pods")).toBeInTheDocument();
    expect(within(load).getByText("6 nodes × 110 max")).toBeInTheDocument();
    expect(within(load).getByRole("img", { name: "Storage: no data" })).toBeInTheDocument();
    expect(within(load).getByText("Usage isn't reported for these volumes")).toBeInTheDocument();
    expect(within(load).getByRole("meter", { name: "Network" })).toBeInTheDocument();
    expect(within(load).getByText("120")).toBeInTheDocument();
    expect(within(load).getByText("MB/s in + out")).toBeInTheDocument();

    // Workload tiles
    const workloads = within(page).getByRole("region", { name: "Workloads" });
    const deployments = within(workloads).getByRole("region", { name: "Deployments" });
    expect(deployments).toHaveTextContent("22 / 24 ready");
    expect(deployments).toHaveTextContent("2 failing");
    expect(within(workloads).getByRole("region", { name: "StatefulSets" })).toHaveTextContent(
      "1 pending",
    );
    expect(within(workloads).getByRole("region", { name: "DaemonSets" })).toHaveTextContent(
      "All ready",
    );
    expect(within(workloads).getByRole("region", { name: "Jobs (24h)" })).toHaveTextContent(
      "38 succeeded",
    );

    // Needs attention: reason codes, node pressure, View links
    const attention = within(page).getByRole("region", { name: "Needs attention" });
    const rows = within(attention).getAllByRole("listitem");
    expect(
      rows.map(
        (r) => within(r).getByText(/CrashLoopBackOff|OOMKilled|Pending|MemoryPressure/).textContent,
      ),
    ).toEqual(["CrashLoopBackOff", "OOMKilled", "Pending", "MemoryPressure"]);
    expect(rows[0]).toHaveTextContent("payments · restarted 14 times · exit code 1");
    expect(rows[0]).toHaveTextContent(/2[23] min/);
    expect(
      within(attention).getByRole("link", { name: "View payments-api-7d9f6b-x2kq9" }),
    ).toHaveAttribute("href", "/workloads/pods/payments/payments-api-7d9f6b-x2kq9");
    expect(within(attention).getByRole("link", { name: "View ip-10-0-2-14" })).toHaveAttribute(
      "href",
      "/nodes/ip-10-0-2-14",
    );

    // Recent events
    const events = within(page).getByRole("region", { name: "Recent events" });
    expect(
      await within(events).findByText("Back-off restarting failed container app"),
    ).toBeInTheDocument();
    expect(within(events).getByText("deploy/checkout-web")).toBeInTheDocument();
    expect(within(events).getByText("job/backup-nightly-29341")).toBeInTheDocument();
    expect(within(events).getByText("Problem:")).toBeInTheDocument();

    // Node load and top consumers
    const nodes = within(page).getByRole("region", { name: "Node load" });
    expect(within(nodes).getByRole("meter", { name: "ip-10-0-2-14 CPU" })).toHaveAttribute(
      "aria-valuetext",
      "ip-10-0-2-14 CPU 88 percent, Critical",
    );
    expect(within(nodes).getByText("Mem 93%")).toBeInTheDocument();
    const table = within(page).getByRole("table", { name: "Pods using the most CPU" });
    const [, first, second] = within(table).getAllByRole("row");
    expect(first).toHaveTextContent("payments-api-7d9f6b-okpayments1.3 cores512 MiB");
    expect(second).toHaveTextContent("search-indexer-1search58m15.5 MiB");
  });

  it("hides the Network gauge when Prometheus isn't found", async () => {
    renderApp(
      "/",
      withOverview((o) => {
        delete o.gauges.network;
        return o;
      }),
    );
    const load = await screen.findByRole("region", { name: "Cluster load" });
    await within(load).findByRole("meter", { name: "CPU" });
    expect(within(load).queryByRole("meter", { name: "Network" })).not.toBeInTheDocument();
  });

  it("explains missing metrics-server once, above the gauges", async () => {
    renderApp(
      "/",
      withOverview((o) => {
        o.gauges.cpu = { ...o.gauges.cpu, state: "no-metrics", value: 0 };
        o.gauges.memory = { ...o.gauges.memory, state: "no-metrics", value: 0 };
        return o;
      }),
    );
    expect(await screen.findByText(/metrics-server isn't installed/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "How to install it" })).toHaveAttribute(
      "href",
      expect.stringContaining("metrics-server"),
    );
    const load = screen.getByRole("region", { name: "Cluster load" });
    expect(within(load).getByRole("img", { name: "CPU: no data" })).toBeInTheDocument();
  });

  it("says when all is well", async () => {
    renderApp(
      "/",
      withOverview((o) => ({ ...o, problems: [], problemCount: 0 })),
    );
    expect(await screen.findByText("All 294 pods look healthy.")).toBeInTheDocument();
    expect(screen.getByText("Nothing needs attention")).toBeInTheDocument();
  });

  it("shows the first six problems and expands to all", async () => {
    const user = userEvent.setup();
    const [crash] = fx.problems();
    if (!crash) throw new Error("fixture has no problems");
    const many = Array.from({ length: 9 }, (_, i) => ({ ...crash, name: `api-${i}` }));
    renderApp(
      "/",
      withOverview((o) => ({ ...o, problems: many, problemCount: many.length })),
    );
    const attention = await screen.findByRole("region", { name: "Needs attention" });
    expect(await within(attention).findAllByRole("listitem")).toHaveLength(6);
    await user.click(within(attention).getByRole("button", { name: "See all 9" }));
    expect(within(attention).getAllByRole("listitem")).toHaveLength(9);
  });

  it("degrades a section that couldn't load", async () => {
    renderApp(
      "/",
      withOverview((o) => ({
        ...o,
        topConsumers: [],
        unavailable: [
          {
            section: "workloads",
            message: "You don't have access to this. Ask an admin for access.",
          },
          { section: "topConsumers", message: "Install metrics-server to see usage." },
        ],
      })),
    );
    expect(
      await screen.findByText("Workloads: You don't have access to this. Ask an admin for access."),
    ).toBeInTheDocument();
    expect(screen.getByText("Install metrics-server to see usage.")).toBeInTheDocument();
  });
});

describe("Overview (Glance)", () => {
  it("shows app cards, plain-language pod problems and recent deploys", async () => {
    window.localStorage.setItem("glowpod.mode", "glance");
    renderApp("/");
    const page = main();

    const apps = await within(page).findByRole("region", { name: "Your apps" });
    const payments = await within(apps).findByRole("region", { name: "payments-api" });
    expect(payments).toHaveTextContent("Failing");
    expect(payments).toHaveTextContent("3 / 4 ready");
    expect(within(apps).getByRole("region", { name: "checkout-web" })).toHaveTextContent("Healthy");

    const attention = within(page).getByRole("region", { name: "Needs attention" });
    const rows = within(attention).getAllByRole("listitem");
    expect(rows).toHaveLength(3); // pods only; node pressure is Deep detail
    expect(rows[0]).toHaveTextContent("Keeps restarting (CrashLoopBackOff)");
    expect(rows[0]).toHaveTextContent("Check the logs from the last crash.");

    const deploys = within(page).getByRole("region", { name: "Recent deploys" });
    expect(deploys).toHaveTextContent("checkout-web rolled out revision 7");
    expect(deploys).toHaveTextContent("checkout · v7");
    expect(within(page).queryByRole("region", { name: "Node load" })).not.toBeInTheDocument();
  });

  it("Show in Deep switches mode", async () => {
    const user = userEvent.setup();
    window.localStorage.setItem("glowpod.mode", "glance");
    renderApp("/");
    const apps = await within(main()).findByRole("region", { name: "Your apps" });
    await user.click(within(apps).getByRole("button", { name: "Show in Deep" }));
    expect(screen.getByRole("button", { name: "Deep" })).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findByRole("region", { name: "Node load" })).toBeInTheDocument();
  });
});

describe("Cluster unreachable", () => {
  it("shows a banner with Retry, then stale gauges once it had data", async () => {
    const user = userEvent.setup();
    let down = false;
    const { client, fetched } = renderApp("/", (url) =>
      down && url.pathname.endsWith("/overview")
        ? Response.json(
            { error: { code: "cluster_unreachable", message: "Can't reach the cluster." } },
            { status: 503 },
          )
        : undefined,
    );
    const load = await screen.findByRole("region", { name: "Cluster load" });
    await within(load).findByRole("meter", { name: "CPU" });

    down = true;
    await act(async () => {
      await client.refetchQueries({ queryKey: ["overview"] });
    });

    const banner = await screen.findByRole("alert");
    expect(banner).toHaveTextContent("Can't reach the cluster.");
    expect(banner).toHaveTextContent(/Last contact \d+ s ago/);
    expect(banner).toHaveTextContent(/Trying again in \d+ s/);
    expect(within(load).getByRole("meter", { name: "CPU" })).toHaveAttribute(
      "aria-valuetext",
      expect.stringMatching(/^CPU 62 percent, OK, updated \d+ s ago$/),
    );

    const before = fetched("/overview").length;
    down = false;
    await user.click(within(banner).getByRole("button", { name: "Retry now" }));
    await waitFor(() => {
      expect(fetched("/overview").length).toBeGreaterThan(before);
    });
    await waitFor(() => {
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });
  });
});
