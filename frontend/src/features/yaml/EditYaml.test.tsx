import { EditorView } from "@codemirror/view";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderApp } from "../../test/renderApp";

const BASE = "/api/v1/clusters/default";
const original =
  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  resourceVersion: '5'\ndata:\n  mode: fast\n";
const types = [
  {
    group: "",
    version: "v1",
    resource: "configmaps",
    kind: "ConfigMap",
    namespaced: true,
    verbs: ["get", "list", "update", "delete"],
  },
  {
    group: "",
    version: "v1",
    resource: "secrets",
    kind: "Secret",
    namespaced: true,
    verbs: ["get", "list", "update", "delete"],
  },
];

interface Put {
  dryRun: boolean;
  yaml: string;
}

function setup(opts: { conflict?: boolean } = {}) {
  const puts: Put[] = [];
  const gets: string[] = [];
  const app = renderApp("/resources/core/v1/configmaps/shop/settings", (url, init) => {
    switch (url.pathname) {
      case `${BASE}/resources`:
        return Response.json({ types });
      case `${BASE}/access`:
        return Response.json({ allowed: [true, false, false, true, false] });
      case `${BASE}/resources/core/v1/configmaps/settings`:
      case `${BASE}/resources/core/v1/secrets/db`: {
        if (init?.method === "PUT") {
          const body = JSON.parse(init.body as string) as { yaml: string };
          const dryRun = url.searchParams.get("dryRun") === "true";
          puts.push({ dryRun, yaml: body.yaml });
          if (!dryRun && opts.conflict) {
            return Response.json(
              { error: { code: "conflict", message: "Someone changed this since you opened it." } },
              { status: 409 },
            );
          }
          return Response.json(
            dryRun
              ? { dryRun: true, before: original, after: body.yaml }
              : { dryRun: false, object: { object: {}, yaml: body.yaml, redacted: false } },
          );
        }
        gets.push(url.search);
        const secret = url.pathname.endsWith("/db");
        return Response.json({
          object: {
            kind: secret ? "Secret" : "ConfigMap",
            metadata: { name: secret ? "db" : "settings", namespace: "shop" },
            ...(secret
              ? { data: { password: url.searchParams.get("reveal") ? "aHVudGVy" : "" } }
              : {}),
          },
          yaml: original,
          redacted: secret && !url.searchParams.get("reveal"),
        });
      }
      default:
        return undefined;
    }
  });
  return { ...app, puts, gets };
}

/** Types into the CodeMirror editor the way a keyboard would end up. */
async function edit(change: (doc: string) => string) {
  const content = await screen.findByRole("textbox", { name: /Edit YAML of/ });
  const view = EditorView.findFromDOM(content);
  if (!view) throw new Error("no editor");
  act(() => {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: change(view.state.doc.toString()) },
    });
  });
}

describe("editing YAML", () => {
  it("reviews with a dry run, then applies", async () => {
    const { puts } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Edit YAML" }));
    const review = screen.getByRole("button", { name: "Review changes" });
    expect(review).toBeDisabled();

    await edit((d) => d.replace("mode: fast", "mode: slow"));
    expect(screen.getByText("Editing · unsaved changes")).toBeInTheDocument();
    await userEvent.click(review);

    expect(await screen.findByText(/server-side dry run/)).toBeInTheDocument();
    expect(puts).toEqual([
      { dryRun: true, yaml: expect.stringContaining("mode: slow") as unknown },
    ]);

    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(await screen.findByText("Saved settings.")).toBeInTheDocument();
    expect(puts[1]).toMatchObject({ dryRun: false });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Apply" })).not.toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "Edit YAML" })).toBeInTheDocument();
  });

  it("offers to start over when someone else changed it", async () => {
    setup({ conflict: true });
    await userEvent.click(await screen.findByRole("button", { name: "Edit YAML" }));
    await edit((d) => d + "# note\n");
    await userEvent.click(screen.getByRole("button", { name: "Review changes" }));
    await userEvent.click(await screen.findByRole("button", { name: "Apply" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Someone changed this");
    await userEvent.click(
      screen.getByRole("button", { name: "Start over from the latest version" }),
    );
    await waitFor(() => {
      expect(screen.getByText("Editing")).toBeInTheDocument();
    });
  });

  it("asks before leaving with unsaved changes", async () => {
    const { router } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "Edit YAML" }));
    await edit((d) => d + "# note\n");

    act(() => {
      void router.navigate("/");
    });
    const dialog = await screen.findByRole("dialog", { name: "Leave without saving?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe(
      "/c/default/resources/core/v1/configmaps/shop/settings",
    );

    act(() => {
      void router.navigate("/");
    });
    await userEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard changes" }),
    );
    expect(router.state.location.pathname).toBe("/c/default/");
  });

  it("reveals a Secret's values before editing it", async () => {
    const { gets, router } = setup();
    act(() => {
      void router.navigate("/resources/core/v1/secrets/shop/db");
    });
    await userEvent.click(await screen.findByRole("button", { name: "Reveal and edit" }));
    await screen.findByRole("textbox", { name: "Edit YAML of db" });
    expect(gets.some((q) => q.includes("reveal=true"))).toBe(true);

    // Hiding the values again leaves the editor.
    await userEvent.click(screen.getByRole("button", { name: "Hide values" }));
    expect(screen.queryByRole("textbox", { name: "Edit YAML of db" })).not.toBeInTheDocument();
  });
});
