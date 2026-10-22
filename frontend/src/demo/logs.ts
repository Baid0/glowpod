// Made-up container logs in each app's own style: JSON, logfmt, klog, nginx, Redis, Postgres.
// Mostly INFO, with the odd WARN and ERROR, like a real cluster.

import type { Random } from "./util";

export type LogKind =
  | "web"
  | "cart"
  | "checkout"
  | "redis"
  | "payments"
  | "postgres"
  | "search-api"
  | "indexer"
  | "prometheus"
  | "grafana"
  | "operator"
  | "klog"
  | "node-exporter"
  | "coredns"
  | "kube-proxy"
  | "nginx"
  | "glowpod"
  | "job";

const KINDS: [RegExp, LogKind][] = [
  [/storefront/, "web"],
  [/\/cart:/, "cart"],
  [/checkout/, "checkout"],
  [/redis/, "redis"],
  [/payments-api/, "payments"],
  [/postgres/, "postgres"],
  [/search-api/, "search-api"],
  [/search-indexer/, "indexer"],
  [/prometheus-operator/, "operator"],
  [/prometheus\/prometheus/, "prometheus"],
  [/grafana/, "grafana"],
  [/node-exporter/, "node-exporter"],
  [/coredns/, "coredns"],
  [/kube-proxy/, "kube-proxy"],
  [/ingress-nginx/, "nginx"],
  [/glowpod/, "glowpod"],
  [/ledger-tools/, "job"],
];

export function logKindFor(image: string): LogKind {
  return KINDS.find(([re]) => re.test(image))?.[1] ?? "klog";
}

/** How often a container logs, in ms between lines on average. */
export function logInterval(kind: LogKind): number {
  switch (kind) {
    case "web":
    case "nginx":
    case "coredns":
    case "cart":
    case "checkout":
      return 900;
    case "node-exporter":
    case "kube-proxy":
    case "redis":
      return 6000;
    default:
      return 2200;
  }
}

const pad = (n: number, w = 2) => String(n).padStart(w, "0");

function klogTime(d: Date): string {
  return `${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}.${pad(d.getUTCMilliseconds(), 3)}${pad(Math.floor((d.getTime() % 997) * 1.003), 3)}`;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function redisTime(d: Date): string {
  return `${pad(d.getUTCDate())} ${MONTHS[d.getUTCMonth()] ?? "Jan"} ${String(d.getUTCFullYear())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}.${pad(d.getUTCMilliseconds(), 3)}`;
}

function nginxTime(d: Date): string {
  return `${pad(d.getUTCDate())}/${MONTHS[d.getUTCMonth()] ?? "Jan"}/${String(d.getUTCFullYear())}:${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} +0000`;
}

const PATHS = [
  "/",
  "/products",
  "/products/ember-lamp",
  "/products/moss-planter",
  "/cart",
  "/api/products",
  "/api/recommendations",
  "/static/app.3f9a1c.js",
  "/healthz",
];
const AGENTS = [
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15",
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X)",
  "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0",
  "kube-probe/1.34",
];
const ITEMS = ["ember-lamp", "moss-planter", "glow-mug", "pod-poster", "lime-socks"];

function ip(r: Random): string {
  return `10.0.${String(1 + r.int(3))}.${String(10 + r.int(240))}`;
}

function id(r: Random, n = 8): string {
  return Array.from({ length: n }, () => "0123456789abcdef"[r.int(16)] ?? "0").join("");
}

