import { cpSync, existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineConfig } from "vite";
import { docInputs, docSources, writeDocPages } from "./scripts/docs.mjs";

const root = import.meta.dirname;

// Cloudflare Web Analytics, which Cloudflare adds to every page it serves: its beacon script (a
// module with an integrity hash) and the one address it reports to. Paths, not whole hosts.
const ANALYTICS_SCRIPT = "https://static.cloudflareinsights.com/beacon.min.js/";
const ANALYTICS_REPORT = "https://cloudflareinsights.com/cdn-cgi/rum";

// The page's Content-Security-Policy. Only in the build: the dev server injects styles inline.
const CSP = [
  "default-src 'self'",
  `script-src 'self' ${ANALYTICS_SCRIPT}`,
  "style-src 'self'",
  "img-src 'self'",
  "font-src 'self'",
  `connect-src 'self' ${ANALYTICS_REPORT}`,
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'none'",
].join("; ");

// Headers Cloudflare Pages sends with every response, written to dist/_headers. The CSP is the
// meta tag's policy plus frame-ancestors, which only works as a header. Hashed files in
// /assets/ never change, so browsers may keep them for a year. The demo's pages get their own
// policy from public/_worker.js.
const HEADERS = `/*
  Content-Security-Policy: ${CSP}; frame-ancestors 'none'
  Strict-Transport-Security: max-age=63072000; includeSubDomains
  X-Content-Type-Options: nosniff
  X-Frame-Options: DENY
  Referrer-Policy: strict-origin-when-cross-origin
  Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()
  Cross-Origin-Opener-Policy: same-origin

/assets/*
  Cache-Control: public, max-age=31536000, immutable

/demo/assets/*
  Cache-Control: public, max-age=31536000, immutable
`;

// Replaces <!-- @include header.html current=docs --> with partials/header.html, marking the
// nav link whose data-nav matches `current` as the current page.
// Partials can include other partials.
function expand(html) {
  return html.replace(/<!-- @include (\S+?)(?: current=(\S+))? -->/g, (_, file, current) => {
    let part = expand(readFileSync(resolve(root, "partials", file), "utf8").trim());
    if (current) {
      part = part.replaceAll(`data-nav="${current}"`, `data-nav="${current}" aria-current="page"`);
    }
    return part;
  });
}

function partials() {
  return {
    name: "glowpod-partials",
    transformIndexHtml: { order: "pre", handler: expand },
    handleHotUpdate({ file, server }) {
      if (file.startsWith(resolve(root, "partials"))) server.ws.send({ type: "full-reload" });
    },
  };
}

// Generates the guide pages from the repository's Markdown, and again whenever it changes.
function docs() {
  writeDocPages();
  return {
    name: "glowpod-docs",
    configureServer(server) {
      server.watcher.add([...docSources(), resolve(root, "src/doc-page.html")]);
      server.watcher.on("change", (file) => {
        if (docSources().includes(file) || file.endsWith("doc-page.html")) {
          writeDocPages();
          server.ws.send({ type: "full-reload" });
        }
      });
    },
  };
}

function csp() {
  return {
    name: "glowpod-csp",
    apply: "build",
    transformIndexHtml: (html) =>
      html.replace(
        '<meta charset="utf-8" />',
        `<meta charset="utf-8" />\n<meta http-equiv="Content-Security-Policy" content="${CSP}" />`,
      ),
  };
}

function headers() {
  return {
    name: "glowpod-headers",
    apply: "build",
    generateBundle() {
      this.emitFile({ type: "asset", fileName: "_headers", source: HEADERS });
    },
  };
}

// The app demo at /demo/: the frontend's demo build (`pnpm build:demo` in frontend/, which
// `make site` runs first), copied in after the site is built.
function demo() {
  const from = resolve(root, "../frontend/dist-demo");
  return {
    name: "glowpod-demo",
    apply: "build",
    closeBundle() {
      if (!existsSync(resolve(from, "index.html"))) {
        throw new Error(
          "The demo isn't built: run `make site`, or `pnpm build:demo` in frontend/.",
        );
      }
      cpSync(from, resolve(root, "dist/demo"), { recursive: true });
    },
  };
}

export default defineConfig({
  plugins: [docs(), partials(), csp(), headers(), demo()],
  server: { port: 5180, strictPort: true },
  build: {
    // Fonts and images stay files: the CSP allows no data: URLs.
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
    rollupOptions: {
      input: {
        home: resolve(root, "index.html"),
        docs: resolve(root, "docs/index.html"),
        ...docInputs(),
        notFound: resolve(root, "404.html"),
      },
    },
  },
});
