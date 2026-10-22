/** Keeps the post-sign-in redirect on this site. */
export function safeReturn(path: string | null): string {
  if (!path?.startsWith("/") || path.startsWith("//") || path.startsWith("/sign-in")) return "/";
  return path;
}
