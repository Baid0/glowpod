import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { GaugeDemo } from "./GaugeDemo";

describe("GaugeDemo", () => {
  afterEach(() => {
    delete document.documentElement.dataset.theme;
  });

  it("shows every gauge state", () => {
    render(<GaugeDemo />);
    const states = screen.getByRole("region", { name: "States" });
    for (const name of ["loading", "no data", "couldn't load"]) {
      expect(within(states).getByRole("img", { name: new RegExp(name) })).toBeInTheDocument();
    }
    // live and stale are meters
    expect(within(states).getAllByRole("meter")).toHaveLength(2);
  });

  it("switches to the light theme and back", async () => {
    render(<GaugeDemo />);
    await userEvent.click(screen.getByRole("button", { name: "Light theme" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    await userEvent.click(screen.getByRole("button", { name: "Dark theme" }));
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });
});
