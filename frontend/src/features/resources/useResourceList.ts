import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import { connectSocket } from "../../lib/socket";
import type { ResourceRef, WatchMessage } from "../../lib/types";

/** Rows per request. Lists load more as you scroll. */
export const PAGE_SIZE = 100;
/** Changes come in bursts; refresh the list at most this often. */
export const CHANGE_DEBOUNCE_MS = 1_000;

/**
 * Pages through a resource's Table output and keeps it fresh: a watch on
 * the same resource, namespace and selector refetches the loaded pages
 * whenever something changes.
 */
export function useResourceList(ref: ResourceRef, namespace: string, labelSelector: string) {
  const cluster = useClusterId();
  const client = useQueryClient();
  const key = queryKeys.resourceList(
    cluster,
    ref.group,
    ref.version,
    ref.resource,
    namespace,
    labelSelector,
  );
  const socketKey = key.join("|");
  const [live, setLive] = useState<{ key: string; on: boolean }>({ key: "", on: false });

  const query = useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam, signal }) =>
      api.listResource(
        cluster,
        ref,
        {
          namespace,
          labelSelector,
          limit: PAGE_SIZE,
          ...(pageParam ? { continue: pageParam } : {}),
        },
        signal,
      ),
    initialPageParam: "",
    // The API omits "continue" on the last page.
    getNextPageParam: (last) => last.continue,
  });

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = () => {
      timer ??= setTimeout(() => {
        timer = undefined;
        void client.invalidateQueries({ queryKey: key });
      }, CHANGE_DEBOUNCE_MS);
    };
    const close = connectSocket<WatchMessage>(
      api.watchURL(cluster, ref, { namespace, labelSelector }),
      {
        onMessage: (msg, reconnected) => {
          if (msg.type === "error") {
            setLive({ key: socketKey, on: false });
            return;
          }
          setLive({ key: socketKey, on: true });
          // changed or resync, or a reconnect that may have missed changes
          if (msg.type !== "ready" || reconnected) refresh();
        },
        onDrop: () => {
          setLive({ key: socketKey, on: false });
        },
      },
    );
    return () => {
      close();
      clearTimeout(timer);
    };
    // key is derived from these; socketKey captures them as one value.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, socketKey]);

  return { query, live: live.key === socketKey && live.on };
}
