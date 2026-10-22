import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as fx from "../../test/fixtures";
import { FakeWebSocket, renderApp } from "../../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const LIST = "/api/v1/clusters/default/resources/core/v1/pods";

function respondWith(handler: (url: URL) => Response | undefined) {
  return (url: URL) => (url.pathname === LIST ? handler(url) : undefined);
}

const pods = () => screen.getByRole("region", { name: "pods" });
const rowNames = () =>
  within(pods())
    .getAllByRole("row")
    .slice(1)
    .map((r) => within(r).queryByRole("link")?.textContent)
    .filter(Boolean);

describe("Resource list", () => {
  it("shows kubectl's columns, a namespace column, status pills and links", async () => {
    renderApp(
      "/workloads/pods",
      respondWith(() => Response.json(fx.podTable())),
    );

    expect(
      await within(pods()).findByRole("link", { name: "payments-api-7d9f6b-x2kq9" }),
    ).toHaveAttribute("href", "/workloads/pods/payments/payments-api-7d9f6b-x2kq9");
    const headers = within(pods())
      .getAllByRole("columnheader")
      .map((h) => h.textContent);
    // Deep shows the -o wide Node column; Namespace is added across all namespaces.
    expect(headers).toEqual(["Name", "Namespace", "Ready", "Status", "Restarts", "Age", "Node"]);
    expect(within(pods()).getByText("CrashLoopBackOff").parentElement).toHaveAttribute(
      "data-tone",
      "critical",
    );
    expect(within(pods()).getByText("Running").parentElement).toHaveAttribute("data-tone", "ok");
    expect(within(pods()).getByText("3 pods")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Pods" })).toBeInTheDocument();
  });

  it("Glance keeps to kubectl's default columns and hides the selector", async () => {
    window.localStorage.setItem("glowpod.mode", "glance");
    renderApp(
      "/workloads/pods",
      respondWith(() => Response.json(fx.podTable())),
    );
    await within(pods()).findByRole("link", { name: "search-indexer-0" });
    const headers = within(pods())
      .getAllByRole("columnheader")
      .map((h) => h.textContent);
    expect(headers).not.toContain("Node");
    expect(screen.queryByRole("textbox", { name: "Label selector" })).not.toBeInTheDocument();
  });

  it("sorts by a column, searches loaded rows and hides columns", async () => {
    const user = userEvent.setup();
    renderApp(
      "/workloads/pods",
      respondWith(() => Response.json(fx.podTable())),
    );
    await within(pods()).findByRole("link", { name: "search-indexer-0" });

    await user.click(within(pods()).getByRole("button", { name: /Restarts/ }));
    // Numbers sort high to low first: the pods restarting most come first.
    expect(rowNames()).toEqual([
      "payments-api-7d9f6b-x2kq9",
      "search-indexer-0",
      "checkout-web-6f4b9-m8zr2",
    ]);
    expect(within(pods()).getByRole("columnheader", { name: /Restarts/ })).toHaveAttribute(
      "aria-sort",
      "descending",
    );

    await user.type(screen.getByRole("searchbox", { name: "Search loaded pods" }), "pending");
    expect(rowNames()).toEqual(["search-indexer-0"]);
    expect(within(pods()).getByText("1 of 3 pods")).toBeInTheDocument();

    await user.click(within(pods()).getByText("Columns"));
    await user.click(within(pods()).getByRole("checkbox", { name: "Node" }));
    expect(within(pods()).queryByRole("columnheader", { name: /Node/ })).not.toBeInTheDocument();
  });

  it("filters by namespace and label selector on the server", async () => {
    const user = userEvent.setup();
    window.localStorage.setItem("glowpod.namespace", "payments");
    const { fetched } = renderApp(
      "/workloads/pods",
      respondWith(() => Response.json(fx.podTable())),
    );
    await within(pods()).findByRole("link", { name: "search-indexer-0" });
    expect(fetched("/resources/core/v1/pods?namespace=payments&limit=100")).toHaveLength(1);
    // One namespace: no Namespace column.
    expect(
      within(pods()).queryByRole("columnheader", { name: /Namespace/ }),
    ).not.toBeInTheDocument();

    await user.type(screen.getByRole("textbox", { name: "Label selector" }), "app=api{Enter}");
    await waitFor(() => {
      expect(fetched("labelSelector=app%3Dapi")).toHaveLength(1);
    });
    await waitFor(() => {
      expect(FakeWebSocket.latest().url).toContain(
        "/watch/core/v1/pods?namespace=payments&labelSelector=app%3Dapi",
      );
    });
    await user.click(screen.getByRole("button", { name: "Clear label selector" }));
    expect(screen.getByRole("textbox", { name: "Label selector" })).toHaveValue("");
  });

  it("loads more pages with the continue token", async () => {
    const user = userEvent.setup();
    const { fetched } = renderApp(
      "/workloads/pods",
      respondWith((url) =>
        url.searchParams.get("continue") === "page-2"
          ? Response.json(fx.podTable({ names: ["late-pod-a", "late-pod-b"] }))
          : Response.json(fx.podTable({ next: "page-2", remaining: 2 })),
      ),
    );
    await within(pods()).findByText("3 pods · 2 more");
    // The virtual list may already fetch page 2 as the last rows come into view.
    const more = within(pods()).queryByRole("button", { name: "Load more" });
    if (more) await user.click(more);
    expect(await within(pods()).findByRole("link", { name: "late-pod-b" })).toBeInTheDocument();
    expect(fetched("continue=page-2")).toHaveLength(1);
    expect(within(pods()).queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("refreshes when the watch says something changed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let version = 0;
      const { fetched } = renderApp(
        "/workloads/pods",
        respondWith(() =>
          Response.json(fx.podTable({ names: version === 0 ? ["api-1"] : ["api-1", "api-2"] })),
        ),
      );
      await within(pods()).findByRole("link", { name: "api-1" });
      const ws = FakeWebSocket.instances.find((w) => w.url.includes("/watch/core/v1/pods"));
      if (!ws) throw new Error("no list watch opened");
      act(() => {
        ws.send({ type: "ready" });
      });
      expect(await within(pods()).findByText("Live")).toBeInTheDocument();

      version = 1;
      act(() => {
        ws.send({
          type: "changed",
          op: "added",
          ref: { kind: "Pod", namespace: "payments", name: "api-2" },
        });
        ws.send({
          type: "changed",
          op: "modified",
          ref: { kind: "Pod", namespace: "payments", name: "api-1" },
        });
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1_100);
      });
      expect(await within(pods()).findByRole("link", { name: "api-2" })).toBeInTheDocument();
      // Two changes in a burst: one refetch.
      expect(fetched("/resources/core/v1/pods?")).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each([
    {
      name: "forbidden",
      res: () => Response.json({ error: { code: "forbidden", message: "x" } }, { status: 403 }),
      text: "You can't list pods. Ask an admin for access.",
    },
    {
      name: "bad selector",
      res: () =>
        Response.json(
          { error: { code: "bad_selector", message: "That label selector isn't valid: bad" } },
          { status: 400 },
        ),
      text: "That label selector isn't valid: bad",
    },
    {
      name: "empty",
      res: () => Response.json(fx.podTable({ names: [] })),
      text: "No pods yet. Nothing growing here.",
    },
  ])("explains $name", async ({ res, text }) => {
    renderApp("/workloads/pods", respondWith(res));
    expect(await within(pods()).findByText(text)).toBeInTheDocument();
  });

  it("works for any resource by URL, CRDs included", async () => {
    const table = fx.podTable({ names: ["guestbook"] });
    table.type = {
      ...table.type,
      group: "argoproj.io",
      version: "v1alpha1",
      resource: "applications",
      kind: "Application",
    };
    renderApp("/resources/argoproj.io/v1alpha1/applications", (url) =>
      url.pathname === "/api/v1/clusters/default/resources/argoproj.io/v1alpha1/applications"
        ? Response.json(table)
        : undefined,
    );
    const list = await screen.findByRole("region", { name: "applications" });
    expect(await within(list).findByRole("link", { name: "guestbook" })).toHaveAttribute(
      "href",
      "/resources/argoproj.io/v1alpha1/applications/payments/guestbook",
    );
    expect(screen.getByRole("heading", { level: 1, name: "applications" })).toBeInTheDocument();
  });
});

describe("Resource types", () => {
  it("lists custom resource types by group", async () => {
    renderApp("/extensions/crds", (url) =>
      url.pathname === "/api/v1/clusters/default/resources"
        ? Response.json({
            types: [
              {
                group: "",
                version: "v1",
                resource: "pods",
                kind: "Pod",
                namespaced: true,
                verbs: ["list"],
              },
              {
                group: "argoproj.io",
                version: "v1alpha1",
                resource: "applications",
                kind: "Application",
                namespaced: true,
                verbs: ["list"],
                shortNames: ["app"],
              },
              {
                group: "bitnami.com",
                version: "v1alpha1",
                resource: "sealedsecrets",
                kind: "SealedSecret",
                namespaced: true,
                verbs: ["list"],
              },
            ],
          })
        : undefined,
    );
    const argo = await screen.findByRole("region", { name: "argoproj.io" });
    expect(within(argo).getByRole("link", { name: /Application/ })).toHaveAttribute(
      "href",
      "/resources/argoproj.io/v1alpha1/applications",
    );
    expect(screen.getByRole("region", { name: "bitnami.com" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "core" })).not.toBeInTheDocument();
  });
});
