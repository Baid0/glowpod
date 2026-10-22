import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { renderApp } from "../../test/renderApp";

afterEach(() => {
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.density;
});

describe("settings", () => {
  it("switches theme and density and remembers them", async () => {
    renderApp("/settings");
    const theme = await screen.findByRole("radiogroup", { name: "Theme" });
    expect(within(theme).getByRole("radio", { name: "Dark" })).toBeChecked();
    expect(document.documentElement.dataset.theme).toBeUndefined();

    await userEvent.click(within(theme).getByRole("radio", { name: "Light" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem("glowpod.theme")).toBe("light");

    // Arrow keys move the choice, like any radio group.
    await userEvent.keyboard("{ArrowRight}");
    expect(within(theme).getByRole("radio", { name: "Match system" })).toBeChecked();

    await userEvent.click(screen.getByRole("radio", { name: "Compact" }));
    expect(document.documentElement.dataset.density).toBe("compact");
  });

  it("changes the refresh interval", async () => {
    renderApp("/settings");
    await userEvent.click(await screen.findByRole("radio", { name: "1 min" }));
    expect(localStorage.getItem("glowpod.refreshMs")).toBe("60000");
    await userEvent.click(screen.getByRole("radio", { name: "Off" }));
    expect(localStorage.getItem("glowpod.refreshMs")).toBe("0");
  });

  it("saves gauge thresholds for this cluster and the gauges follow", async () => {
    const { router } = renderApp("/settings");
    const card = await screen.findByRole("region", { name: /Gauge colors for/ });
    const high = within(card).getByLabelText(/High from/);
    const critical = within(card).getByLabelText(/Critical from/);

    await userEvent.clear(high);
    await userEvent.type(high, "90");
    expect(within(card).getByRole("alert")).toHaveTextContent("first smaller than the second");
    expect(within(card).getByRole("button", { name: "Save" })).toBeDisabled();

    await userEvent.clear(high);
    await userEvent.type(high, "40");
    await userEvent.clear(critical);
    await userEvent.type(critical, "60");
    await userEvent.click(within(card).getByRole("button", { name: "Save" }));
    expect(within(card).getByRole("status")).toHaveTextContent("Saved.");
    expect(JSON.parse(localStorage.getItem("glowpod.thresholds") ?? "{}")).toEqual({
      default: [0.4, 0.6],
    });

    // The fixture's CPU is at 62%: past the new critical line.
    await router.navigate("/");
    const load = await screen.findByRole("region", { name: "Cluster load" });
    expect(within(load).getByRole("meter", { name: "CPU" })).toHaveAttribute(
      "aria-valuetext",
      "CPU 62 percent, Critical",
    );

    await router.navigate("/settings");
    await userEvent.click(await screen.findByRole("button", { name: /Use the defaults/ }));
    expect(localStorage.getItem("glowpod.thresholds")).toBe("{}");
  });

  it("says who and where", async () => {
    renderApp("/settings");
    const about = await screen.findByRole("region", { name: "About" });
    expect(await within(about).findByText("kubernetes-admin")).toBeInTheDocument();
    expect(about).toHaveTextContent("Local, with your kubeconfig");
  });
});
