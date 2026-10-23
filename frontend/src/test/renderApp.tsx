import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";

import { Providers } from "../app/Providers";
import { routes } from "../app/routes";
import { createQueryClient } from "../lib/queries";
import * as fx from "./fixtures";

/** A WebSocket the test drives by hand. */
export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  onmessage: ((e: MessageEvent<string>) => void) | null = null;
  onclose: (() => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  close() {
    this.closed = true;
  }

  send(msg: unknown) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(msg) }));
  }

  drop() {
    this.onclose?.();
  }

  static latest(): FakeWebSocket {
    const ws = FakeWebSocket.instances.at(-1);
    if (!ws) throw new Error("no WebSocket was opened");
    return ws;
  }
}

type Responder = (url: URL, init?: RequestInit) => Response | undefined;

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  return input instanceof URL ? input.href : input.url;
}

/** Answers the API like the Go server would, from fixtures. */
export function defaultResponder(url: URL): Response | undefined {
  const ns = url.searchParams.get("namespace") ?? "";
  switch (url.pathname) {
    case "/api/v1/session":
      return Response.json(fx.session);
    case "/api/v1/clusters":
      return Response.json({ clusters: fx.clusters });
    case "/api/v1/clusters/default/namespaces":
      return Response.json({ namespaces: fx.namespaces });
    case "/api/v1/clusters/default/overview":
      return Response.json(fx.overview(ns));
    case "/api/v1/clusters/default/apps":
      return Response.json({ apps: fx.overview(ns).apps });
    case "/api/v1/clusters/default/events":
      return Response.json({ events: fx.events() });
    case "/api/v1/clusters/default/search":
      return Response.json(fx.search(url.searchParams.get("q") ?? ""));
    default:
      return undefined;
  }
}

/**
 * Renders the app at path. A page path ("/workloads/pods") opens on the
 * fixtures' cluster, like a link inside it; paths outside a cluster
 * ("/sign-in", "/c/…") open as given; renderApp("/", …, { home: true })
 * opens the Clusters page.
 */
export function renderApp(
  path = "/",
  respond: Responder = defaultResponder,
  { home = false }: { home?: boolean } = {},
) {
  const start = home || /^\/(c\/|sign-in)/.test(path) ? path : `/c/default${path}`;
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(urlOf(input), "http://127.0.0.1:7171");
    const res = respond(url, init) ?? defaultResponder(url);
    return Promise.resolve(
      res ?? Response.json({ error: { code: "not_found", message: "nope" } }, { status: 404 }),
    );
  });
  vi.stubGlobal("fetch", fetchMock);

  const client = createQueryClient();
  client.setDefaultOptions({ queries: { ...client.getDefaultOptions().queries, retry: false } });
  const router = createMemoryRouter(routes, { initialEntries: [start] });
  const utils = render(
    <Providers client={client}>
      <RouterProvider router={router} />
    </Providers>,
  );
  const fetched = (path: string) =>
    fetchMock.mock.calls.map(([input]) => urlOf(input)).filter((u) => u.includes(path));
  return { ...utils, router, client, fetchMock, fetched };
}
