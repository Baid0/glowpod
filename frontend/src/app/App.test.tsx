import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { App } from "./App";

describe("App", () => {
  it.each([
    {
      name: "server is up",
      fetchImpl: () => Promise.resolve(new Response("{}")),
      text: /connected/i,
    },
    {
      name: "server returns an error",
      fetchImpl: () => Promise.resolve(new Response("", { status: 503 })),
      text: /can't reach/i,
    },
    {
      name: "network fails",
      fetchImpl: () => Promise.reject(new TypeError("Failed to fetch")),
      text: /can't reach/i,
    },
  ])("shows the server status when $name", async ({ fetchImpl, text }) => {
    vi.stubGlobal("fetch", vi.fn(fetchImpl));

    render(<App />);

    expect(screen.getByRole("heading", { name: "Glowpod" })).toBeInTheDocument();
    expect(await screen.findByText(text)).toBeInTheDocument();
  });
});
