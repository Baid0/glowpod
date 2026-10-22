import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";

import { api } from "./api";
import { applyMessage, LiveContext, POD_REFRESH_DEBOUNCE_MS, type Live } from "./live";
import { usePreferences } from "./preferences";
import { useClusterId } from "./queries";
import { queryKeys } from "./queryKeys";
import { connectSocket } from "./socket";
import type { StreamMessage } from "./types";

/** Keeps one WebSocket open for the current namespace and feeds the query cache. */
export function LiveProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const cluster = useClusterId();
  const { namespace } = usePreferences();
  const key = cluster + "/" + namespace;
  // State belongs to one socket (key); a new namespace reads as connecting
  // until its socket reports, without resetting state inside the effect.
  const [state, setState] = useState<Live & { key: string }>({
    key: "",
    status: "connecting",
    lastMessageAt: null,
  });

  useEffect(() => {
    let podTimer: ReturnType<typeof setTimeout> | undefined;
    const schedulePodRefresh = () => {
      podTimer ??= setTimeout(() => {
        podTimer = undefined;
        void client.invalidateQueries({ queryKey: queryKeys.overview(cluster, namespace) });
      }, POD_REFRESH_DEBOUNCE_MS);
    };

    const close = connectSocket<StreamMessage>(api.streamURL(cluster, namespace), {
      onMessage: (msg, reconnected) => {
        // Anything may have changed while we were away.
        if (msg.type === "ready" && reconnected) {
          void client.invalidateQueries({ queryKey: ["events", cluster] });
        }
        setState({ key, status: "live", lastMessageAt: Date.now() });
        applyMessage(client, cluster, namespace, msg, schedulePodRefresh);
      },
      onDrop: () => {
        setState((s) => ({ ...s, key, status: "reconnecting" }));
      },
    });
    return () => {
      close();
      clearTimeout(podTimer);
    };
  }, [client, cluster, namespace, key]);

  const live: Live =
    state.key === key ? state : { status: "connecting", lastMessageAt: state.lastMessageAt };
  return <LiveContext value={live}>{children}</LiveContext>;
}
