import { useEffect, useState } from "react";

import { api, type LogOptions } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { connectSocket } from "../../lib/socket";
import { levelOf, stripAnsi, type LogLine } from "./lines";

/** The viewer keeps at most this many lines; older ones scroll away. */
export const MAX_LINES = 10_000;
export const TAIL_LINES = 1_000;

export type LogStatus = "connecting" | "streaming" | "reconnecting" | "ended" | "error";

interface LogMessage {
  type: "ready" | "lines" | "end" | "error";
  lines?: { t?: string; x: string }[];
  code?: string;
  message?: string;
}

interface State {
  key: string;
  lines: LogLine[];
  status: LogStatus;
  message?: string;
  dropped: number;
}

/**
 * Follows a container's log. A dropped connection starts over with a fresh
 * tail (no gaps, no duplicates); "end" and "error" stop for good.
 */
export function useLogStream(namespace: string, pod: string, opts: LogOptions) {
  const cluster = useClusterId();
  const key = [cluster, namespace, pod, opts.container ?? "", String(opts.previous ?? false)].join(
    "|",
  );
  const [state, setState] = useState<State>({
    key: "",
    lines: [],
    status: "connecting",
    dropped: 0,
  });

  useEffect(() => {
    let nextId = 0;
    // Handlers run only after connectSocket returns, so stop is always set.
    const stop = connectSocket<LogMessage>(
      api.logsURL(cluster, namespace, pod, { ...opts, tailLines: TAIL_LINES }),
      {
        onMessage: (msg) => {
          switch (msg.type) {
            case "ready":
              setState({ key, lines: [], status: "streaming", dropped: 0 });
              break;
            case "lines": {
              const incoming = (msg.lines ?? []).map((l) => {
                const x = stripAnsi(l.x);
                return { id: nextId++, ...(l.t ? { t: l.t } : {}), x, level: levelOf(x) };
              });
              setState((s) => {
                const all = s.lines.concat(incoming);
                const over = Math.max(0, all.length - MAX_LINES);
                return { ...s, lines: over ? all.slice(over) : all, dropped: s.dropped + over };
              });
              break;
            }
            case "end":
            case "error":
              stop();
              setState((s) => ({
                ...s,
                key,
                status: msg.type === "end" ? "ended" : "error",
                ...(msg.message ? { message: msg.message } : {}),
              }));
              break;
          }
        },
        onDrop: () => {
          setState((s) => ({ ...s, key, status: "reconnecting" }));
        },
      },
    );
    return () => {
      stop();
    };
    // key captures cluster, namespace, pod and the options.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return state.key === key ? state : { key, lines: [], status: "connecting" as const, dropped: 0 };
}
