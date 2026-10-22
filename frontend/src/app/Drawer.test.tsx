import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { renderApp } from "../test/renderApp";
import { DRAWER_QUERY } from "./panel";

describe("section panel on narrow screens", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "matchMedia",
      (query: string) =>
        ({
          matches: query === DRAWER_QUERY,
          media: query,
          addEventListener: () => undefined,
          removeEventListener: () => undefined,
        }) as unknown as MediaQueryList,
    );
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("is a drawer: closed at first, closed again after a pick", async () => {
    const { router } = renderApp("/workloads/deployments");
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    const rail = screen.getByRole("navigation", { name: "Sections" });

    await userEvent.click(within(rail).getByRole("button", { name: "Expand panel" }));
    const panel = screen.getByRole("complementary");
    await userEvent.click(within(panel).getByRole("link", { name: /StatefulSets/ }));
    expect(router.state.location.pathname).toBe("/c/default/workloads/statefulsets");
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
  });

  it("closes on the scrim or its own button, leaving the saved setting alone", async () => {
    renderApp("/workloads/deployments");
    const rail = screen.getByRole("navigation", { name: "Sections" });

    await userEvent.click(within(rail).getByRole("button", { name: "Expand panel" }));
    await userEvent.click(screen.getByRole("button", { name: "Close panel" }));
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();

    await userEvent.click(within(rail).getByRole("button", { name: "Expand panel" }));
    await userEvent.click(
      within(screen.getByRole("complementary")).getByRole("button", { name: "Collapse panel" }),
    );
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    expect(localStorage.getItem("glowpod.panelCollapsed")).toBeNull();
  });
});
