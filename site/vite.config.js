import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineConfig } from "vite";
import { docInputs, docSources, writeDocPages } from "./scripts/docs.mjs";

const root = import.meta.dirname;

// The page's Content-Security-Policy. Only in the build: the dev server injects styles inline.
const CSP = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self'",
  "img-src 'self'",
  "font-src 'self'",
  "connect-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'none'",
].join("; ");

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

export default defineConfig({
  plugins: [docs(), partials(), csp()],
  server: { port: 5180, strictPort: true },
  preview: { port: 5181, strictPort: true },
  build: {
    // Fonts and images stay files: the CSP allows no data: URLs.
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
    rollupOptions: {
      input: {
        home: resolve(root, "index.html"),
        docs: resolve(root, "docs/index.html"),
        brand: resolve(root, "brand/index.html"),
        ...docInputs(),
        notFound: resolve(root, "404.html"),
      },
    },
  },
});
