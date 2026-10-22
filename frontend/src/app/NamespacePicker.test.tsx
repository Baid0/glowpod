import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { describeNamespaces, normalizeNamespaces } from "../lib/preferences";
import { podTable } from "../test/fixtures";
import { renderApp } from "../test/renderApp";

const trigger = () => screen.getByRole("button", { name: /^Namespace:/ });
const option = (name: string) =>
  within(screen.getByRole("listbox", { name: "Namespaces" })).getByRole("option", { name });

describe("picking several namespaces", () => {
  it("adds with the checkbox or Space, and every view follows the set", async () => {
    const user = userEvent.setup();
    const { fetched } = renderApp("/");
    await user.click(trigger());
    expect(screen.getByRole("listbox")).toHaveAttribute("aria-multiselectable", "true");

    // The checkbox adds without closing.
    const box = option("checkout").querySelector("[data-checked]");
    if (!box) throw new Error("no checkbox");
    await user.click(box);
    expect(option("checkout")).toHaveAttribute("aria-selected", "true");
    // The trigger's text holds still while the list is open, so nothing
    // shifts under the pointer; it updates on close.
    expect(trigger()).toHaveTextContent("All namespaces");
    expect(option("All namespaces")).toHaveAttribute("aria-selected", "false");

    // Space adds the active one and clears the search for the next name.
    const search = screen.getByRole("combobox", { name: "Search namespaces" });
    await user.type(search, "pay");
    await user.keyboard(" ");
    expect(screen.getByText("2 picked")).toBeInTheDocument();
    expect(search).toHaveValue("");

    await user.keyboard("{Escape}");
    expect(trigger()).toHaveAccessibleName("Namespace: checkout, payments");
    expect(trigger()).toHaveTextContent("checkout, payments");
    await waitFor(() => {
      expect(fetched("/overview?namespace=checkout%2Cpayments")).not.toHaveLength(0);
    });
    expect(localStorage.getItem("glowpod.namespace")).toBe("checkout,payments");
  });

  it("takes one out, clears back to all, and a row click means only that one", async () => {
    const user = userEvent.setup();
    localStorage.setItem("glowpod.namespace", "search,checkout,payments");
    renderApp("/");
    expect(trigger()).toHaveTextContent("checkout +2");

    await user.click(trigger());
    const box = option("search").querySelector("[data-checked]");
    if (!box) throw new Error("no checkbox");
    await user.click(box);
    expect(trigger()).toHaveAccessibleName("Namespace: checkout, payments");

    await user.click(screen.getByRole("button", { name: "Clear" }));
    expect(trigger()).toHaveAccessibleName("Namespace: All namespaces");

    await user.click(option("payments"));
    expect(trigger()).toHaveAccessibleName("Namespace: payments");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });
});

describe("lists over several namespaces", () => {
  it("show the namespace column and say which namespaces were left out", async () => {
    localStorage.setItem("glowpod.namespace", "checkout,payments");
    renderApp("/workloads/pods", (url) =>
      url.pathname === "/api/v1/clusters/default/resources/core/v1/pods"
        ? Response.json({ ...podTable(), skipped: ["payments"] })
        : undefined,
    );
    expect(await screen.findByRole("status")).toHaveTextContent(
      "You can't list pods in payments, so it's left out.",
    );
    expect(screen.getByRole("columnheader", { name: /Namespace/ })).toBeInTheDocument();
  });

  it("the map switches between the picked namespaces", async () => {
    const user = userEvent.setup();
    localStorage.setItem("glowpod.namespace", "checkout,payments");
    const { fetched } = renderApp("/network/map", (url) =>
      url.pathname === "/api/v1/clusters/default/map"
        ? Response.json({
            namespace: url.searchParams.get("namespace"),
            nodes: [],
            edges: [],
            truncated: false,
          })
        : undefined,
    );
    expect(await screen.findByRole("region", { name: "Map of checkout" })).toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "payments" }));
    expect(await screen.findByRole("region", { name: "Map of payments" })).toBeInTheDocument();
    expect(fetched("/map?namespace=payments")).not.toHaveLength(0);
  });
});

describe("describeNamespaces", () => {
  it.each([
    [[], "all namespaces"],
    [["shop"], "shop"],
    [["a", "b"], "a and b"],
    [["a", "b", "c"], "3 namespaces"],
  ])("%j → %s", (list, want) => {
    expect(describeNamespaces(list)).toBe(want);
  });

  it("normalizes", () => {
    expect(normalizeNamespaces(["b", "", "a", "b"])).toEqual(["a", "b"]);
  });
});
