// Cloudflare Pages runs this for /demo/ only (see _routes.json); the rest of the site is static
// files with the headers in _headers. The demo is the Glowpod app, whose YAML editor and terminal
// create <style> elements. Like the Glowpod server, this gives every page load a fresh nonce for
// them, so the demo keeps script-src 'self' and a style-src without 'unsafe-inline'.

const PLACEHOLDER = "__GLOWPOD_NONCE__";

// The Glowpod server's policy (backend/internal/web/csp.go), plus Cloudflare Web Analytics, which
// Cloudflare adds to every page: its beacon script and the one address it reports to (the same
// two paths as in vite.config.js). connect-src allows nothing else: the demo answers its API
// inside the page.
const ANALYTICS_SCRIPT = "https://static.cloudflareinsights.com/beacon.min.js/";
const ANALYTICS_REPORT = "https://cloudflareinsights.com/cdn-cgi/rum";

function policy(nonce) {
  return [
    "default-src 'self'",
    `script-src 'self' ${ANALYTICS_SCRIPT}`,
    `style-src 'self' 'nonce-${nonce}'`,
    "img-src 'self' data:",
    "font-src 'self'",
    `connect-src ${ANALYTICS_REPORT}`,
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
}

// The rest of what _headers sends with every page.
const HEADERS = {
  "Strict-Transport-Security": "max-age=63072000; includeSubDomains",
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
  "Cross-Origin-Opener-Policy": "same-origin",
};

function newNonce() {
  const bytes = crypto.getRandomValues(new Uint8Array(18));
  return btoa(String.fromCharCode(...bytes));
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/demo") {
      return Response.redirect(`${url.origin}/demo/${url.search}`, 301);
    }
    // Files keep their own paths; every other path under /demo/ is a page of the app.
    if (!url.pathname.startsWith("/demo/") || /\.[a-z0-9]+$/i.test(url.pathname)) {
      return env.ASSETS.fetch(request);
    }
    const page = await env.ASSETS.fetch(new URL("/demo/", url).toString());
    if (!page.ok) return page;
    const nonce = newNonce();
    const html = (await page.text()).replaceAll(PLACEHOLDER, nonce);
    return new Response(html, {
      headers: {
        "Content-Type": "text/html; charset=utf-8",
        // The nonce is good for one response only.
        "Cache-Control": "no-store",
        "Content-Security-Policy": policy(nonce),
        ...HEADERS,
      },
    });
  },
};
