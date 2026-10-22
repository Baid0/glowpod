// The demo's WebSocket: the live stream, resource watches, log follow and the exec shell,
// answered from the in-memory cluster in the same message shapes as the Go server.

import type { StreamMessage, WatchMessage } from "../lib/types";
import { DemoError, K, meta, namespaceOf, resourceKey, type Change } from "./cluster";
import { logInterval, logKindFor, logLine } from "./logs";
import { logTail, type DemoServer } from "./server";
import { FakeShell } from "./shell";
import { child, isObj, labelsOf, list, quantity, Random, str, text } from "./util";
import { inScope, kubeEvent, podSummary, podUsage, selectObjects } from "./views";

type Listener = ((e: Event) => void) | null;

/** Builds a WebSocket class whose connections are served by `server`. */
export function demoWebSocket(server: DemoServer) {
  return class DemoSocket {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSING = 2;
    static readonly CLOSED = 3;
    readonly CONNECTING = 0;
    readonly OPEN = 1;
    readonly CLOSING = 2;
    readonly CLOSED = 3;

    readyState = 0;
    binaryType: BinaryType = "blob";
    readonly protocol = "";
    readonly extensions = "";
    bufferedAmount = 0;
    onopen: Listener = null;
    onmessage: ((e: MessageEvent) => void) | null = null;
    onclose: Listener = null;
    onerror: Listener = null;
    private cleanups: (() => void)[] = [];
    private onInput: ((data: unknown) => void) | null = null;

    constructor(readonly url: string) {
      const t = setTimeout(() => {
        if (this.readyState !== 0) return;
        this.readyState = 1;
        // Set up the server side first, so it hears what the page sends on open.
        this.serve();
        if (this.readyState === 1) this.onopen?.(new Event("open"));
      }, 20);
      this.cleanups.push(() => {
        clearTimeout(t);
      });
    }

    send(data: unknown) {
      if (this.readyState !== 1) return;
      this.onInput?.(data);
    }

    close() {
      if (this.readyState >= 2) return;
      this.readyState = 3;
      for (const f of this.cleanups) f();
      this.cleanups = [];
      setTimeout(() => {
        this.onclose?.(new CloseEvent("close", { code: 1000 }));
      }, 0);
    }

    addEventListener() {
      // The app only uses the on* handlers.
    }

    removeEventListener() {
      // As above.
    }

    private emit(data: unknown) {
      if (this.readyState !== 1) return;
      const payload =
        typeof data === "string" || Object.prototype.toString.call(data) === "[object ArrayBuffer]"
          ? data
          : JSON.stringify(data);
      this.onmessage?.(new MessageEvent("message", { data: payload }));
    }

    private every(ms: () => number, fn: () => void) {
      let t: ReturnType<typeof setTimeout>;
      const loop = () => {
        t = setTimeout(() => {
          fn();
          if (this.readyState === 1) loop();
        }, ms());
      };
      loop();
      this.cleanups.push(() => {
        clearTimeout(t);
      });
    }

    private serve() {
      const url = new URL(this.url, "http://demo.invalid");
      const m = /^\/api\/v1\/clusters\/[^/]+\/(stream|watch|logs|exec)(?:\/(.*))?$/.exec(
        url.pathname,
      );
      const parts = (m?.[2] ?? "").split("/").map(decodeURIComponent);
      try {
        switch (m?.[1]) {
          case "stream":
            this.stream(url.searchParams.get("namespace") ?? "");
            return;
          case "watch":
            this.watch(parts, url.searchParams);
            return;
          case "logs":
            this.logs(parts[0] ?? "", parts[1] ?? "", url.searchParams);
            return;
          case "exec":
            this.exec(parts[0] ?? "", parts[1] ?? "", url.searchParams.get("container") ?? "");
            return;
        }
        this.emit({ type: "error", code: "not_found", message: "That doesn't exist." });
      } catch (err) {
        const e =
          err instanceof DemoError
            ? err
            : new DemoError(500, "demo_error", "Something went wrong in the demo.");
        this.emit({ type: "error", code: e.code, message: e.message });
      }
    }

    private subscribe(fn: (c: Change) => void) {
      this.cleanups.push(server.cluster.subscribe(fn));
    }

    /** Overview's live feed: pod changes and new events. */
    private stream(scope: string) {
      const c = server.cluster;
      this.emit({ type: "ready" } satisfies StreamMessage);
      this.subscribe((ch) => {
        if (ch.type === "event") {
          if (ch.event.object.namespace && !inScope(ch.event.object.namespace, scope)) return;
          this.emit({
            type: "event",
            op: ch.op,
            event: kubeEvent(ch.event),
          } satisfies StreamMessage);
        } else if (ch.key === K.pods && inScope(ch.namespace ?? "", scope)) {
          this.emit({
            type: "pod",
            op: ch.op,
            pod: podSummary(ch.object, c.now()),
          } satisfies StreamMessage);
        }
      });
    }

    private watch(parts: string[], q: URLSearchParams) {
      const [group = "", , resource = ""] = parts;
      const key = resourceKey(group, resource);
      const query = {
        namespace: q.get("namespace") ?? "",
        labelSelector: q.get("labelSelector") ?? "",
        fieldSelector: q.get("fieldSelector") ?? "",
      };
      this.emit({ type: "ready" } satisfies WatchMessage);
      this.subscribe((ch) => {
        if (key === K.events) {
          if (ch.type !== "event") return;
          if (ch.event.object.namespace && !inScope(ch.event.object.namespace, query.namespace))
            return;
          this.emit({
            type: "changed",
            op: ch.op === "added" ? "added" : "modified",
            ref: { kind: "Event", name: ch.event.id },
          } satisfies WatchMessage);
          return;
        }
        if (ch.type !== "object" || ch.key !== key) return;
        if (ch.op !== "deleted" && !selectObjects(server.cluster, key, query).includes(ch.object))
          return;
        if (ch.op === "deleted" && ch.namespace && !inScope(ch.namespace, query.namespace)) return;
        this.emit({
          type: "changed",
          op: ch.op,
          ref: {
            kind: ch.kind,
            ...(ch.namespace ? { namespace: ch.namespace } : {}),
            name: ch.name,
          },
        } satisfies WatchMessage);
      });
    }

    private logs(namespace: string, pod: string, q: URLSearchParams) {
      const c = server.cluster;
      const previous = q.get("previous") === "true";
      const container = q.get("container") ?? "";
      const tail = Number(q.get("tailLines") ?? 1000);
      const { lines, ended } = logTail(c, namespace, pod, container, previous, Math.min(tail, 400));
      this.emit({ type: "ready" });
      if (lines.length) this.emit({ type: "lines", lines });
      if (ended) {
        this.emit({
          type: "end",
          message: previous
            ? "That's the whole previous run."
            : "The container stopped, so the log ended.",
        });
        return;
      }
      const p = c.find(K.pods, namespace, pod);
      const uid = p ? String(meta(p).uid) : "";
      const ctr = list(child(p ?? {}, "spec").containers).find(
        (x) => !container || x.name === container,
      );
      const kind = logKindFor(text(ctr?.image) ?? "");
      const r = new Random(uid.length + Date.now());
      const base = logInterval(kind);
      this.every(
        () => base * (0.3 + r.next() * 1.4),
        () => {
          const now = c.now();
          const n = r.next() < 0.2 ? 2 : 1;
          const out = Array.from({ length: n }, (_, i) => {
            const at = now - (n - 1 - i) * 7;
            return { t: new Date(at).toISOString(), x: logLine(kind, at, r) };
          });
          this.emit({ type: "lines", lines: out });
        },
      );
      this.subscribe((ch) => {
        if (ch.type === "object" && ch.key === K.pods && String(meta(ch.object).uid) === uid) {
          const crashed = list(child(ch.object, "status").containerStatuses).some(
            (s) => isObj(s.state) && !isObj(s.state.running),
          );
          if (ch.op === "deleted" || crashed) {
            this.emit({ type: "end", message: "The container stopped, so the log ended." });
          }
        }
      });
    }

    private exec(namespace: string, pod: string, container: string) {
      const c = server.cluster;
      const p = c.find(K.pods, namespace, pod);
      if (!p)
        throw new DemoError(404, "not_found", `There's no pod called ${pod} in ${namespace}.`);
      const ctr =
        list(child(p, "spec").containers).find((x) => x.name === container) ??
        list(child(p, "spec").containers)[0];
      const status = list(child(p, "status").containerStatuses).find((s) => s.name === ctr?.name);
      const running =
        isObj(status?.state) && isObj(status.state.running) ? status.state.running : undefined;
      const encoder = new TextEncoder();
      const decoder = new TextDecoder();
      this.onInput = (data) => {
        if (typeof data !== "string") return;
        let msg: unknown;
        try {
          msg = JSON.parse(data);
        } catch {
          return;
        }
        if (!isObj(msg) || msg.type !== "start") return;
        if (!running) {
          this.emit({
            type: "error",
            code: "not_running",
            message: `The container isn't running, so there's no shell to open.`,
          });
          this.close();
          return;
        }
        const env: Record<string, string> = {};
        for (const e of list(ctr?.env)) {
          const from =
            isObj(e.valueFrom) && isObj(e.valueFrom.secretKeyRef)
              ? e.valueFrom.secretKeyRef
              : undefined;
          if (from) {
            const secret = c.find(K.secrets, namespace, String(from.name));
            const v = isObj(secret?.data) ? text(secret.data[String(from.key)]) : undefined;
            env[String(e.name)] = v ? atob(v) : "";
          } else env[String(e.name)] = str(e.value, "");
        }
        for (const ref of list(ctr?.envFrom)) {
          const cm = isObj(ref.configMapRef)
            ? c.find(K.configmaps, namespace, String(ref.configMapRef.name))
            : undefined;
          for (const [k, v] of Object.entries(labelsOf(cm ?? {}, "data")))
            if (!k.includes(".")) env[k] = v;
        }
        const limits =
          isObj(ctr?.resources) && isObj(ctr.resources.limits) ? ctr.resources.limits : {};
        const shell = new FakeShell(
          {
            pod,
            namespace: namespaceOf(p),
            container: str(ctr?.name, "app"),
            image: str(ctr?.image, ""),
            node: str(child(p, "spec").nodeName, ""),
            ip: str(child(p, "status").podIP, ""),
            env,
            memory: {
              used: podUsage(p, c.now()).memory,
              limit: quantity(limits.memory) || 2 * 1024 ** 3,
            },
            startedAt: Date.parse(String(running.startedAt)),
            now: () => c.now(),
          },
          (s) => {
            this.emit(encoder.encode(s).buffer);
          },
          (code) => {
            this.emit({
              type: "exit",
              exit: code,
              message: code ? `The shell exited with code ${String(code)}.` : "The shell exited.",
            });
            setTimeout(() => {
              this.serverClose();
            }, 0);
          },
        );
        this.onInput = (d) => {
          if (typeof d === "string") return; // resize messages
          const bytes =
            d instanceof ArrayBuffer
              ? new Uint8Array(d)
              : ArrayBuffer.isView(d)
                ? new Uint8Array(d.buffer, d.byteOffset, d.byteLength)
                : undefined;
          if (bytes) shell.input(decoder.decode(bytes));
        };
        this.emit({ type: "ready" });
        shell.start();
      };
      this.subscribe((ch) => {
        if (ch.type === "object" && ch.key === K.pods && ch.op === "deleted" && ch.object === p) {
          this.emit({
            type: "exit",
            exit: 137,
            message: "The pod was deleted, so the shell ended.",
          });
          this.serverClose();
        }
      });
    }

    /** The server ends the connection, as the Go server does after "end" or "exit". */
    private serverClose() {
      if (this.readyState >= 2) return;
      this.readyState = 3;
      for (const f of this.cleanups) f();
      this.cleanups = [];
      this.onclose?.(new CloseEvent("close", { code: 1000 }));
    }
  };
}
