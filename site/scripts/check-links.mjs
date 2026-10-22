// Fails the build if a link to a page on this site, or to an #anchor on it, leads nowhere. The
// guides come from Markdown, so a renamed heading or file would otherwise break links silently.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

const dist = join(import.meta.dirname, "..", "dist");

function* pages(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) yield* pages(path);
    else if (entry.name.endsWith(".html")) yield path;
  }
}

const ids = (html) => new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]));
const urlOf = (file) => "/" + relative(dist, file).replace(/index\.html$/, "");

const byUrl = new Map([...pages(dist)].map((file) => [urlOf(file), readFileSync(file, "utf8")]));
let broken = 0;
for (const [url, html] of byUrl) {
  for (const [, href] of html.matchAll(/\shref="([^"]+)"/g)) {
    if (/^[a-z]+:/i.test(href)) continue;
    const [path, hash] = href.split("#");
    const target = path === "" ? url : path;
    const page = byUrl.get(target) ?? (existsSync(join(dist, target)) ? "" : undefined);
    if (page === undefined) {
      console.error(`check-links: ${url} links to ${href}, which doesn't exist`);
      broken++;
    } else if (hash && !ids(page).has(hash)) {
      console.error(`check-links: ${url} links to ${href}, which has no #${hash}`);
      broken++;
    }
  }
}
if (broken) process.exit(1);
console.log("check-links: every link on the site leads somewhere");
