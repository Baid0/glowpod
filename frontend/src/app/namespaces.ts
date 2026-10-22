/** Names containing the query, those starting with it first. */
export function filterNamespaces(names: string[], query: string): string[] {
  const q = query.trim().toLowerCase();
  if (!q) return names;
  const hits = names.filter((n) => n.includes(q));
  return [...hits.filter((n) => n.startsWith(q)), ...hits.filter((n) => !n.startsWith(q))];
}
