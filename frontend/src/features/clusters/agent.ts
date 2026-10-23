import type { ClusterInfo } from "../../lib/types";

/** A Hub's imported cluster without its agent has nothing to ask. */
export const agentDown = (c: ClusterInfo) => c.state === "pending" || c.state === "disconnected";
