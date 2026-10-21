import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";

import { api } from "./api";
import { applyMessage, BACKOFF_MS, LiveContext, POD_REFRESH_DEBOUNCE_MS, type Live } from "./live";
import { usePreferences } from "./preferences";
import { useClusterId } from "./queries";
import { queryKeys } from "./queryKeys";
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
    let ws: WebSocket | null = null;
    let attempt = 0;
    let closed = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let podTimer: ReturnType<typeof setTimeout> | undefined;

    const schedulePodRefresh = () => {
      podTimer ??= setTimeout(() => {
        podTimer = undefined;
        void client.invalidateQueries({ queryKey: queryKeys.overview(cluster, namespace) });
      }, POD_REFRESH_DEBOUNCE_MS);
    };

    const connect = () => {
      ws = new WebSocket(api.streamURL(cluster, namespace));
      ws.onmessage = (e: MessageEvent<string>) => {
        let msg: StreamMessage;
        try {
          msg = JSON.parse(e.data) as StreamMessage;
        } catch {
          return;
        }
        if (msg.type === "ready") {
          // Anything may have changed while we were away.
          if (attempt > 0) void client.invalidateQueries({ queryKey: ["events", cluster] });
          attempt = 0;
        }
        setState({ key, status: "live", lastMessageAt: Date.now() });
        applyMessage(client, cluster, namespace, msg, schedulePodRefresh);
      };
      ws.onclose = () => {
        if (closed) return;
        setState((s) => ({ ...s, key, status: "reconnecting" }));
        const delay = BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)];
        attempt++;
        retry = setTimeout(connect, delay);
      };
    };

    connect();
    return () => {
      closed = true;
      clearTimeout(retry);
      clearTimeout(podTimer);
      ws?.close();
    };
  }, [client, cluster, namespace, key]);

  const live: Live =
    state.key === key ? state : { status: "connecting", lastMessageAt: state.lastMessageAt };
  return <LiveContext value={live}>{children}</LiveContext>;
}
