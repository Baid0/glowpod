import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Session } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

// xterm.js needs a real canvas; the drawer's own behaviour is what's tested.
vi.mock("../terminal/XTerm", () => ({
  default: ({ url, label }: { url: string; label: string }) => (
    <div role="application" aria-label={label} data-url={url} />
  ),
}));

const hubSession: Session = {
  ...fx.session,
  mode: "in-cluster",
  method: "password",
  user: { username: "glowpod:admin" },
  hub: { role: "admin" },
};

describe("cluster shell", () => {
  it("opens a drawer with a shell for the cluster on screen, and closes it", async () => {
    const user = userEvent.setup();
    renderApp("/", (url) =>
      url.pathname === "/api/v1/session" ? Response.json(hubSession) : undefined,
    );
    await user.click(await screen.findByRole("button", { name: "Open a shell for this cluster" }));
    const drawer = await screen.findByRole("region", { name: "Shell for default" });
    const term = await within(drawer).findByRole("application", { name: "Shell for default" });
    expect(term.getAttribute("data-url")).toMatch(/\/api\/v1\/hub\/shell\/default$/);
    expect(within(drawer).getByText("Starting")).toBeInTheDocument();

    await user.click(within(drawer).getByRole("button", { name: "Close the shell" }));
    expect(screen.queryByRole("region", { name: "Shell for default" })).not.toBeInTheDocument();
  });

  it("isn't offered outside a Hub", async () => {
    renderApp("/");
    await screen.findByRole("button", { name: "Search and commands" });
    expect(
      screen.queryByRole("button", { name: "Open a shell for this cluster" }),
    ).not.toBeInTheDocument();
  });
});
