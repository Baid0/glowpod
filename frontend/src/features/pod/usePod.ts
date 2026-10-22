import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import { connectSocket } from "../../lib/socket";
import type { WatchMessage } from "../../lib/types";

/** Usage and events don't trigger the pod watch, so poll for those. */
export const POD_REFRESH_MS = 15_000;
const CHANGE_DEBOUNCE_MS = 500;

const PODS = { group: "", version: "v1", resource: "pods" };

/** One pod's detail, refreshed whenever the pod changes. */
export function usePod(namespace: string, name: string) {
  const cluster = useClusterId();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: queryKeys.pod(cluster, namespace, name),
    queryFn: ({ signal }) => api.pod(cluster, namespace, name, signal),
    refetchInterval: POD_REFRESH_MS,
    refetchIntervalInBackground: false,
  });

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const close = connectSocket<WatchMessage>(
      api.watchURL(cluster, PODS, { namespace, fieldSelector: `metadata.name=${name}` }),
      {
        onMessage: (msg, reconnected) => {
          if (msg.type === "error" || (msg.type === "ready" && !reconnected)) return;
          timer ??= setTimeout(() => {
            timer = undefined;
            void client.invalidateQueries({ queryKey: queryKeys.pod(cluster, namespace, name) });
          }, CHANGE_DEBOUNCE_MS);
        },
      },
    );
    return () => {
      close();
      clearTimeout(timer);
    };
  }, [client, cluster, namespace, name]);

  return query;
}
