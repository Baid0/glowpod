import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { podDetail } from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";
import { parseExecMessage } from "./protocol";
import { withStyleNonce } from "./styleNonce";

// xterm.js needs a real canvas; the panel's own behaviour is what's tested.
vi.mock("./XTerm", () => ({
  default: ({ url, label }: { url: string; label: string }) => (
    <div role="application" aria-label={label} data-url={url} />
  ),
}));

const BASE = "/api/v1/clusters/default";

function setup(canExec: boolean) {
  return renderApp("/workloads/pods/payments/payments-api-7d9f6b-x2kq9?tab=terminal", (url) => {
    switch (url.pathname) {
      case `${BASE}/resources`:
        return Response.json({
          types: [
            {
              group: "",
              version: "v1",
              resource: "pods",
              kind: "Pod",
              namespaced: true,
              verbs: ["get", "list", "delete"],
              subresources: ["exec", "log"],
            },
          ],
        });
      case `${BASE}/access`:
        return Response.json({ allowed: [false, false, false, true, canExec] });
      case `${BASE}/pods/payments/payments-api-7d9f6b-x2kq9`:
        return Response.json(podDetail());
      default:
        return undefined;
    }
  });
}

describe("terminal tab", () => {
  afterEach(() => {
    document.head.querySelector('meta[name="csp-nonce"]')?.remove();
  });

  it("is there only for people allowed to exec", async () => {
    setup(false);
    await screen.findByRole("tab", { name: "Logs" });
    expect(screen.queryByRole("tab", { name: "Terminal" })).not.toBeInTheDocument();
  });

  it("opens a shell on a click, in the chosen container", async () => {
    setup(true);
    const panel = await screen.findByRole("tabpanel");
    await within(panel).findByText(/Opens a shell in/);
    expect(screen.queryByRole("application")).not.toBeInTheDocument();
    await userEvent.click(within(panel).getByRole("button", { name: "Open shell" }));
    const term = await screen.findByRole("application");
    expect(term.getAttribute("aria-label")).toMatch(/^Shell in /);
    expect(term.dataset.url).toMatch(/\/exec\/payments\/payments-api-7d9f6b-x2kq9\?container=/);
    expect(within(panel).getByRole("status")).toHaveTextContent("Connecting");
  });
});

describe("withStyleNonce", () => {
  it("tags styles created inside, and only those", () => {
    const meta = document.createElement("meta");
    meta.name = "csp-nonce";
    meta.content = "abc123";
    document.head.append(meta);

    const inside = withStyleNonce(() => [
      document.createElement("style"),
      document.createElement("div"),
    ]);
    const after = document.createElement("style");
    expect(inside[0]?.getAttribute("nonce")).toBe("abc123");
    expect(inside[1]?.hasAttribute("nonce")).toBe(false);
    expect(after.hasAttribute("nonce")).toBe(false);
    meta.remove();
  });

  it("does nothing without a nonce (vite dev)", () => {
    const el = withStyleNonce(() => document.createElement("style"));
    expect(el.hasAttribute("nonce")).toBe(false);
  });
});

describe("parseExecMessage", () => {
  it.each([
    ['{"type":"ready"}', "ready"],
    ['{"type":"exit","exit":0}', "exit"],
    ['{"type":"error","code":"no_shell"}', "error"],
    ['{"type":"other"}', null],
    ["not json", null],
  ])("%s", (raw, want) => {
    expect(parseExecMessage(raw)?.type ?? null).toBe(want);
  });
});
