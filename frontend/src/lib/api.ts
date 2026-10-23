import type {
  AccessCheck,
  ApplyResult,
  AuthMethod,
  AppDetail,
  AppSummary,
  ClusterInfo,
  ClusterSummary,
  ImportCommand,
  KubeTokenList,
  CheckResult,
  HubSettings,
  HubUser,
  RestoreResult,
  LDAPSettings,
  OIDCSettings,
  ClusterRoleChoice,
  Members,
  ProjectRole,
  Projects,
  Subject,
  CreateResult,
  DrainPlan,
  DrainResult,
  HelmRelease,
  HelmReleaseDetail,
  KubeEvent,
  NamespaceList,
  NodeDetail,
  Overview,
  PodDetail,
  ResourceObject,
  ResourceRef,
  ResourceTable,
  ResourceMap,
  ResourceType,
  Session,
  UpdateResult,
} from "./types";

/** The id of the cluster an in-cluster Glowpod runs in. */
export const DEFAULT_CLUSTER = "default";

/** An API error with the server's code and plain-language message. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** The cluster's own explanation (validation, policy), when it gave one. */
    readonly detail?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

interface ErrorBody {
  error?: { code?: string; message?: string; detail?: string };
}

async function toApiError(res: Response): Promise<ApiError> {
  let body: ErrorBody = {};
  try {
    body = (await res.json()) as ErrorBody;
  } catch {
    // not JSON; fall through to a generic message
  }
  return new ApiError(
    res.status,
    body.error?.code ?? "http_" + String(res.status),
    body.error?.message ?? "Something went wrong.",
    body.error?.detail,
  );
}

const unreachable = () =>
  new ApiError(0, "server_unreachable", "Can't reach the Glowpod server. Is it running?");

// The session's CSRF token; every write sends it back in a header.
let csrfToken = "";

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { signal, headers: { Accept: "application/json" } });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw unreachable();
  }
  if (!res.ok) throw await toApiError(res);
  return (await res.json()) as T;
}

/**
 * A write (POST, PUT, DELETE). Carries the CSRF token, and fetches it again
 * once if the server no longer knows it (it restarted).
 */
