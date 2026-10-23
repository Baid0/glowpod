// Renders the guides in the repository's docs/ (and the SSO demo README) into site pages at
// docs/<slug>/index.html, so the site and the repository never disagree. The pages are generated:
// edit the Markdown, not the output.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, posix, resolve } from "node:path";
import { Marked, Renderer } from "marked";

const site = resolve(import.meta.dirname, "..");
const repo = resolve(site, "..");
const GITHUB = "https://github.com/glowpod-dev/glowpod/blob/main/";

// In reading order; the quick start (docs/index.html) comes first and is written by hand.
export const DOC_PAGES = [
  { slug: "terminal", source: "docs/terminal.md", nav: "Glowpod in the terminal" },
  { slug: "install", source: "docs/install.md", nav: "Install in a cluster" },
  { slug: "hub", source: "docs/hub.md", nav: "Glowpod Hub" },
  { slug: "authentication", source: "docs/authentication.md", nav: "Sign-in and access" },
  { slug: "sso-demo", source: "deploy/examples/sso-demo/README.md", nav: "Try SSO in ten minutes" },
  { slug: "configuration", source: "docs/configuration.md", nav: "Configuration" },
  { slug: "operations", source: "docs/operations.md", nav: "Running Glowpod" },
];

const QUICK_START = { slug: "", nav: "Quick start", url: "/docs/" };
const pageUrl = (page) => page.url ?? `/docs/${page.slug}/`;

export const docSources = () => DOC_PAGES.map((page) => join(repo, page.source));
export const docInputs = () =>
  Object.fromEntries(DOC_PAGES.map((page) => [`docs-${page.slug}`, docPath(page)]));
const docPath = (page) => join(site, "docs", page.slug, "index.html");

const escapeHtml = (text) =>
  text.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
