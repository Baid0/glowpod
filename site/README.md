# glowpod.dev

The Glowpod website: the home page, the quick start and the brand guidelines. Plain HTML and CSS
built with Vite, separate from the app in `frontend/`.

```bash
make site-dev   # dev server on http://localhost:5180
make site       # format check, build into site/dist, check the CSP
```

- **Pages:** `index.html`, `docs/index.html` (the quick start), `brand/index.html`, `404.html`.
  Shared pieces (head, header, footer, docs sidebar, the logo mark, icons) live in `partials/`
  and are pulled in with `<!-- @include header.html -->`; `current=docs` marks the current nav
  link.
- **Guides:** `scripts/docs.mjs` renders the Markdown in the repository's `docs/` (and the SSO
  demo README) into `docs/<slug>/index.html` with `src/doc-page.html`, on every build and on
  every save in `make site-dev`. Those pages are generated and not committed: edit the Markdown.
  Links between guides become site links, links to other repository files go to GitHub. To add a
  guide, add it to `DOC_PAGES` and to `partials/docs-nav.html`.
- **Styles:** `src/styles/` — the same tokens as the app, dark by default, light with
  `<html data-theme="light">`. The visitor's choice is saved under `glowpod.theme`.
- **Script:** `src/main.js` (theme toggle, menus, copy buttons, install tabs, "On this page").
  `public/theme.js` applies the saved theme before the page paints.
- **Checks:** `scripts/check-links.mjs` fails the build when a link to a page or `#anchor` on
  the site leads nowhere, for example after a heading in a guide is renamed.
- **CSP:** every built page carries
  `default-src 'self'; script-src 'self'; style-src 'self'; …` in a meta tag, and
  `scripts/check-csp.mjs` fails the build on inline scripts, styles, event handlers or `data:`
  URLs. Send the same policy as a header from the host, plus `frame-ancestors 'none'`, which a
  meta tag can't set.
- **Brand files:** the logo mark and Pip are SVGs in `public/brand/`, offered for download on the
  brand page.
