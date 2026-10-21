import { QueryClient, useQuery } from "@tanstack/react-query";

import { api, ApiError, DEFAULT_CLUSTER } from "./api";
import { usePreferences } from "./preferences";
import { queryKeys } from "./queryKeys";

/** Gauges refresh every 15 s; TanStack pauses this while the tab is hidden. */
export const OVERVIEW_REFRESH_MS = 15_000;
export const EVENTS_LIMIT = 20;

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: true,
        retry: (count, err) => {
          // Don't retry what won't change on its own.
          if (err instanceof ApiError && [400, 401, 403, 404].includes(err.status)) return false;
          return count < 2;
        },
      },
    },
  });
}

export function useSession() {
  return useQuery({
    queryKey: queryKeys.session,
    queryFn: ({ signal }) => api.session(signal),
    staleTime: Infinity,
  });
}

export function useClusterId(): string {
  const { data } = useSession();
  return data?.clusters[0]?.id ?? DEFAULT_CLUSTER;
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
  const { namespace } = usePreferences();
  return useQuery({
    queryKey: queryKeys.overview(cluster, namespace),
    queryFn: ({ signal }) => api.overview(cluster, namespace, signal),
    refetchInterval: OVERVIEW_REFRESH_MS,
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
