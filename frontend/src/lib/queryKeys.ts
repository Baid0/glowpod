// All TanStack Query keys live here so invalidation stays consistent.
export const queryKeys = {
  session: ["session"] as const,
  clusters: ["clusters"] as const,
  kubeconfigTokens: ["kubeconfigTokens"] as const,
  hubUsers: ["hubUsers"] as const,
  hubSettings: ["hubSettings"] as const,
  clusterSummary: (cluster: string, namespace: string) =>
    ["clusterSummary", cluster, namespace] as const,
  search: (cluster: string, query: string, namespace: string) =>
    ["search", cluster, query, namespace] as const,
  namespaces: (cluster: string) => ["namespaces", cluster] as const,
  members: (cluster: string) => ["members", cluster] as const,
  projects: (cluster: string) => ["projects", cluster] as const,
  overview: (cluster: string, namespace: string) => ["overview", cluster, namespace] as const,
  overviewAll: (cluster: string) => ["overview", cluster] as const,
  events: (cluster: string, namespace: string) => ["events", cluster, namespace] as const,
  objectEvents: (cluster: string, namespace: string, kind: string, name: string) =>
    ["events", cluster, namespace, "object", kind, name] as const,
  /** The Events page's longer list; under events() so resyncs refresh both. */
  eventsPage: (cluster: string, namespace: string) =>
    ["events", cluster, namespace, "page"] as const,
  resourceTypes: (cluster: string) => ["resourceTypes", cluster] as const,
  apps: (cluster: string, namespace: string) => ["apps", cluster, namespace] as const,
  appsAll: (cluster: string) => ["apps", cluster] as const,
  app: (cluster: string, namespace: string, name: string) =>
    ["app", cluster, namespace, name] as const,
  appAll: (cluster: string) => ["app", cluster] as const,
  pod: (cluster: string, namespace: string, name: string) =>
    ["pod", cluster, namespace, name] as const,
  object: (
    cluster: string,
    group: string,
    version: string,
    resource: string,
    namespace: string,
    name: string,
    reveal: boolean,
  ) => ["object", cluster, group, version, resource, namespace, name, reveal] as const,
  helmReleases: (cluster: string, namespace: string) => ["helm", cluster, namespace] as const,
  helmRelease: (cluster: string, namespace: string, name: string) =>
    ["helm", cluster, namespace, name] as const,
  resourceMap: (cluster: string, namespace: string) => ["map", cluster, namespace] as const,
  node: (cluster: string, name: string) => ["node", cluster, name] as const,
  drainPlan: (cluster: string, name: string) => ["drainPlan", cluster, name] as const,
  /** Permission answers; cleared when anything is changed. */
  access: (cluster: string, checks: string) => ["access", cluster, checks] as const,
  accessAll: (cluster: string) => ["access", cluster] as const,
  resourceList: (
    cluster: string,
    group: string,
    version: string,
    resource: string,
    namespace: string,
    labelSelector: string,
  ) => ["resourceList", cluster, group, version, resource, namespace, labelSelector] as const,
};
