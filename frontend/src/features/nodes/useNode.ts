import { useQuery } from "@tanstack/react-query";

import { api } from "../../lib/api";
import { OVERVIEW_REFRESH_MS, useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";

/** One node's page data, refreshed like the Overview's gauges. */
export function useNode(name: string) {
  const cluster = useClusterId();
  return useQuery({
    queryKey: queryKeys.node(cluster, name),
    queryFn: ({ signal }) => api.node(cluster, name, signal),
    refetchInterval: OVERVIEW_REFRESH_MS,
    placeholderData: (prev) => prev,
  });
}
