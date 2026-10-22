const PLACEHOLDER = "__GLOWPOD_NONCE__";

/**
 * This page load's CSP nonce, for libraries that create <style> elements
 * (the YAML editor, the terminal). Empty in `vite dev`, which sends no CSP.
 */
export function cspNonce(): string {
  const content = document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content ?? "";
  return content === PLACEHOLDER ? "" : content;
}
