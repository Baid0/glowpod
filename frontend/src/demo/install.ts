import { DemoServer } from "./server";
import { demoWebSocket } from "./socket";

/** A short wait, like a round trip to a real server, so loading states show for a moment. */
function latency(signal: AbortSignal | null | undefined): Promise<void> {
  return new Promise((resolve, reject) => {
    const t = setTimeout(resolve, 60 + Math.random() * 140);
    signal?.addEventListener("abort", () => {
      clearTimeout(t);
      reject(new DOMException("The request was aborted.", "AbortError"));
    });
  });
}

function urlOf(input: RequestInfo | URL): URL {
  const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
  return new URL(raw, window.location.href);
}

/** Saves a log the demo writes, instead of following the link to an API that isn't there. */
async function download(server: DemoServer, href: string, name: string) {
  const res = server.handle("GET", new URL(href, window.location.href));
  const blob = new Blob([await res.text()], { type: "text/plain" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  setTimeout(() => {
    URL.revokeObjectURL(a.href);
  }, 1000);
}

/**
 * Points the app's API client at the in-memory cluster: fetch and WebSocket calls to /api/ are
 * answered here and never leave the browser.
 */
export function installDemo(server = new DemoServer()): DemoServer {
  const realFetch = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    if (url.origin !== window.location.origin || !url.pathname.startsWith("/api/")) {
      return realFetch(input, init);
    }
    const method = init?.method ?? (input instanceof Request ? input.method : "GET");
    const body = typeof init?.body === "string" ? init.body : undefined;
    await latency(init?.signal ?? (input instanceof Request ? input.signal : undefined));
    return server.handle(method.toUpperCase(), url, body);
  };
  window.WebSocket = demoWebSocket(server) as unknown as typeof WebSocket;

  document.addEventListener(
    "click",
    (e) => {
      const a =
        e.target instanceof Element
          ? e.target.closest<HTMLAnchorElement>("a[href^='/api/']")
          : null;
      if (!a) return;
      e.preventDefault();
      const path = new URL(a.href).pathname.split("/");
      const name = `${decodeURIComponent(path.at(-2) ?? "pod")}.log`;
      void download(server, a.href, name);
    },
    true,
  );

  server.start();
  return server;
}
