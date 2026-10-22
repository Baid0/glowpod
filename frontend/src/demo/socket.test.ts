import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { K, nameOf } from "./cluster";
import { DemoServer } from "./server";
import { demoWebSocket } from "./socket";

const API = "ws://glowpod.dev/api/v1/clusters/default";

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(Date.parse("2026-10-22T10:02:30Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

function open(server: DemoServer, url: string) {
  const Socket = demoWebSocket(server);
  const ws = new Socket(url);
  ws.binaryType = "arraybuffer";
  const messages: unknown[] = [];
  const text: string[] = [];
  let closed = false;
  ws.onmessage = (e: MessageEvent) => {
    if (typeof e.data === "string") messages.push(JSON.parse(e.data));
    else text.push(new TextDecoder().decode(e.data as ArrayBuffer));
  };
  ws.onclose = () => {
    closed = true;
  };
  vi.advanceTimersByTime(25);
  return { ws, messages, text: () => text.join(""), closed: () => closed };
}

describe("the demo's WebSocket", () => {
  it("streams pod changes and new events to the Overview", () => {
    const server = new DemoServer();
    const s = open(server, `${API}/stream?namespace=shop`);
    expect(s.messages[0]).toEqual({ type: "ready" });
    const cart = server.cluster.find(K.deployments, "shop", "cart");
    if (!cart) throw new Error("no cart");
    server.cluster.scale(K.deployments, cart, 3);
    expect(s.messages).toContainEqual(expect.objectContaining({ type: "pod", op: "added" }));
    const events = s.messages.filter(
      (m): m is { type: "event"; event: { reason: string } } =>
        (m as { type?: string }).type === "event",
    );
    expect(events.map((m) => m.event.reason)).toContain("ScalingReplicaSet");
  });

  it("tells a watch when something it covers changes, and only then", () => {
    const server = new DemoServer();
    const w = open(server, `${API}/watch/apps/v1/deployments?namespace=shop`);
    const other = server.cluster.find(K.deployments, "search", "search-api");
    const cart = server.cluster.find(K.deployments, "shop", "cart");
    if (!other || !cart) throw new Error("missing deployments");
    server.cluster.scale(K.deployments, other, 3);
    expect(w.messages).toEqual([{ type: "ready" }]);
    server.cluster.scale(K.deployments, cart, 3);
    expect(w.messages).toContainEqual({
      type: "changed",
      op: "modified",
      ref: { kind: "Deployment", namespace: "shop", name: "cart" },
    });
  });

  it("follows a log: a tail first, then new lines", () => {
    const server = new DemoServer();
    const pod = server.cluster.objects(K.pods).find((p) => nameOf(p).startsWith("checkout-"));
    const l = open(
      server,
      `${API}/logs/shop/${nameOf(pod ?? {})}?container=checkout&tailLines=1000`,
    );
    expect(l.messages[0]).toEqual({ type: "ready" });
    const tail = l.messages[1] as { lines: { t: string; x: string }[] };
    expect(tail.lines.length).toBeGreaterThan(100);
    const before = l.messages.length;
    vi.advanceTimersByTime(10_000);
    expect(l.messages.length).toBeGreaterThan(before);
    l.ws.close();
  });

  it("shows the last crash of a crash-looping container, then ends", () => {
    const server = new DemoServer();
    const pod = server.cluster
      .objects(K.pods)
      .find(
        (p) =>
          nameOf(p).startsWith("payments-api-") && JSON.stringify(p).includes("CrashLoopBackOff"),
      );
    const l = open(server, `${API}/logs/payments/${nameOf(pod ?? {})}?container=api&previous=true`);
    const lines = (l.messages[1] as { lines: { x: string }[] }).lines.map((x) => x.x);
    expect(lines.at(-2)).toContain("PAYMENTS_LEDGER_URL is not set");
    expect(l.messages.at(-1)).toEqual({ type: "end", message: "That's the whole previous run." });
  });

  it("says a pending pod has no logs yet", () => {
    const server = new DemoServer();
    const l = open(server, `${API}/logs/search/search-indexer-1?container=indexer`);
    expect(l.messages).toEqual([
      {
        type: "error",
        code: "not_started",
        message: "This container hasn't started yet, so there are no logs.",
      },
    ]);
  });

  it("opens a pretend shell that answers commands and exits", () => {
    const server = new DemoServer();
    const pod = server.cluster.objects(K.pods).find((p) => nameOf(p).startsWith("cart-"));
    const sh = open(server, `${API}/exec/shop/${nameOf(pod ?? {})}?container=cart`);
    sh.ws.send(JSON.stringify({ type: "start", csrf: "demo", cols: 80, rows: 24 }));
    expect(sh.messages).toEqual([{ type: "ready" }]);
    const type = (s: string) => {
      sh.ws.send(new TextEncoder().encode(s));
    };
    type("hostname\r");
    expect(sh.text()).toContain(nameOf(pod ?? {}));
    type("echo $REDIS_ADDR\r");
    expect(sh.text()).toContain("shop-redis-master.shop.svc.cluster.local:6379");
    type("cat /etc/resolv.conf\r");
    expect(sh.text()).toContain("search shop.svc.cluster.local");
    type("rm -rf /\r");
    expect(sh.text()).toContain("rm: Read-only file system");
    type("exit\r");
    vi.advanceTimersByTime(5);
    expect(sh.messages.at(-1)).toEqual({ type: "exit", exit: 0, message: "The shell exited." });
    expect(sh.closed()).toBe(true);
  });
});
