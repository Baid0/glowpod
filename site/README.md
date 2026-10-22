# glowpod.dev

The Glowpod website: the home page, the quick start, the guides and the demo. Plain HTML and CSS
built with Vite, separate from the app in `frontend/`.

```bash
make site-dev      # dev server on http://localhost:5180 (no demo)
make site          # demo build, format check, build into site/dist, check the CSP and links
make site-preview  # serve site/dist like Cloudflare Pages on http://localhost:5181, demo included
```

- **Pages:** `index.html`, `docs/index.html` (the quick start), `404.html`.
  Shared pieces (head, header, footer, docs sidebar, the logo mark, icons) live in `partials/`
  and are pulled in with `<!-- @include header.html -->`; `current=docs` marks the current nav
  link.
- **Guides:** `scripts/docs.mjs` renders the Markdown in the repository's `docs/` (and the SSO
  demo README) into `docs/<slug>/index.html` with `src/doc-page.html`, on every build and on
  every save in `make site-dev`. Those pages are generated and not committed: edit the Markdown.
  Links between guides become site links, links to other repository files go to GitHub. To add a
  guide, add it to `DOC_PAGES` and to `partials/docs-nav.html`.
- **Styles:** `src/styles/`, all pulled together by `site.css`, which every page links from
  `<head>` so nothing paints unstyled. The same tokens as the app, dark by default, light with
  `<html data-theme="light">`. The visitor's choice is saved under `glowpod.theme`.
- **Script:** `src/main.js` (theme toggle, menus, copy buttons, install tabs, "On this page").
  `public/theme.js` applies the saved theme before the page paints.
- **Checks:** `scripts/check-links.mjs` fails the build when a link to a page or `#anchor` on
  the site leads nowhere, for example after a heading in a guide is renamed.
- **CSP:** every built page carries
  `default-src 'self'; script-src 'self'; style-src 'self'; …` in a meta tag, and
  `scripts/check-csp.mjs` fails the build on inline scripts, styles, event handlers or `data:`
  URLs. The build also writes `dist/_headers` from the same policy, so Cloudflare Pages sends it
  as a header, plus `frame-ancestors 'none'` (which a meta tag can't set), HSTS, `nosniff` and a
  year-long cache for the hashed files in `/assets/`.
- **Demo:** `/demo/` is the real app from `frontend/`, built with `pnpm build:demo` (Vite's
  `demo` mode). Its entry, `frontend/src/demo/main.tsx`, answers the app's API calls and
  WebSockets from an in-memory sample cluster (`frontend/src/demo/`), so it needs no server and
  nothing leaves the browser; the release build never includes it. `make site` copies the build
  into `dist/demo/`. To work on it with hot reload, run
  `pnpm --dir frontend exec vite --mode demo --port 5174` and open http://localhost:5174/demo/.
- **Demo CSP:** the app's YAML editor and terminal create `<style>` elements, which need a nonce.
  `public/_worker.js` serves the demo's pages (only those: see `public/_routes.json`) with a
  fresh nonce per response, the way the Glowpod server does, so the demo keeps
  `script-src 'self'` and a `style-src` without `'unsafe-inline'`; its policy also sets
  `connect-src 'none'`. `make site-preview` (`scripts/serve.mjs`) runs that worker locally.
- **Deploy:** `.github/workflows/site.yml` builds the site and deploys it to the Cloudflare Pages
  project `glowpod`: pushes to `main` go live, pull requests get a preview URL. It needs the
  repository secrets `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`; without them it only
  builds.
- **Brand files:** the logo mark, the chart icon and Pip, in `public/brand/` (used by the pages and the
  Helm chart; there is no brand page). `/brand/` redirects to the home page (`public/_redirects`).
