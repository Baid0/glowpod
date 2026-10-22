import type { QueryClient } from "@tanstack/react-query";
import { createContext, useContext } from "react";

import { queryKeys } from "./queryKeys";
import type { KubeEvent, StreamMessage } from "./types";

export type LiveStatus = "connecting" | "live" | "reconnecting";

export interface Live {
  status: LiveStatus;
  /** When the stream last delivered something, as epoch ms. */
  lastMessageAt: number | null;
}

export const LiveContext = createContext<Live>({ status: "connecting", lastMessageAt: null });

const MAX_EVENTS = 50;
/** Pod changes come in bursts; refetch the Overview at most this often. */
export const POD_REFRESH_DEBOUNCE_MS = 2_000;

/** Applies one stream message to the query cache. */
export function applyMessage(
  client: QueryClient,
  cluster: string,
  namespace: string,
  msg: StreamMessage,
  schedulePodRefresh: () => void,
) {
  switch (msg.type) {
    case "event":
      client.setQueryData<KubeEvent[]>(queryKeys.events(cluster, namespace), (old) => {
        if (!old) return old; // not loaded yet; the first fetch will include it
        const rest = old.filter((e) => e.id !== msg.event.id);
        return [msg.event, ...rest]
          .sort((a, b) => Date.parse(b.time) - Date.parse(a.time))
          .slice(0, MAX_EVENTS);
      });
      break;
    case "pod":
      schedulePodRefresh();
      break;
    case "resync":
      void client.invalidateQueries({
        queryKey:
          msg.resource === "events"
            ? queryKeys.events(cluster, namespace)
            : queryKeys.overview(cluster, namespace),
      });
      break;
    default:
      break;
  }
}

export function useLive(): Live {
  return useContext(LiveContext);
}
