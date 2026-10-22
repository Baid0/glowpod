/** The pod page, optionally with a query such as "?tab=logs". */
export function podPath(p: { namespace: string; name: string }, query = "") {
  return `/workloads/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}${query}`;
}
