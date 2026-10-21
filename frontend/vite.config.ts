import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The Go server (`glowpod --dev`) serves the API on this address during `make dev`.
const backend = "http://127.0.0.1:7171";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: backend, ws: true },
      "/healthz": backend,
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Emit every asset as a file. Inlined data: URIs (e.g. small font subsets)
    // would be blocked by the server's CSP (font-src 'self').
    assetsInlineLimit: 0,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
    unstubGlobals: true,
  },
});
