// All TanStack Query keys live here so invalidation stays consistent.
export const queryKeys = {
  session: ["session"] as const,
  namespaces: (cluster: string) => ["namespaces", cluster] as const,
  overview: (cluster: string, namespace: string) => ["overview", cluster, namespace] as const,
  overviewAll: (cluster: string) => ["overview", cluster] as const,
  events: (cluster: string, namespace: string) => ["events", cluster, namespace] as const,
  resourceTypes: (cluster: string) => ["resourceTypes", cluster] as const,
  pod: (cluster: string, namespace: string, name: string) =>
    ["pod", cluster, namespace, name] as const,
  resourceList: (
    cluster: string,
    group: string,
    version: string,
    resource: string,
    namespace: string,
    labelSelector: string,
  ) => ["resourceList", cluster, group, version, resource, namespace, labelSelector] as const,
};
