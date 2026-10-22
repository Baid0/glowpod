import type { StatusTone } from "../../components/Badge";
import type { AppStatus } from "../../lib/types";

export const APP_BADGES: Record<AppStatus, [StatusTone, string]> = {
  ok: ["ok", "Healthy"],
  high: ["high", "Rolling out"],
  critical: ["critical", "Failing"],
};

export function appPath(a: { namespace: string; name: string }, tab?: "logs"): string {
  const base = `/apps/${encodeURIComponent(a.namespace)}/${encodeURIComponent(a.name)}`;
  return tab ? `${base}?tab=${tab}` : base;
}
