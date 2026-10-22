import { useQuery } from "@tanstack/react-query";

import { api } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";

const REFRESH_MS = 15_000;

/** Apps in the selected namespace; pod changes on the live stream refresh it too. */
export function useApps() {
  const cluster = useClusterId();
  const { namespace } = usePreferences();
  return useQuery({
    queryKey: queryKeys.apps(cluster, namespace),
    queryFn: ({ signal }) => api.apps(cluster, namespace, signal),
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

export function useApp(namespace: string, name: string) {
  const cluster = useClusterId();
  return useQuery({
    queryKey: queryKeys.app(cluster, namespace, name),
    queryFn: ({ signal }) => api.app(cluster, namespace, name, signal),
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}
