import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { configDefaults, defineConfig } from "vitest/config";

// The Go server (`glowpod --dev`) serves the API on this address during `make dev`.
const backend = "http://127.0.0.1:7171";

// The website demo (glowpod.dev/demo/) is the same app with src/demo/main.tsx as its entry,
// which answers the API from an in-memory cluster. `vite build --mode demo` builds it into
// dist-demo/; the release build never includes it.
const DEMO_BASE = "/demo/";

function demoEntry(): Plugin {
  return {
    name: "glowpod-demo-entry",
    transformIndexHtml: {
      order: "pre",
      handler: (html, ctx) => {
        const page = html
          .replace('src="/src/main.tsx"', 'src="/src/demo/main.tsx"')
          .replace("<title>Glowpod</title>", "<title>Glowpod demo</title>");
        // The dev server adds the base to every URL itself; the build leaves classic scripts alone.
        return ctx.server ? page : page.replace('src="/theme.js"', `src="${DEMO_BASE}theme.js"`);
      },
    },
  };
}

export default defineConfig(({ mode }) => {
  const demo = mode === "demo";
  return {
    plugins: [react(), ...(demo ? [demoEntry()] : [])],
    base: demo ? DEMO_BASE : "/",
    server: {
      port: 5173,
      strictPort: true,
      proxy: {
        "/api": { target: backend, ws: true },
        "/healthz": backend,
      },
    },
    build: {
      outDir: demo ? "dist-demo" : "dist",
      emptyOutDir: true,
      // Emit every asset as a file. Inlined data: URIs (e.g. small font subsets)
      // would be blocked by the server's CSP (font-src 'self').
      assetsInlineLimit: 0,
    },
    test: {
      environment: "jsdom",
      setupFiles: ["./src/test/setup.ts"],
      // e2e/ holds Playwright specs; `make e2e` runs those.
      exclude: [...configDefaults.exclude, "e2e/**"],
      restoreMocks: true,
      unstubGlobals: true,
    },
  };
});
