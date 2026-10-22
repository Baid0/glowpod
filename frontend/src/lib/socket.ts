/** Reconnect delays after a drop, in ms; the last one repeats. */
export const BACKOFF_MS = [1_000, 2_000, 5_000, 10_000, 30_000];

export interface SocketHandlers<T> {
  onMessage: (msg: T, reconnected: boolean) => void;
  /** The connection dropped; a reconnect is scheduled. */
  onDrop?: () => void;
}

/**
 * Opens a JSON WebSocket that reconnects with backoff until closed. Returns
 * a function that closes it for good. `reconnected` is true for messages on
 * any connection after the first, so callers can refetch what they missed.
 */
export function connectSocket<T>(url: string, handlers: SocketHandlers<T>): () => void {
  let ws: WebSocket | null = null;
  let attempt = 0;
  let opened = 0;
  let closed = false;
  let retry: ReturnType<typeof setTimeout> | undefined;

  const connect = () => {
    ws = new WebSocket(url);
    opened++;
    const reconnected = opened > 1;
    ws.onmessage = (e: MessageEvent<string>) => {
      let msg: T;
      try {
        msg = JSON.parse(e.data) as T;
      } catch {
        return;
      }
      attempt = 0;
      handlers.onMessage(msg, reconnected);
    };
    ws.onclose = () => {
      if (closed) return;
      handlers.onDrop?.();
      const delay = BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)];
      attempt++;
      retry = setTimeout(connect, delay);
    };
  };

  connect();
  return () => {
    closed = true;
    clearTimeout(retry);
    ws?.close();
  };
}
