import { EditorView } from "@codemirror/view";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { ApplyItem } from "../../lib/types";
import { renderApp } from "../../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const BASE = "/api/v1/clusters/default";
const LIST = `${BASE}/resources/core/v1/configmaps`;

const configMaps = (verbs = ["create", "get", "list", "update", "delete"]) => ({
  group: "",
  version: "v1",
  resource: "configmaps",
  kind: "ConfigMap",
  namespaced: true,
  verbs,
});

interface Sent {
  path: string;
  dryRun: boolean;
  force: boolean;
  namespace: string | null;
  yaml: string;
}

function setup(
  opts: {
    allowed?: boolean;
    verbs?: string[];
    check?: (yaml: string, force: boolean) => Response;
    apply?: (yaml: string, force: boolean, dryRun: boolean) => ApplyItem[];
  } = {},
) {
  window.localStorage.setItem("glowpod.namespace", "shop");
  const sent: Sent[] = [];
  const accessChecks: unknown[] = [];
  const type = configMaps(opts.verbs);
  const app = renderApp("/resources/core/v1/configmaps", (url, init) => {
    const post = init?.method === "POST";
    const body = post
      ? (JSON.parse(init.body as string) as { yaml?: string; checks?: unknown[] })
      : {};
    const record = (): Sent => {
      const s: Sent = {
        path: url.pathname,
        dryRun: url.searchParams.get("dryRun") === "true",
        force: url.searchParams.get("force") === "true",
        namespace: url.searchParams.get("namespace"),
        yaml: body.yaml ?? "",
      };
      sent.push(s);
      return s;
    };
    switch (url.pathname) {
      case `${BASE}/resources`:
        return Response.json({ types: [type] });
      case `${BASE}/access`:
        accessChecks.push(...(body.checks ?? []));
        return Response.json({ allowed: (body.checks ?? []).map(() => opts.allowed ?? true) });
      case LIST: {
        if (!post)
          return Response.json({
            type,
            columns: [{ name: "Name", type: "string", priority: 0 }],
            rows: [],
          });
        const s = record();
        if (s.dryRun && opts.check) return opts.check(s.yaml, s.force);
        return Response.json(
          {
            dryRun: s.dryRun,
            object: {
              object: { kind: "ConfigMap", metadata: { name: "my-config-map", namespace: "shop" } },
              yaml: s.yaml,
              redacted: false,
            },
          },
          { status: 201 },
        );
      }
      case `${BASE}/apply`: {
        const s = record();
        return Response.json({
          dryRun: s.dryRun,
          items: opts.apply?.(s.yaml, s.force, s.dryRun) ?? [],
        });
      }
      default:
        return undefined;
    }
  });
  return { ...app, sent, accessChecks };
}

/** Replaces the editor's text, as typing or pasting would. */
async function setEditor(name: RegExp | string, text: string) {
  const content = await screen.findByRole("textbox", { name });
  const view = EditorView.findFromDOM(content);
  if (!view) throw new Error("no editor");
  act(() => {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
  });
}

const editorText = async (name: RegExp | string) => {
  const content = await screen.findByRole("textbox", { name });
  return EditorView.findFromDOM(content)?.state.doc.toString() ?? "";
};

