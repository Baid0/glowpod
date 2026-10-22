import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { podTable } from "../../../test/fixtures";
import { renderApp } from "../../../test/renderApp";
import { conditionTone } from "./logic";

const BASE = "/api/v1/clusters/default";

function show(
  path: string,
  object: Record<string, unknown>,
  extra?: (url: URL) => Response | undefined,
) {
  const seen: string[] = [];
  const app = renderApp(path, (url) => {
    seen.push(url.pathname + url.search);
    const more = extra?.(url);
    if (more) return more;
    if (url.pathname.startsWith(`${BASE}/resources/`) && url.pathname.split("/").length === 10) {
      return Response.json({ object, yaml: "kind: x\n", redacted: false });
    }
    if (url.pathname === `${BASE}/events`) return Response.json({ events: [] });
    return undefined;
  });
  return { ...app, seen };
}

describe("object overview", () => {
  it("summarises a Service and lists the pods it sends traffic to", async () => {
    const { seen } = show(
      "/resources/core/v1/services/shop/web",
      {
        kind: "Service",
        metadata: { name: "web", namespace: "shop" },
        spec: {
          type: "ClusterIP",
          clusterIP: "10.96.0.10",
          selector: { app: "web" },
          ports: [{ name: "http", port: 80, targetPort: 8080, protocol: "TCP" }],
        },
      },
      (url) =>
        url.pathname === `${BASE}/resources/core/v1/pods` ? Response.json(podTable()) : undefined,
    );
    const card = await screen.findByRole("region", { name: "Service" });
    expect(card).toHaveTextContent("10.96.0.10");
    expect(within(card).getByRole("row", { name: /80 \(http\) 8080 TCP/ })).toBeInTheDocument();
    const pods = await screen.findByRole("region", { name: "Sends traffic to" });
    expect(
      await within(pods).findByRole("link", { name: "payments-api-7d9f6b-x2kq9" }),
    ).toBeInTheDocument();
    expect(
      seen.some(
        (u) => u.includes("/resources/core/v1/pods?") && u.includes("labelSelector=app%3Dweb"),
      ),
    ).toBe(true);
    expect(
      seen.some(
        (u) => u.includes("/events?") && u.includes("kind=Service") && u.includes("name=web"),
      ),
    ).toBe(true);
  });

  it("shows a ConfigMap's keys and values", async () => {
    show("/resources/core/v1/configmaps/shop/settings", {
      kind: "ConfigMap",
      metadata: { name: "settings" },
      data: { mode: "fast", "app.yaml": "port: 8080\n" },
    });
    const card = await screen.findByRole("region", { name: "2 keys" });
    expect(card).toHaveTextContent("mode");
    expect(card).toHaveTextContent("port: 8080");
  });

  it("keeps Secret values out of the overview", async () => {
    show("/resources/core/v1/secrets/shop/db", {
      kind: "Secret",
      type: "Opaque",
      metadata: { name: "db" },
      data: { password: "" },
    });
    const card = await screen.findByRole("region", { name: "Secret" });
    expect(card).toHaveTextContent("password");
    expect(card).toHaveTextContent("Values stay hidden");
  });

  it("explains an Ingress's routes", async () => {
    show("/resources/networking.k8s.io/v1/ingresses/shop/web", {
      kind: "Ingress",
      metadata: { name: "web" },
      spec: {
        ingressClassName: "nginx",
        tls: [{ hosts: ["shop.example.com"] }],
        rules: [
          {
            host: "shop.example.com",
            http: {
              paths: [
                { path: "/api", backend: { service: { name: "api", port: { number: 8080 } } } },
              ],
            },
          },
        ],
      },
    });
    const card = await screen.findByRole("region", { name: "Routes" });
    expect(card).toHaveTextContent("https://shop.example.com");
    expect(within(card).getByRole("link", { name: "api:8080" })).toHaveAttribute(
      "href",
      "/c/default/resources/core/v1/services/shop/api",
    );
  });

  it("puts a NetworkPolicy in words", async () => {
    show("/resources/networking.k8s.io/v1/networkpolicies/shop/web", {
      kind: "NetworkPolicy",
      metadata: { name: "web" },
      spec: {
        podSelector: { matchLabels: { app: "web" } },
        policyTypes: ["Ingress", "Egress"],
        ingress: [
          { from: [{ podSelector: { matchLabels: { app: "front" } } }], ports: [{ port: 80 }] },
        ],
      },
    });
    const card = await screen.findByRole("region", { name: "Policy" });
    expect(card).toHaveTextContent("pods app=web");
    expect(card).toHaveTextContent("Allowed from pods app=front on 80/TCP");
    expect(card).toHaveTextContent("Blocked: nothing may connect out");
  });

  it("shows any custom resource's conditions", async () => {
    show("/resources/cert-manager.io/v1/certificates/shop/tls", {
      kind: "Certificate",
      metadata: { name: "tls" },
      status: {
        conditions: [
          { type: "Ready", status: "False", reason: "Pending", message: "Issuing certificate" },
        ],
      },
    });
    const card = await screen.findByRole("region", { name: "Conditions" });
    expect(card).toHaveTextContent("Ready: False");
    expect(card).toHaveTextContent("Issuing certificate");
  });
});

describe("conditionTone", () => {
  it.each([
    ["Ready", "True", "ok"],
    ["Ready", "False", "critical"],
    ["Available", "Unknown", "high"],
    ["MemoryPressure", "False", "ok"],
    ["ReplicaFailure", "True", "critical"],
    ["Degraded", "False", "ok"],
  ])("%s=%s is %s", (type, status, want) => {
    expect(conditionTone(type, status)).toBe(want);
  });
});
