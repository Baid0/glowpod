// All TanStack Query keys live here so invalidation stays consistent.
export const queryKeys = {
  session: ["session"] as const,
  namespaces: (cluster: string) => ["namespaces", cluster] as const,
  overview: (cluster: string, namespace: string) => ["overview", cluster, namespace] as const,
  overviewAll: (cluster: string) => ["overview", cluster] as const,
  events: (cluster: string, namespace: string) => ["events", cluster, namespace] as const,
};