/** One log line written at time `at`. */
export function logLine(kind: LogKind, at: number, r: Random): string {
  const d = new Date(at);
  const ts = d.toISOString();
  const roll = r.next();
  switch (kind) {
    case "web": {
      const path = r.pick(PATHS);
      const ms = 4 + r.int(path.startsWith("/api") ? 180 : 40);
      if (roll < 0.06)
        return JSON.stringify({
          level: "warn",
          time: ts,
          msg: "slow upstream response",
          upstream: "recommendations",
          duration_ms: 1200 + r.int(900),
        });
      if (roll < 0.08)
        return JSON.stringify({
          level: "error",
          time: ts,
          msg: "render failed",
          path: "/products/" + r.pick(ITEMS),
          error: "fetch failed: ECONNRESET",
        });
      return JSON.stringify({
        level: "info",
        time: ts,
        msg: "request",
        method: "GET",
        path,
        status: roll < 0.12 ? 404 : 200,
        duration_ms: ms,
        request_id: id(r),
      });
    }
    case "cart": {
      if (roll < 0.05)
        return JSON.stringify({
          level: "warn",
          ts,
          msg: "redis command slow",
          cmd: "HGETALL",
          duration_ms: 210 + r.int(300),
        });
      const op = r.pick(["cart.add", "cart.get", "cart.get", "cart.remove", "cart.merge"]);
      return JSON.stringify({
        level: "info",
        ts,
        msg: op,
        cart_id: "c_" + id(r, 10),
        items: 1 + r.int(5),
        duration_ms: 1 + r.int(9),
      });
    }
    case "checkout": {
      if (roll < 0.07)
        return JSON.stringify({
          level: "warn",
          ts,
          msg: "payment retried",
          attempt: 2,
          reason: "payments-api: 503 Service Unavailable",
        });
      if (roll < 0.1)
        return JSON.stringify({
          level: "error",
          ts,
          msg: "card declined",
          order_id: "o_" + id(r, 10),
          code: "insufficient_funds",
        });
      return JSON.stringify({
        level: "info",
        ts,
        msg: "order placed",
        order_id: "o_" + id(r, 10),
        total_eur: (9 + r.int(180) + r.int(100) / 100).toFixed(2),
        items: 1 + r.int(4),
      });
    }
    case "redis": {
      if (roll < 0.5)
        return `1:M ${redisTime(d)} * ${String(10 + r.int(90))} changes in 300 seconds. Saving...`;
      if (roll < 0.75)
        return `1:M ${redisTime(d)} * Background saving started by pid ${String(1200 + r.int(800))}`;
      if (roll < 0.97) return `1:M ${redisTime(d)} * Background saving terminated with success`;
      return `1:M ${redisTime(d)} # WARNING Memory overcommit must be enabled! Without it, a background save may fail under low memory condition.`;
    }
    case "payments": {
      if (roll < 0.07)
        return `time=${ts} level=WARN msg="ledger write retried" attempt=2 err="context deadline exceeded"`;
      if (roll < 0.09)
        return `time=${ts} level=ERROR msg="charge failed" order=o_${id(r, 10)} err="provider timeout after 5s"`;
      return `time=${ts} level=INFO msg="charge created" order=o_${id(r, 10)} amount_eur=${(9 + r.int(180)).toFixed(2)} provider=stripe duration=${String(80 + r.int(240))}ms`;
    }
    case "postgres": {
      const t = `${ts.slice(0, 10)} ${ts.slice(11, 23)} UTC`;
      if (roll < 0.4) return `${t} [${String(60 + r.int(40))}] LOG:  checkpoint starting: time`;
      if (roll < 0.8)
        return `${t} [${String(60 + r.int(40))}] LOG:  checkpoint complete: wrote ${String(20 + r.int(400))} buffers (${(r.next() * 2).toFixed(1)}%); 0 WAL file(s) added, 0 removed, 1 recycled`;
      if (roll < 0.95)
        return `${t} [${String(4000 + r.int(900))}] LOG:  duration: ${String(500 + r.int(1500))}.${String(r.int(999))} ms  statement: SELECT * FROM ledger_entries WHERE settled = false`;
      return `${t} [${String(4000 + r.int(900))}] ERROR:  duplicate key value violates unique constraint "charges_idempotency_key_key"`;
    }
    case "search-api": {
      if (roll < 0.07)
        return JSON.stringify({
          level: "warn",
          ts,
          msg: "query took long",
          q: r.pick(ITEMS).replace("-", " "),
          took_ms: 800 + r.int(600),
        });
      return JSON.stringify({
        level: "info",
        ts,
        msg: "search",
        q: r.pick(ITEMS).replace("-", " "),
        hits: r.int(60),
        took_ms: 3 + r.int(40),
      });
    }
    case "indexer": {
      if (roll < 0.06)
        return `${ts} WARN  segment merge slow: merged 8 segments in ${(1.5 + r.next() * 3).toFixed(1)}s`;
      return `${ts} INFO  indexed batch=${String(10000 + r.int(90000))} docs=${String(200 + r.int(800))} took=${String(40 + r.int(300))}ms`;
    }
    case "prometheus": {
      if (roll < 0.5)
        return `time=${ts} level=INFO source=head.go:1254 msg="Head GC completed" component=tsdb caller=truncateMemory duration=${String(20 + r.int(90))}.${String(r.int(999))}ms`;
      if (roll < 0.96)
        return `time=${ts} level=INFO source=compact.go:584 msg="write block" component=tsdb mint=${String(at - 7_200_000)} maxt=${String(at)} ulid=01J${id(r, 23).toUpperCase()} duration=${String(300 + r.int(900))}ms`;
      return `time=${ts} level=WARN source=scrape.go:1832 msg="Error on ingesting samples that are too old or are too far into the future" component="scrape manager" scrape_pool=serviceMonitor/monitoring/storefront/0 num_dropped=${String(1 + r.int(20))}`;
    }
    case "grafana":
      return `logger=context userId=1 orgId=1 uname=admin t=${ts} level=info msg="Request Completed" method=GET path=/api/datasources/proxy/uid/prometheus/api/v1/query_range status=200 remote_addr=${ip(r)} time_ms=${String(8 + r.int(120))} duration=${String(8 + r.int(120))}ms size=${String(900 + r.int(9000))}`;
    case "operator":
      return `level=info ts=${ts} caller=operator.go:${String(600 + r.int(400))} component=prometheus-controller key=monitoring/kube-prometheus-stack-prometheus msg="sync prometheus"`;
    case "node-exporter":
      return `time=${ts} level=INFO source=node_exporter.go:${String(100 + r.int(50))} msg="Listening on" address=[::]:9100`;
    case "coredns": {
      const names = [
        "payments-db.payments.svc.cluster.local.",
        "shop-redis-master.shop.svc.cluster.local.",
        "payments-api.payments.svc.cluster.local.",
        "api.stripe.com.",
        "search-api.search.svc.cluster.local.",
      ];
      const name = r.pick(names);
      return `[INFO] ${ip(r)}:${String(30000 + r.int(30000))} - ${String(r.int(65535))} "A IN ${name} udp ${String(60 + r.int(40))} false 512" NOERROR qr,aa,rd ${String(100 + r.int(100))} 0.000${String(100 + r.int(800))}s`;
    }
    case "kube-proxy":
      return `I${klogTime(d)}       1 proxier.go:${String(800 + r.int(100))}] "SyncProxyRules complete" elapsed="${String(40 + r.int(60))}.${String(r.int(999))}ms"`;
    case "nginx": {
      const status = roll < 0.03 ? 502 : roll < 0.08 ? 404 : 200;
      const host = r.pick([
        "shop.demo.glowpod.dev",
        "shop.demo.glowpod.dev",
        "grafana.demo.glowpod.dev",
      ]);
      return `${ip(r)} - - [${nginxTime(d)}] "GET ${r.pick(PATHS)} HTTP/2.0" ${String(status)} ${String(200 + r.int(30000))} "-" "${r.pick(AGENTS)}" ${String(300 + r.int(300))} 0.0${String(10 + r.int(80))} [${host.startsWith("shop") ? "shop-storefront-80" : "monitoring-kube-prometheus-stack-grafana-80"}] [] ${ip(r)}:3000 ${String(200 + r.int(30000))} 0.0${String(10 + r.int(80))} ${String(status)} ${id(r, 32)}`;
    }
    case "glowpod":
      return `time=${ts} level=INFO msg=request method=GET path=/api/v1/clusters/default/overview status=200 duration=${String(12 + r.int(80))}ms`;
    case "job":
      return `time=${ts} level=INFO msg="reconciled batch" batch=${String(r.int(40))} entries=${String(400 + r.int(100))} mismatches=0`;
    case "klog":
      if (roll < 0.04)
        return `W${klogTime(d)}       1 reflector.go:561] "Watch close - *v1.Pod total ${String(100 + r.int(300))} items received"`;
      return `I${klogTime(d)}       1 controller.go:${String(100 + r.int(200))}] "Reconciled" duration="${String(1 + r.int(30))}.${String(r.int(999))}ms"`;
  }
}

