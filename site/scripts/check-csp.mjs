// Fails the build if a page would break under the strict CSP: inline scripts, inline styles,
// inline event handlers, javascript: URLs or data: URLs.
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

const dist = join(import.meta.dirname, "..", "dist");
const rules = [
  [/<script(?![^>]*\bsrc=)[^>]*>/i, "inline <script>"],
  [/<style[\s>]/i, "inline <style>"],
  [/\sstyle=/i, "style attribute"],
  [/\son[a-z]+=/i, "inline event handler"],
  [/javascript:/i, "javascript: URL"],
  [/(?:src|href)="data:/i, "data: URL"],
];

function* files(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) yield* files(path);
    else if (/\.(html|css)$/.test(entry.name)) yield path;
  }
}

let failed = false;
for (const file of files(dist)) {
  // Comments never run, and the demo's page mentions <style> in one.
  const text = readFileSync(file, "utf8").replace(/<!--[\s\S]*?-->/g, "");
  const checks = file.endsWith(".css") ? [[/url\(\s*["']?data:/i, "data: URL"]] : rules;
  for (const [pattern, what] of checks) {
    if (pattern.test(text)) {
      console.error(`check-csp: ${relative(dist, file)} has an ${what}`);
      failed = true;
    }
  }
}
if (failed) process.exit(1);
console.log("check-csp: every page works under the strict CSP");
