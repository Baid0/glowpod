# glowpod.dev

The Glowpod website: the home page, the quick start and the brand guidelines. Plain HTML and CSS
built with Vite, separate from the app in `frontend/`.

```bash
make site-dev   # dev server on http://localhost:5180
make site       # format check, build into site/dist, check the CSP
```

- **Pages:** `index.html`, `docs/index.html`, `brand/index.html`, `404.html`. Shared pieces
  (head, header, footer, the logo mark, icons) live in `partials/` and are pulled in with
  `<!-- @include header.html -->`; `current=docs` marks the current nav link.
- **Styles:** `src/styles/` — the same tokens as the app, dark by default, light with
  `<html data-theme="light">`. The visitor's choice is saved under `glowpod.theme`.
- **Script:** `src/main.js` (theme toggle, menus, copy buttons, install tabs, "On this page").
  `public/theme.js` applies the saved theme before the page paints.
- **CSP:** every built page carries
  `default-src 'self'; script-src 'self'; style-src 'self'; …` in a meta tag, and
  `scripts/check-csp.mjs` fails the build on inline scripts, styles, event handlers or `data:`
  URLs. Send the same policy as a header from the host, plus `frame-ancestors 'none'`, which a
  meta tag can't set.
- **Brand files:** the logo mark and Pip are SVGs in `public/brand/`, offered for download on the
  brand page.

The longer guides stay in `docs/` in the repository; the site links to them on GitHub.
