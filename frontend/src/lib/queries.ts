import { QueryCache, QueryClient, useQuery } from "@tanstack/react-query";
import { useParams } from "react-router";

import { api, ApiError, DEFAULT_CLUSTER } from "./api";
import { usePreferences } from "./preferences";
import { queryKeys } from "./queryKeys";

/** The default gauge refresh; people can change it in Settings. TanStack
 * pauses refreshes while the tab is hidden. */
export const OVERVIEW_REFRESH_MS = 15_000;

/** The thresholds in effect: this user's for the cluster, else the server's. */
export function useThresholds(server: [number, number] | undefined): [number, number] | undefined {
  const cluster = useClusterId();
  const { thresholdsFor } = usePreferences();
  return thresholdsFor(cluster) ?? server;
}
export const EVENTS_LIMIT = 20;

/** Cluster errors that come after waiting the full connect time. */
const NO_RETRY_CODES = ["cluster_timeout", "cluster_unreachable"];

export function createQueryClient() {
  const client: QueryClient = new QueryClient({
    // Any call finding the session gone makes the session query ask again;
    // the shell then sends the user to sign in.
    queryCache: new QueryCache({
      onError: (err, query) => {
        if (
          err instanceof ApiError &&
          err.code === "unauthenticated" &&
          query.queryKey[0] !== "session"
        ) {
          void client.invalidateQueries({ queryKey: queryKeys.session });
        }
      },
    }),
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: true,
        retry: (count, err) => {
          // Don't retry what won't change on its own, or a cluster that
          // already had its full time to connect and didn't.
          if (err instanceof ApiError && [400, 401, 403, 404].includes(err.status)) return false;
          if (err instanceof ApiError && NO_RETRY_CODES.includes(err.code)) return false;
          return count < 2;
        },
      },
    },
  });
  return client;
}

export function useSession() {
  return useQuery({
    queryKey: queryKeys.session,
    queryFn: ({ signal }) => api.session(signal),
    staleTime: Infinity,
  });
}

/** The cluster in the URL (/c/<cluster>/…). Outside a cluster's pages, and
 * in tests that render a page on its own, the in-cluster one. */
export function useClusterId(): string {
  return useParams().cluster ?? DEFAULT_CLUSTER;
}

/** Every cluster this Glowpod serves. */
export function useClusters() {
  return useQuery({
    queryKey: queryKeys.clusters,
    queryFn: ({ signal }) => api.clusters(signal),
    staleTime: 60_000,
  });
}

/** How often rows on the Clusters page refresh. */
export const SUMMARY_REFRESH_MS = 30_000;

/** One row of the Clusters page. A cluster that's down doesn't retry for long. */
export function useClusterSummary(cluster: string) {
  return useQuery({
    queryKey: queryKeys.clusterSummary(cluster),
    queryFn: ({ signal }) => api.clusterSummary(cluster, signal),
    refetchInterval: SUMMARY_REFRESH_MS,
    retry: false,
  });
}

export function useNamespaces() {
  const cluster = useClusterId();
  return useQuery({
    queryKey: queryKeys.namespaces(cluster),
    queryFn: ({ signal }) => api.namespaces(cluster, signal),
    staleTime: 60_000,
  });
}

export function useOverview() {
  const cluster = useClusterId();
  const { namespace, refreshMs } = usePreferences();
  return useQuery({
    queryKey: queryKeys.overview(cluster, namespace),
    queryFn: ({ signal }) => api.overview(cluster, namespace, signal),
    refetchInterval: refreshMs || false,
    refetchIntervalInBackground: false,
    placeholderData: (prev) => prev,
  });
}

export function useEvents() {
  const cluster = useClusterId();
  const { namespace } = usePreferences();
  return useQuery({
    queryKey: queryKeys.events(cluster, namespace),
    queryFn: ({ signal }) => api.events(cluster, namespace, EVENTS_LIMIT, signal),
    // The live stream keeps this fresh; refetch only on resync or focus.
    staleTime: Infinity,
  });
}
