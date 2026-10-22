import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

afterEach(() => {
  vi.unstubAllGlobals();
});

const BASE = "/api/v1/clusters/default/resources";

const deploymentYaml = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: shop
  labels:
    app: web
spec:
  replicas: 3
`;

function secret(reveal: boolean) {
  return {
    object: {
      apiVersion: "v1",
      kind: "Secret",
      metadata: { name: "db", namespace: "shop", creationTimestamp: new Date().toISOString() },
      data: reveal ? { password: "aHVudGVyMg==", cert: "//79" } : { password: "", cert: "" },
    },
    yaml: `apiVersion: v1\nkind: Secret\ndata:\n  password: ${reveal ? "aHVudGVyMg==" : '""'}\n`,
    redacted: !reveal,
  };
}

describe("Resource detail with YAML", () => {
  it("shows what the object is and its highlighted YAML", async () => {
    const { fetched } = renderApp("/workloads/deployments/shop/web", (url) =>
      url.pathname === `${BASE}/apps/v1/deployments/web`
        ? Response.json({
            object: {
              kind: "Deployment",
              metadata: { name: "web", namespace: "shop", labels: { app: "web" } },
            },
            yaml: deploymentYaml,
            redacted: false,
          })
        : undefined,
    );
    const yaml = await screen.findByLabelText("YAML of web");
    expect(fetched(`${BASE}/apps/v1/deployments/web?namespace=shop&format=yaml`)).toHaveLength(1);
    expect(screen.getByText("Deployment", { selector: "p" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "web" })).toBeInTheDocument();
    expect(
      within(screen.getByRole("list", { name: "Labels" })).getByText("app=web"),
    ).toBeInTheDocument();
    expect(screen.getByText("9 lines · read-only")).toBeInTheDocument();
    const replicas = within(yaml).getByText("3", { selector: "[data-token]" });
    expect(replicas).toHaveAttribute("data-token", "number");
    expect(within(yaml).getByText("replicas")).toHaveAttribute("data-token", "key");
  });

  it("copies the YAML", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderApp("/resources/apps/v1/deployments/shop/web", (url) =>
      url.pathname === `${BASE}/apps/v1/deployments/web`
        ? Response.json({ object: { kind: "Deployment" }, yaml: deploymentYaml, redacted: false })
        : undefined,
    );
    await user.click(await screen.findByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith(deploymentYaml);
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("hides Secret values until Reveal, decodes them, and hides them again", async () => {
    const user = userEvent.setup();
    const { fetched } = renderApp("/resources/core/v1/secrets/shop/db", (url) =>
      url.pathname === `${BASE}/core/v1/secrets/db`
        ? Response.json(secret(url.searchParams.get("reveal") === "true"))
        : undefined,
    );
    const notice = await screen.findByText("Secret values are hidden. 2 keys.");
    expect(notice).toBeInTheDocument();
    expect(screen.queryByText("hunter2")).not.toBeInTheDocument();
    expect(fetched("reveal=true")).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Reveal values" }));
    await waitFor(() => {
      expect(fetched("reveal=true")).toHaveLength(1);
    });
    const decoded = await screen.findByRole("region", { name: "Decoded values" });
    expect(within(decoded).getByText("hunter2")).toBeInTheDocument();
    expect(within(decoded).getByText("(binary, 3 bytes)")).toBeInTheDocument();
    expect(
      screen.getByText("Secret values are showing. Hide them when you're done."),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Hide values" }));
    await waitFor(() => {
      expect(screen.queryByText("hunter2")).not.toBeInTheDocument();
    });
    expect(screen.getByText("Secret values are hidden. 2 keys.")).toBeInTheDocument();
  });

  it.each([
    { status: 403, code: "forbidden", text: "You can't read this. Ask an admin for access." },
    { status: 404, code: "not_found", text: "This object is gone." },
  ])("explains $status", async ({ status, code, text }) => {
    renderApp("/resources/core/v1/nodes/_/w9", (url) =>
      url.pathname === `${BASE}/core/v1/nodes/w9`
        ? Response.json({ error: { code, message: "x" } }, { status })
        : undefined,
    );
    expect(await screen.findByText(text)).toBeInTheDocument();
  });

  it("an unknown workload kind is a plain 404", () => {
    renderApp("/workloads/widgets/shop/x");
    expect(screen.getByText("Nothing at this address")).toBeInTheDocument();
  });
});

describe("Pod YAML tab", () => {
  it("shows the pod's YAML", async () => {
    renderApp("/workloads/pods/payments/payments-api-7d9f6b-x2kq9?tab=yaml", (url) => {
      if (url.pathname === "/api/v1/clusters/default/pods/payments/payments-api-7d9f6b-x2kq9") {
        return Response.json(fx.podDetail());
      }
      if (url.pathname === `${BASE}/core/v1/pods/payments-api-7d9f6b-x2kq9`) {
        return Response.json({
          object: { kind: "Pod" },
          yaml: "apiVersion: v1\nkind: Pod\n",
          redacted: false,
        });
      }
      return undefined;
    });
    const yaml = await screen.findByLabelText("YAML of payments-api-7d9f6b-x2kq9");
    expect(yaml).toHaveTextContent("kind: Pod");
    expect(screen.queryByRole("button", { name: "Reveal values" })).not.toBeInTheDocument();
  });
});
