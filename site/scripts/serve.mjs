// Serves dist/ on http://localhost:5181 the way Cloudflare Pages does, closely enough to try pages
// under their real headers: the rules in _headers, the redirects in _redirects, /dir/index.html at
// /dir/, 404.html, and _worker.js for the paths _routes.json sends to it (the demo).
import { existsSync, readFileSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { extname, join, normalize } from "node:path";
import { pathToFileURL } from "node:url";

const dist = join(import.meta.dirname, "..", "dist");
const port = Number(process.env.PORT ?? 5181);

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".yaml": "text/yaml; charset=utf-8",
  ".yml": "text/yaml; charset=utf-8",
  ".txt": "text/plain; charset=utf-8",
};

const read = (name) => (existsSync(join(dist, name)) ? readFileSync(join(dist, name), "utf8") : "");

// "/demo/*" → a regular expression; * matches anything.
const pattern = (p) =>
  new RegExp("^" + p.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*") + "$");

const headerRules = [];
for (const line of read("_headers").split("\n")) {
  if (!line.trim() || line.trim().startsWith("#")) continue;
  if (!/^\s/.test(line)) headerRules.push({ match: pattern(line.trim()), headers: [] });
  else {
    const i = line.indexOf(":");
    headerRules.at(-1)?.headers.push([line.slice(0, i).trim(), line.slice(i + 1).trim()]);
  }
}

const redirects = read("_redirects")
  .split("\n")
  .filter((l) => l.trim() && !l.trim().startsWith("#"))
  .map((l) => l.trim().split(/\s+/))
  .map(([from, to, status]) => ({ from, to, status: Number(status ?? 302) }));

const routes = JSON.parse(read("_routes.json") || '{"include":[],"exclude":[]}');
const toWorker = (path) =>
  routes.include.some((p) => pattern(p).test(path)) &&
  !routes.exclude.some((p) => pattern(p).test(path));
const worker = existsSync(join(dist, "_worker.js"))
  ? (await import(pathToFileURL(join(dist, "_worker.js")).href)).default
  : null;

function file(path) {
  const full = normalize(join(dist, decodeURIComponent(path)));
  if (!full.startsWith(dist)) return null;
  return existsSync(full) && statSync(full).isFile() ? full : null;
}

/** A static file, without the _headers rules (Pages adds those to static responses only). */
function asset(url) {
  const { pathname } = url;
  if (pathname.endsWith("/index.html")) {
    return new Response(null, { status: 308, headers: { Location: pathname.slice(0, -10) } });
  }
  const direct = pathname.endsWith("/") ? file(pathname + "index.html") : file(pathname);
  if (direct) {
    return new Response(readFileSync(direct), {
      headers: { "Content-Type": TYPES[extname(direct)] ?? "application/octet-stream" },
    });
  }
  if (!pathname.endsWith("/") && file(pathname + "/index.html")) {
    return new Response(null, { status: 308, headers: { Location: pathname + "/" } });
  }
  const notFound = file("/404.html");
  return new Response(notFound ? readFileSync(notFound) : "Not found", {
    status: 404,
    headers: { "Content-Type": TYPES[".html"] },
  });
}

async function respond(request) {
  const url = new URL(request.url);
  const redirect = redirects.find((r) => r.from === url.pathname);
  if (redirect)
    return new Response(null, { status: redirect.status, headers: { Location: redirect.to } });
  if (worker && toWorker(url.pathname)) {
    return worker.fetch(request, {
      ASSETS: {
        fetch: async (input) => asset(new URL(typeof input === "string" ? input : input.url)),
      },
    });
  }
  const res = asset(url);
  const headers = new Headers(res.headers);
  for (const rule of headerRules) {
    if (rule.match.test(url.pathname)) for (const [k, v] of rule.headers) headers.set(k, v);
  }
  return new Response(res.body, { status: res.status, headers });
}

createServer(async (req, res) => {
  const response = await respond(
    new Request(`http://localhost:${port}${req.url}`, { method: req.method }),
  );
  res.writeHead(response.status, Object.fromEntries(response.headers));
  res.end(req.method === "HEAD" ? undefined : Buffer.from(await response.arrayBuffer()));
}).listen(port, () => {
  console.log(`Serving dist/ like Cloudflare Pages on http://localhost:${port}`);
});