export async function apiSend<T = void>(
  method: "POST" | "PUT" | "DELETE",
  path: string,
  body?: unknown,
): Promise<T> {
  const res = await apiSendRaw(method, path, body);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

/** apiSend's request, for an answer that isn't JSON (a file). */
async function apiSendRaw(
  method: "POST" | "PUT" | "DELETE",
  path: string,
  body?: unknown,
): Promise<Response> {
  const send = async () => {
    if (!csrfToken) await api.session();
    try {
      return await fetch(path, {
        method,
        headers: {
          Accept: "application/json",
          "X-CSRF-Token": csrfToken,
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
    } catch {
      throw unreachable();
    }
  };
  let res = await send();
  if (res.status === 403) {
    const err = await toApiError(res);
    if (err.code !== "csrf") throw err;
    csrfToken = "";
    res = await send();
  }
  if (!res.ok) throw await toApiError(res);
  return res;
}

function clusterPath(cluster: string, rest: string, params: Record<string, string | undefined>) {
  const query = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) query.set(k, v);
  const qs = query.toString();
  return `/api/v1/clusters/${encodeURIComponent(cluster)}/${rest}${qs ? "?" + qs : ""}`;
}

/** A sign-in POST: no session yet, so no CSRF token; the server checks the origin. */
async function signIn(path: string, body: unknown): Promise<void> {
  let res: Response;
  try {
    res = await fetch(path, {
      method: "POST",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    throw unreachable();
  }
  if (!res.ok) throw await toApiError(res);
  csrfToken = "";
}

export const api = {
  session: (signal?: AbortSignal) =>
    apiGet<Session>("/api/v1/session", signal).then((s) => {
      csrfToken = s.csrfToken;
      return s;
    }),
  clusters: (signal?: AbortSignal) =>
    apiGet<{ clusters: ClusterInfo[] }>("/api/v1/clusters", signal).then((r) => r.clusters),
  clusterSummary: (cluster: string, signal?: AbortSignal) =>
    apiGet<ClusterSummary>(clusterPath(cluster, "summary", {}), signal),
  hubSetup: (password: string, serverURL: string) =>
    apiSend<undefined>("POST", "/api/v1/hub/setup", { password, serverURL }),
  importCluster: (name: string, owner: boolean) =>
    apiSend<ImportCommand>("POST", "/api/v1/hub/clusters", { name, owner }),
  reimportCluster: (cluster: string, owner: boolean) =>
    apiSend<ImportCommand>("POST", `/api/v1/hub/clusters/${encodeURIComponent(cluster)}/import`, {
      owner,
    }),
  /** A new kubeconfig for every cluster you see, starting in current. */
  kubeconfig: async (current?: string) => {
    const res = await apiSendRaw("POST", "/api/v1/hub/kubeconfig", { current });
    return { yaml: await res.text(), expires: res.headers.get("X-Glowpod-Token-Expires") ?? "" };
  },
  kubeconfigTokens: (signal?: AbortSignal) =>
    apiGet<KubeTokenList>("/api/v1/hub/kubeconfig/tokens", signal),
  revokeKubeconfigToken: (id: string) =>
    apiSend("DELETE", `/api/v1/hub/kubeconfig/tokens/${encodeURIComponent(id)}`),
  members: (cluster: string, signal?: AbortSignal) =>
    apiGet<Members>(clusterPath(cluster, "members", {}), signal),
  setMember: (
    cluster: string,
    member: Subject & { role: ClusterRoleChoice; clusterRole?: string },
  ) => apiSend("PUT", clusterPath(cluster, "members", {}), member),
  removeMember: (cluster: string, s: Subject) =>
    apiSend("DELETE", clusterPath(cluster, "members", { kind: s.kind, name: s.name })),
  projects: (cluster: string, signal?: AbortSignal) =>
    apiGet<Projects>(clusterPath(cluster, "projects", {}), signal),
  createProject: (cluster: string, name: string, namespaces: string[]) =>
    apiSend("POST", clusterPath(cluster, "projects", {}), { name, namespaces }),
  setProjectNamespaces: (cluster: string, project: string, namespaces: string[]) =>
    apiSend("PUT", clusterPath(cluster, `projects/${encodeURIComponent(project)}/namespaces`, {}), {
      namespaces,
    }),
  deleteProject: (cluster: string, project: string) =>
    apiSend("DELETE", clusterPath(cluster, `projects/${encodeURIComponent(project)}`, {})),
  setProjectMember: (cluster: string, project: string, member: Subject & { role: ProjectRole }) =>
    apiSend(
      "PUT",
      clusterPath(cluster, `projects/${encodeURIComponent(project)}/members`, {}),
      member,
    ),
  removeProjectMember: (cluster: string, project: string, s: Subject) =>
    apiSend(
      "DELETE",
      clusterPath(cluster, `projects/${encodeURIComponent(project)}/members`, {
        kind: s.kind,
        name: s.name,
      }),
    ),
  hubUsers: (signal?: AbortSignal) =>
    apiGet<{ users: HubUser[] }>("/api/v1/hub/users", signal).then((r) => r.users),
  createHubUser: (username: string, role: HubUser["role"], password: string) =>
    apiSend("POST", "/api/v1/hub/users", { username, role, password }),
  updateHubUser: (username: string, change: { role?: HubUser["role"]; password?: string }) =>
    apiSend("PUT", `/api/v1/hub/users/${encodeURIComponent(username)}`, change),
  deleteHubUser: (username: string) =>
    apiSend("DELETE", `/api/v1/hub/users/${encodeURIComponent(username)}`),
  changePassword: (current: string, next: string) =>
    apiSend("POST", "/api/v1/hub/account/password", { current, new: next }),
  hubSettings: (signal?: AbortSignal) => apiGet<HubSettings>("/api/v1/hub/settings", signal),
  setServerURL: (serverURL: string) =>
    apiSend("PUT", "/api/v1/hub/settings/server-url", { serverURL }),
  setOIDC: (s: OIDCSettings) => apiSend("PUT", "/api/v1/hub/settings/oidc", s),
  setLDAP: (s: LDAPSettings) => apiSend("PUT", "/api/v1/hub/settings/ldap", s),
  checkOIDC: (s: OIDCSettings) =>
    apiSend<CheckResult>("POST", "/api/v1/hub/settings/oidc/check", s),
  checkLDAP: (s: LDAPSettings) =>
    apiSend<CheckResult>("POST", "/api/v1/hub/settings/ldap/check", s),
  /** An encrypted backup of the Hub's state, and its file name. */
  hubBackup: async (passphrase: string) => {
    const res = await apiSendRaw("POST", "/api/v1/hub/backup", { passphrase });
    const name = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") ?? "")?.[1];
    return { text: await res.text(), name: name ?? "glowpod-hub-backup.json" };
  },
  hubRestore: (passphrase: string, backup: string) =>
    apiSend<RestoreResult>("POST", "/api/v1/hub/restore", { passphrase, backup }),
  removeCluster: (cluster: string) =>
    apiSend<{ cleanup: string }>("DELETE", `/api/v1/hub/clusters/${encodeURIComponent(cluster)}`),
  namespaces: (cluster: string, signal?: AbortSignal) =>
    apiGet<NamespaceList>(clusterPath(cluster, "namespaces", {}), signal),
  authMethods: (signal?: AbortSignal) =>
    apiGet<{ methods: AuthMethod[] }>("/api/v1/auth/methods", signal).then((r) => r.methods),
  signInToken: (token: string) => signIn("/api/v1/auth/token", { token }),
  signInLDAP: (username: string, password: string) =>
    signIn("/api/v1/auth/ldap", { username, password }),
  signInPassword: (username: string, password: string) =>
    signIn("/api/v1/auth/password", { username, password }),
  signOut: async () => {
    await apiSend("POST", "/api/v1/auth/logout");
    csrfToken = "";
  },
  overview: (cluster: string, namespace: string, signal?: AbortSignal) =>
    apiGet<Overview>(clusterPath(cluster, "overview", { namespace }), signal),
  events: (cluster: string, namespace: string, limit: number, signal?: AbortSignal) =>
    apiGet<{ events: KubeEvent[] }>(
      clusterPath(cluster, "events", { namespace, limit: String(limit) }),
      signal,
    ).then((r) => r.events),
  objectEvents: (
    cluster: string,
    namespace: string,
    kind: string,
    name: string,
    signal?: AbortSignal,
  ) =>
    apiGet<{ events: KubeEvent[] }>(
      clusterPath(cluster, "events", { namespace, kind, name, limit: "50" }),
      signal,
    ).then((r) => r.events),
  streamURL: (cluster: string, namespace: string) =>
    wsURL(clusterPath(cluster, "stream", { namespace })),
  resourceTypes: (cluster: string, signal?: AbortSignal) =>
    apiGet<{ types: ResourceType[] }>(clusterPath(cluster, "resources", {}), signal).then(
      (r) => r.types,
    ),
  listResource: (cluster: string, ref: ResourceRef, opts: ListOptions, signal?: AbortSignal) =>
    apiGet<ResourceTable>(
      clusterPath(cluster, `resources/${refPath(ref)}`, {
        namespace: opts.namespace,
        labelSelector: opts.labelSelector,
        continue: opts.continue,
        limit: opts.limit ? String(opts.limit) : undefined,
      }),
      signal,
    ),
  watchURL: (
    cluster: string,
    ref: ResourceRef,
    opts: Pick<ListOptions, "namespace" | "labelSelector" | "fieldSelector">,
  ) =>
    wsURL(
      clusterPath(cluster, `watch/${refPath(ref)}`, {
        namespace: opts.namespace,
        labelSelector: opts.labelSelector,
        fieldSelector: opts.fieldSelector,
      }),
    ),
  logsURL: (cluster: string, namespace: string, pod: string, opts: LogOptions) =>
    wsURL(logsPath(cluster, namespace, pod, "", opts)),
  execURL: (cluster: string, namespace: string, pod: string, container: string) =>
    wsURL(
      clusterPath(cluster, `exec/${encodeURIComponent(namespace)}/${encodeURIComponent(pod)}`, {
        container,
      }),
    ),
  /** A Hub's browser shell for a cluster, starting in namespace. */
  shellURL: (cluster: string, namespace: string) =>
    wsURL(
      `/api/v1/hub/shell/${encodeURIComponent(cluster)}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ""}`,
    ),
  logsDownloadURL: (cluster: string, namespace: string, pod: string, opts: LogOptions) =>
    logsPath(cluster, namespace, pod, "/download", opts),
  getResource: (
    cluster: string,
    ref: ResourceRef,
    namespace: string,
    name: string,
    opts: { reveal?: boolean },
    signal?: AbortSignal,
  ) =>
    apiGet<ResourceObject>(
      clusterPath(cluster, `resources/${refPath(ref)}/${encodeURIComponent(name)}`, {
        namespace,
        format: "yaml",
        reveal: opts.reveal ? "true" : undefined,
      }),
      signal,
    ),
  apps: (cluster: string, namespace: string, signal?: AbortSignal) =>
    apiGet<{ apps: AppSummary[] }>(clusterPath(cluster, "apps", { namespace }), signal).then(
      (r) => r.apps,
    ),
  app: (cluster: string, namespace: string, name: string, signal?: AbortSignal) =>
    apiGet<AppDetail>(
      clusterPath(cluster, `apps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {}),
      signal,
    ),
  pod: (cluster: string, namespace: string, name: string, signal?: AbortSignal) =>
    apiGet<PodDetail>(
      clusterPath(cluster, `pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {}),
      signal,
    ),
  helmReleases: (cluster: string, namespace: string, signal?: AbortSignal) =>
    apiGet<{ releases: HelmRelease[] }>(clusterPath(cluster, "helm", { namespace }), signal).then(
      (r) => r.releases,
    ),
  helmRelease: (cluster: string, namespace: string, name: string, signal?: AbortSignal) =>
    apiGet<HelmReleaseDetail>(
      clusterPath(cluster, `helm/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {}),
      signal,
    ),
  resourceMap: (cluster: string, namespace: string, signal?: AbortSignal) =>
    apiGet<ResourceMap>(clusterPath(cluster, "map", { namespace }), signal),
  node: (cluster: string, name: string, signal?: AbortSignal) =>
    apiGet<NodeDetail>(clusterPath(cluster, `nodes/${encodeURIComponent(name)}`, {}), signal),
  cordon: (cluster: string, name: string, cordon: boolean) =>
    apiSend("POST", clusterPath(cluster, `nodes/${encodeURIComponent(name)}/cordon`, {}), {
      cordon,
    }),
  drainPlan: (cluster: string, name: string, signal?: AbortSignal) =>
    apiGet<DrainPlan>(clusterPath(cluster, `nodes/${encodeURIComponent(name)}/drain`, {}), signal),
  drain: (cluster: string, name: string) =>
    apiSend<DrainResult>(
      "POST",
      clusterPath(cluster, `nodes/${encodeURIComponent(name)}/drain`, {}),
    ),
  access: (cluster: string, checks: AccessCheck[]) =>
    apiSend<{ allowed: boolean[] }>("POST", clusterPath(cluster, "access", {}), { checks }).then(
      (r) => r.allowed,
    ),
  updateResource: (
    cluster: string,
    ref: ResourceRef,
    namespace: string,
    name: string,
    yaml: string,
    dryRun: boolean,
  ) =>
    apiSend<UpdateResult>(
      "PUT",
      objectPath(cluster, ref, namespace, name, "", { dryRun: dryRun ? "true" : undefined }),
      { yaml },
    ),
  createResource: (
    cluster: string,
    ref: ResourceRef,
    namespace: string,
    yaml: string,
    dryRun: boolean,
  ) =>
    apiSend<CreateResult>(
      "POST",
      clusterPath(cluster, `resources/${refPath(ref)}`, {
        namespace: namespace || undefined,
        dryRun: dryRun ? "true" : undefined,
      }),
      { yaml },
    ),
  apply: (cluster: string, namespace: string, yaml: string, dryRun: boolean, force = false) =>
    apiSend<ApplyResult>(
      "POST",
      clusterPath(cluster, "apply", {
        namespace: namespace || undefined,
        dryRun: dryRun ? "true" : undefined,
        force: force ? "true" : undefined,
      }),
      { yaml },
    ),
  deleteResource: (
    cluster: string,
    ref: ResourceRef,
    namespace: string,
    name: string,
    propagation?: "background" | "foreground" | "orphan",
  ) => apiSend("DELETE", objectPath(cluster, ref, namespace, name, "", { propagation })),
  scale: (cluster: string, ref: ResourceRef, namespace: string, name: string, replicas: number) =>
    apiSend("PUT", objectPath(cluster, ref, namespace, name, "/scale", {}), { replicas }),
  restart: (cluster: string, ref: ResourceRef, namespace: string, name: string) =>
    apiSend("POST", objectPath(cluster, ref, namespace, name, "/restart", {})),
};

function objectPath(
  cluster: string,
  ref: ResourceRef,
  namespace: string,
  name: string,
  suffix: string,
  params: Record<string, string | undefined>,
) {
  return clusterPath(cluster, `resources/${refPath(ref)}/${encodeURIComponent(name)}${suffix}`, {
    namespace: namespace || undefined,
    ...params,
  });
}

export interface ListOptions {
  namespace?: string;
  labelSelector?: string;
  fieldSelector?: string;
  continue?: string;
  limit?: number;
}

export interface LogOptions {
  container?: string;
  previous?: boolean;
  tailLines?: number;
}

function logsPath(
  cluster: string,
  namespace: string,
  pod: string,
  suffix: string,
  opts: LogOptions,
) {
  return clusterPath(
    cluster,
    `logs/${encodeURIComponent(namespace)}/${encodeURIComponent(pod)}${suffix}`,
    {
      container: opts.container,
      previous: opts.previous ? "true" : undefined,
      tailLines: opts.tailLines ? String(opts.tailLines) : undefined,
    },
  );
}

function refPath(ref: ResourceRef): string {
  return [ref.group || "core", ref.version, ref.resource].map(encodeURIComponent).join("/");
}

function wsURL(path: string): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return proto + "//" + window.location.host + path;
}
