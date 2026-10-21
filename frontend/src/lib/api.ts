import type { KubeEvent, Overview, Session } from "./types";

/** The only cluster in v1. Routes already carry the id for multi-cluster later. */
export const DEFAULT_CLUSTER = "default";

/** An API error with the server's code and plain-language message. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

interface ErrorBody {
  error?: { code?: string; message?: string };
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { signal, headers: { Accept: "application/json" } });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw new ApiError(0, "server_unreachable", "Can't reach the Glowpod server. Is it running?");
  }
  if (!res.ok) {
    let body: ErrorBody = {};
    try {
      body = (await res.json()) as ErrorBody;
    } catch {
      // not JSON; fall through to a generic message
    }
    throw new ApiError(
      res.status,
      body.error?.code ?? "http_" + String(res.status),
      body.error?.message ?? "Something went wrong.",
    );
  }
  return (await res.json()) as T;
}

function clusterPath(cluster: string, rest: string, params: Record<string, string | undefined>) {
  const query = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) query.set(k, v);
  const qs = query.toString();
  return `/api/v1/clusters/${encodeURIComponent(cluster)}/${rest}${qs ? "?" + qs : ""}`;
}

export const api = {
  session: (signal?: AbortSignal) => apiGet<Session>("/api/v1/session", signal),
  namespaces: (cluster: string, signal?: AbortSignal) =>
    apiGet<{ namespaces: string[] }>(clusterPath(cluster, "namespaces", {}), signal).then(
      (r) => r.namespaces,
    ),
  overview: (cluster: string, namespace: string, signal?: AbortSignal) =>
    apiGet<Overview>(clusterPath(cluster, "overview", { namespace }), signal),
  events: (cluster: string, namespace: string, limit: number, signal?: AbortSignal) =>
    apiGet<{ events: KubeEvent[] }>(
      clusterPath(cluster, "events", { namespace, limit: String(limit) }),
      signal,
    ).then((r) => r.events),
  streamURL: (cluster: string, namespace: string) => {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    return proto + "//" + window.location.host + clusterPath(cluster, "stream", { namespace });
  },
};