describe("New", () => {
  it("shows New only when RBAC allows creating there", async () => {
    const { accessChecks } = setup({ allowed: false });
    await screen.findByRole("button", { name: "Apply YAML" });
    await waitFor(() => {
      expect(accessChecks).toContainEqual({
        verb: "create",
        group: "",
        resource: "configmaps",
        namespace: "shop",
      });
    });
    expect(screen.queryByRole("button", { name: "New ConfigMap" })).not.toBeInTheDocument();
  });

  it("shows no New for kinds the API can't create", async () => {
    const { accessChecks } = setup({ verbs: ["get", "list"] });
    await screen.findByRole("button", { name: "Apply YAML" });
    expect(screen.queryByRole("button", { name: "New ConfigMap" })).not.toBeInTheDocument();
    expect(accessChecks).not.toContainEqual(expect.objectContaining({ verb: "create" }));
  });

  it("checks with a dry run, then creates and opens the new object", async () => {
    const { sent, router } = setup();
    await userEvent.click(await screen.findByRole("button", { name: "New ConfigMap" }));
    const dialog = screen.getByRole("dialog", { name: "New ConfigMap" });
    const start = await editorText(/YAML of the new ConfigMap/);
    expect(start).toContain("kind: ConfigMap");
    expect(start).toContain("namespace: shop");

    const create = within(dialog).getByRole("button", { name: "Create" });
    expect(create).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Check" }));
    await waitFor(() => {
      expect(create).toBeEnabled();
    });
    expect(sent).toEqual([
      expect.objectContaining({ path: LIST, dryRun: true, namespace: "shop" }),
    ]);

    // Any edit needs a new check before Create.
    await setEditor(/YAML of the new ConfigMap/, start.replace("info", "debug"));
    expect(create).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Check" }));
    await waitFor(() => {
      expect(create).toBeEnabled();
    });
    await userEvent.click(create);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe(
        "/c/default/resources/core/v1/configmaps/shop/my-config-map",
      );
    });
    expect(sent.at(-1)).toMatchObject({
      dryRun: false,
      yaml: expect.stringContaining("LOG_LEVEL: debug") as string,
    });
    expect(await screen.findByText("Created ConfigMap my-config-map.")).toBeInTheDocument();
  });

  it("shows why the cluster refused, and keeps Create off", async () => {
    setup({
      check: () =>
        Response.json(
          {
            error: {
              code: "invalid",
              message: "The cluster didn't accept this. Fix the fields below and try again.",
              detail: 'ConfigMap "my-config-map" is invalid: data[bad key]: Invalid value',
            },
          },
          { status: 422 },
        ),
    });
    await userEvent.click(await screen.findByRole("button", { name: "New ConfigMap" }));
    const dialog = screen.getByRole("dialog", { name: "New ConfigMap" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Check" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("Fix the fields below");
    expect(alert).toHaveTextContent("data[bad key]: Invalid value");
    expect(within(dialog).getByRole("button", { name: "Create" })).toBeDisabled();
  });
});

describe("Apply YAML", () => {
  const two =
    "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n";
  const ok = (name: string, kind: string, result: ApplyItem["result"]): ApplyItem => ({
    kind,
    name,
    namespace: "shop",
    group: kind === "Deployment" ? "apps" : "core",
    version: "v1",
    resource: kind === "Deployment" ? "deployments" : "configmaps",
    result,
  });

  it("lists what would happen, needs every object to pass, then applies", async () => {
    const { sent } = setup({
      apply: (_, force) => [
        ok("a", "ConfigMap", "created"),
        force
          ? ok("web", "Deployment", "configured")
          : {
              ...ok("web", "Deployment", "failed"),
              code: "conflict",
              message: "Some of these fields were set another way.",
              detail: 'conflict with "kubectl": .spec.replicas',
            },
      ],
    });
    await userEvent.click(await screen.findByRole("button", { name: "Apply YAML" }));
    const dialog = screen.getByRole("dialog", { name: "Apply YAML" });
    await setEditor("YAML to apply", two);
    const apply = within(dialog).getByRole("button", { name: "Apply" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Check" }));

    const results = await within(dialog).findByRole("list", { name: "What would happen" });
    expect(
      within(results)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual([
      "Will be createdConfigMap a in shop",
      expect.stringContaining(
        "Can't applyDeployment web in shopSome of these fields were set another way.",
      ),
    ]);
    expect(within(dialog).getByRole("status")).toHaveTextContent("1 of 2 can't be applied");
    expect(apply).toBeDisabled();

    // Overwriting is a choice, and needs a fresh check.
    await userEvent.click(within(dialog).getByRole("checkbox", { name: /Overwrite fields/ }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Check" }));
    await waitFor(() => {
      expect(apply).toBeEnabled();
    });
    expect(sent.at(-1)).toMatchObject({
      path: `${BASE}/apply`,
      dryRun: true,
      force: true,
      namespace: "shop",
    });

    await userEvent.click(apply);
    const done = await screen.findByRole("dialog", { name: "Applied" });
    expect(done).toHaveTextContent("1 created, 1 updated.");
    expect(within(done).getByRole("link", { name: "Deployment web" })).toHaveAttribute(
      "href",
      "/c/default/resources/apps/v1/deployments/shop/web",
    );
    expect(sent.at(-1)).toMatchObject({ dryRun: false, force: true, yaml: two });
  });

  it("needs a new check after the text changes", async () => {
    setup({ apply: () => [ok("a", "ConfigMap", "created")] });
    await userEvent.click(await screen.findByRole("button", { name: "Apply YAML" }));
    const dialog = screen.getByRole("dialog", { name: "Apply YAML" });
    await setEditor("YAML to apply", two);
    await userEvent.click(within(dialog).getByRole("button", { name: "Check" }));
    const apply = within(dialog).getByRole("button", { name: "Apply" });
    await waitFor(() => {
      expect(apply).toBeEnabled();
    });
    await setEditor("YAML to apply", two + "# more\n");
    expect(apply).toBeDisabled();
  });
});
