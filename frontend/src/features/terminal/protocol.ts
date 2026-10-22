/** Control messages on the exec WebSocket (output and keystrokes are binary). */
export interface ExecMessage {
  type: "ready" | "exit" | "error";
  code?: string;
  exit?: number;
  message?: string;
}

export type TerminalStatus = "connecting" | "connected" | "ended" | "failed";

export function parseExecMessage(data: string): ExecMessage | null {
  try {
    const m = JSON.parse(data) as Partial<ExecMessage>;
    return m.type === "ready" || m.type === "exit" || m.type === "error"
      ? (m as ExecMessage)
      : null;
  } catch {
    return null;
  }
}
