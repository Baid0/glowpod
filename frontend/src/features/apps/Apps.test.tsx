import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as fx from "../../test/fixtures";
import { FakeWebSocket, renderApp } from "../../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const APP = "/api/v1/clusters/default/apps/payments/payments";
const withApp = (url: URL) => {
  if (url.pathname === APP) return Response.json(fx.appDetail());
  if (url.pathname.startsWith("/api/v1/clusters/default/pods/payments/"))
    return Response.json(fx.podDetail());
  return undefined;
};

describe("Apps page", () => {
  it("lists apps, troubled first, and searches them", async () => {
    const user = userEvent.setup();
    window.localStorage.setItem("glowpod.mode", "glance");
    renderApp("/apps");
    const main = screen.getByRole("main");
    const payments = await within(main).findByRole("region", { name: "payments-api" });
    expect(payments).toHaveTextContent("Failing");
    expect(within(payments).getByRole("link", { name: "payments-api" })).toHaveAttribute(
      "href",
      "/c/default/apps/payments/payments-api",
    );
    expect(within(main).getByText("2 apps")).toBeInTheDocument();

    await user.type(within(main).getByRole("searchbox", { name: "Search apps" }), "checkout");
    expect(within(main).queryByRole("region", { name: "payments-api" })).not.toBeInTheDocument();
    expect(within(main).getByText("1 of 2 apps")).toBeInTheDocument();
  });

  it("Glance's Logs page starts from apps", async () => {
    renderApp("/logs");
    const link = await screen.findByRole("link", { name: /payments-api/ });
    expect(link).toHaveAttribute("href", "/c/default/apps/payments/payments-api?tab=logs");
  });
});

describe("App page", () => {
  it("shows problems in plain words, workloads, pods and deploys", async () => {
    renderApp("/apps/payments/payments", withApp);
    const main = screen.getByRole("main");
    expect(
      await within(main).findByRole("heading", { level: 2, name: "payments" }),
    ).toBeInTheDocument();
    expect(await within(main).findByText("Failing")).toBeInTheDocument();

    const attention = within(main).getByRole("region", { name: "Needs attention" });
    expect(attention).toHaveTextContent("Keeps restarting (CrashLoopBackOff)");
    expect(attention).toHaveTextContent("Check the logs from the last crash.");
    expect(
      within(attention).getByRole("link", { name: "View payments-api-7d9f6b-x2kq9" }),
    ).toHaveAttribute("href", "/c/default/workloads/pods/payments/payments-api-7d9f6b-x2kq9");
    expect(
      within(attention).getByRole("link", { name: "Open the last crash's log" }),
    ).toHaveAttribute(
      "href",
      "/c/default/workloads/pods/payments/payments-api-7d9f6b-x2kq9?tab=logs&previous=1",
    );

    const workloads = within(main).getByRole("region", { name: "Workloads" });
    expect(within(workloads).getByRole("link", { name: "payments-db" })).toHaveAttribute(
      "href",
      "/c/default/workloads/statefulsets/payments/payments-db",
    );
    // Images show as their tag.
    expect(workloads).toHaveTextContent("17");

    const pods = within(main).getByRole("table", { name: "Pods of this app" });
    expect(within(pods).getAllByRole("row")).toHaveLength(4);
    expect(within(pods).getByText("CrashLoopBackOff")).toBeInTheDocument();

    expect(within(main).getByRole("region", { name: "Recent deploys" })).toHaveTextContent(
      "payments-api revision 3",
    );
  });

  it("Logs tab opens on the struggling pod and can switch pods", async () => {
    const user = userEvent.setup();
    renderApp("/apps/payments/payments?tab=logs", withApp);
    const picker = await screen.findByRole("combobox", { name: "Pod" });
    expect(picker).toHaveValue("payments-api-7d9f6b-x2kq9");
    await waitFor(() => {
      expect(
        FakeWebSocket.instances.some((w) =>
          w.url.includes("/logs/payments/payments-api-7d9f6b-x2kq9?container=app"),
        ),
      ).toBe(true);
    });

    await user.selectOptions(picker, "payments-db-0");
    await waitFor(() => {
      expect(
        FakeWebSocket.instances.some((w) => w.url.includes("/logs/payments/payments-db-0")),
      ).toBe(true);
    });
  });

  it("explains an app that isn't there", async () => {
    renderApp("/apps/payments/gone", (url) =>
      url.pathname === "/api/v1/clusters/default/apps/payments/gone"
        ? Response.json({ error: { code: "not_found", message: "x" } }, { status: 404 })
        : undefined,
    );
    expect(await screen.findByRole("heading", { name: "This app isn't here" })).toBeInTheDocument();
  });
});