/** What payments-api 1.8.0 prints before it exits. */
export function crashLines(at: number): string[] {
  const t = (ms: number) => new Date(at + ms).toISOString();
  return [
    `time=${t(0)} level=INFO msg="starting payments-api" version=1.8.0 commit=9c1e4fa`,
    `time=${t(12)} level=INFO msg="loading config" source=env`,
    `time=${t(31)} level=INFO msg="connected to database" host=payments-db.payments.svc.cluster.local`,
    `time=${t(33)} level=WARN msg="config key renamed in 1.8.0" old=LEDGER_URL new=PAYMENTS_LEDGER_URL`,
    `time=${t(34)} level=ERROR msg="config: required env PAYMENTS_LEDGER_URL is not set"`,
    `time=${t(35)} level=ERROR msg="exiting" code=1`,
  ];
}

/** The first lines a container writes when it starts. */
export function startLines(kind: LogKind, at: number): string[] {
  const ts = (ms: number) => new Date(at + ms).toISOString();
  switch (kind) {
    case "payments":
      return [
        `time=${ts(0)} level=INFO msg="starting payments-api" version=1.7.3 commit=4b2d81c`,
        `time=${ts(30)} level=INFO msg="connected to database" host=payments-db.payments.svc.cluster.local`,
        `time=${ts(41)} level=INFO msg="listening" addr=:8080`,
      ];
    case "job":
      return [
        `time=${ts(0)} level=INFO msg="ledger reconcile started" since=15m`,
        `time=${ts(900)} level=INFO msg="fetched provider settlements" count=${String(1800 + (at % 400))}`,
      ];
    case "web":
      return [
        JSON.stringify({
          level: "info",
          time: ts(0),
          msg: "starting storefront",
          version: "2.14.0",
          node: "v22.11.0",
        }),
        JSON.stringify({ level: "info", time: ts(420), msg: "ready", port: 3000 }),
      ];
    default:
      return [];
  }
}
