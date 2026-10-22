import { cspNonce } from "../../lib/csp";

type CreateElement = (tag: string, options?: ElementCreationOptions) => HTMLElement;

/**
 * Runs fn with every <style> element it creates tagged with the page's CSP
 * nonce. For libraries (xterm.js) that create styles and take no nonce.
 */
export function withStyleNonce<T>(fn: () => T): T {
  const nonce = cspNonce();
  if (!nonce) return fn();
  const doc = document as unknown as { createElement: CreateElement };
  const original = doc.createElement;
  doc.createElement = (tag, options) => {
    const el = original.call(document, tag, options);
    if (tag.toLowerCase() === "style") el.setAttribute("nonce", nonce);
    return el;
  };
  try {
    return fn();
  } finally {
    doc.createElement = original;
  }
}