const stripTags = (html) => html.replace(/<[^>]+>/g, "");
const decode = (text) =>
  text.replace(
    /&(amp|lt|gt|quot|#39);/g,
    (_, e) => ({ amp: "&", lt: "<", gt: ">", quot: '"', "#39": "'" })[e],
  );

// GitHub's heading anchors, so links like authentication.md#give-people-access keep working.
function slugger() {
  const seen = new Map();
  return (text) => {
    const base = decode(stripTags(text))
      .toLowerCase()
      .replace(/[^\p{L}\p{N}\s_-]/gu, "")
      .replace(/\s/g, "-");
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    return count ? `${base}-${count}` : base;
  };
}

// An image in a guide lives in site/public/, so the same relative path works on GitHub; the site
// serves it from its own root. Width and height come from the file, so the page doesn't jump.
function resolveImage(page, href) {
  const target = posix.normalize(posix.join(posix.dirname(page.source), href));
  if (!target.startsWith("site/public/")) {
    throw new Error(`${page.source}: images must live in site/public/, not ${target}`);
  }
  const { width, height } = imageSize(join(repo, target));
  return { src: "/" + target.slice("site/public/".length), width, height };
}

// The pixel size of a PNG or WebP (lossy, lossless or extended).
function imageSize(path) {
  const b = readFileSync(path);
  if (b.toString("ascii", 1, 4) === "PNG")
    return { width: b.readUInt32BE(16), height: b.readUInt32BE(20) };
  const chunk = b.toString("ascii", 12, 16);
  if (chunk === "VP8 ")
    return { width: b.readUInt16LE(26) & 0x3fff, height: b.readUInt16LE(28) & 0x3fff };
  if (chunk === "VP8L") {
    const bits = b.readUInt32LE(21);
    return { width: (bits & 0x3fff) + 1, height: ((bits >> 14) & 0x3fff) + 1 };
  }
  if (chunk === "VP8X") return { width: b.readUIntLE(24, 3) + 1, height: b.readUIntLE(27, 3) + 1 };
  throw new Error(`${path}: not a PNG or WebP`);
}

// A link in a guide: another guide becomes its site page, any other repository file its GitHub
// page, and outside links stay as they are.
function resolveLink(page, href) {
  if (/^[a-z]+:|^#/i.test(href)) return { href };
  const [path, hash = ""] = href.split("#");
  const target = posix.normalize(posix.join(posix.dirname(page.source), path));
  const doc = DOC_PAGES.find((p) => p.source === target);
  if (doc) return { href: pageUrl(doc) + (hash ? `#${hash}` : ""), doc };
  return { href: GITHUB + target + (hash ? `#${hash}` : "") };
}

const COPY_ICONS = readFileSync(join(site, "partials", "copy-icons.html"), "utf8").trim();
const LANGUAGES = { bash: "Terminal", powershell: "PowerShell", yaml: "YAML" };

function render(page, markdown) {
  const slug = slugger();
  const toc = [];
  let title = "";
  const marked = new Marked({ gfm: true });
  marked.use({
    renderer: {
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens);
        if (depth === 1) {
          title = decode(stripTags(html));
          return "";
        }
        const id = slug(html);
        if (depth === 2) toc.push({ id, text: decode(stripTags(html)) });
        // Numbered steps ("1. Get the image") get the same badge as the quick start.
        const step = depth === 2 && html.match(/^(\d+)\.\s+(.*)$/s);
        if (step) {
          return `<h2 class="step" id="${id}"><span class="step-number" aria-hidden="true">${step[1]}</span>${step[2]}</h2>\n`;
        }
        return `<h${depth} id="${id}">${html}</h${depth}>\n`;
      },
      code({ text, lang }) {
        const code = `<pre class="code-body" tabindex="0"><code>${escapeHtml(text)}</code></pre>`;
        if (!LANGUAGES[lang]) return `<div class="card code-block">${code}</div>\n`;
        return `<div class="card code-block terminal">
  <div class="terminal-bar"><span>${LANGUAGES[lang]}</span><button class="button button-small copy-button" type="button" data-copy="${escapeHtml(text)}" aria-label="Copy this ${lang === "yaml" ? "YAML" : "command"}">${COPY_ICONS} Copy</button></div>
  ${code}
</div>\n`;
      },
      image({ href, text }) {
        const img = resolveImage(page, href);
        return `<img class="docs-image" src="${escapeHtml(img.src)}" alt="${escapeHtml(text)}" width="${img.width}" height="${img.height}" loading="lazy" decoding="async" />`;
      },
      codespan({ text }) {
        return `<code class="inline-code">${escapeHtml(text)}</code>`;
      },
      link({ href, tokens }) {
        const link = resolveLink(page, href);
        let text = this.parser.parseInline(tokens);
        // "[authentication.md](authentication.md)" reads better as the page's name.
        if (link.doc && /\.md$/.test(stripTags(text))) text = link.doc.nav;
        return `<a href="${escapeHtml(link.href)}">${text}</a>`;
      },
      table(token) {
        const table = Renderer.prototype.table
          .call(this, token)
          .replace("<table>", '<table class="docs-table">');
        return `<div class="table-wrap" role="region" aria-label="Table" tabindex="0">${table}</div>\n`;
      },
    },
  });
  const content = marked.parse(markdown);
  const lead = content.match(/<p>([\s\S]*?)<\/p>/)?.[1] ?? "";
  const firstParagraph = decode(stripTags(lead)).replace(/\s+/g, " ");
  return {
    title,
    content,
    toc,
    description: firstParagraph.slice(0, 157).replace(/\s\S*$/, "") + "…",
  };
}

function pagerLink(page, label, className) {
  if (!page) return "";
  return `<a class="pager-link ${className}" href="${pageUrl(page)}">
              <span class="pager-label">${label}</span>
              <span class="pager-title">${page.nav}</span>
            </a>`;
}

export function writeDocPages() {
  const template = readFileSync(join(site, "src", "doc-page.html"), "utf8");
  const order = [QUICK_START, ...DOC_PAGES];
  for (const page of DOC_PAGES) {
    const { title, content, toc, description } = render(
      page,
      readFileSync(join(repo, page.source), "utf8"),
    );
    const i = order.indexOf(page);
    const html = template
      .replaceAll("{{slug}}", page.slug)
      .replaceAll("{{title}}", escapeHtml(title))
      .replaceAll("{{description}}", escapeHtml(description))
      .replaceAll("{{source}}", GITHUB + page.source)
      .replace(
        "{{toc}}",
        toc.map((t) => `<a href="#${t.id}">${escapeHtml(t.text)}</a>`).join("\n          "),
      )
      .replace(
        "{{pager}}",
        pagerLink(order[i - 1], "Previous", "") + pagerLink(order[i + 1], "Next", "pager-next"),
      )
      .replace("{{content}}", content);
    mkdirSync(dirname(docPath(page)), { recursive: true });
    writeFileSync(docPath(page), html);
  }
}
