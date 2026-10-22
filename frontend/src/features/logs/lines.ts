import type { PodContainer } from "../../lib/types";

/** One log line as the viewer keeps it. */
export interface LogLine {
  id: number;
  /** RFC 3339 timestamp from the API server. */
  t?: string;
  /** The text, with terminal color codes removed. */
  x: string;
  level?: "error" | "warn";
}

// Terminal escape sequences (colors, cursor moves) that some apps log.
// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;
const ERROR =
  /\b(ERROR|ERR|FATAL|PANIC|CRITICAL|EXCEPTION|Traceback)\b|level=(error|fatal|panic)|"level":"(error|fatal|panic)"/i;
const WARN = /\b(WARN|WARNING)\b|level=warn|"level":"warn/i;

export function stripAnsi(s: string): string {
  return s.replace(ANSI, "");
}

/** Marks lines that look like errors or warnings, for a margin marker. */
export function levelOf(text: string): LogLine["level"] {
  if (ERROR.test(text)) return "error";
  if (WARN.test(text)) return "warn";
  return undefined;
}

/** Splits text around case-insensitive matches of query, for highlighting. */
export function splitMatches(text: string, query: string): { text: string; match: boolean }[] {
  if (!query) return [{ text, match: false }];
  const out: { text: string; match: boolean }[] = [];
  const lower = text.toLowerCase();
  const q = query.toLowerCase();
  let at = 0;
  for (let i = lower.indexOf(q); i >= 0; i = lower.indexOf(q, at)) {
    if (i > at) out.push({ text: text.slice(at, i), match: false });
    out.push({ text: text.slice(i, i + q.length), match: true });
    at = i + q.length;
  }
  if (at < text.length) out.push({ text: text.slice(at), match: false });
  return out;
}

/** "2026-10-22T03:00:01.123456789Z" → "03:00:01.123" in local time. */
export function shortTime(t: string): string {
  const d = new Date(t);
  if (Number.isNaN(d.getTime())) return t;
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

/** The container the log should open on: the struggling one, else the first app container. */
export function defaultContainer(containers: PodContainer[]): string {
  const struggling = containers.find(
    (c) => c.restarts > 0 || (c.state === "waiting" && c.role !== "init"),
  );
  return (struggling ?? containers.find((c) => c.role === "app") ?? containers[0])?.name ?? "";
}
