import {
  IconAdjustmentsHorizontal,
  IconBox,
  IconDatabase,
  IconLayoutGrid,
  IconShare,
  type Icon,
} from "@tabler/icons-react";
import { keepPreviousData, useQueries } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { api } from "../../lib/api";
import { usePreferences } from "../../lib/preferences";
import { queryKeys } from "../../lib/queryKeys";
import type { ClusterInfo, SearchHit, SearchResult } from "../../lib/types";
import { agentDown } from "../clusters/agent";
import { objectPagePath } from "../resources/cells";

/** Searches start from this many characters. */
export const MIN_QUERY = 2;
const DEBOUNCE_MS = 200;

/** value, once it has stopped changing for ms. */
export function useDebounced<T>(value: T, ms = DEBOUNCE_MS): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => {
      setSettled(value);
    }, ms);
    return () => {
      clearTimeout(t);
    };
  }, [value, ms]);
  return settled;
}

export interface ClusterSearch {
  cluster: ClusterInfo;
  data: SearchResult | undefined;
  error: Error | null;
  pending: boolean;
}

/**
 * Asks every cluster that can answer for objects named like query, at once.
 * Each cluster answers as the signed-in user, on its own time.
 */
export function useSearch(clusters: ClusterInfo[], query: string): ClusterSearch[] {
  const { namespaceFor } = usePreferences();
  const q = query.trim();
  const enabled = q.length >= MIN_QUERY;
  const asked = clusters.filter((c) => !agentDown(c));
  const results = useQueries({
    queries: asked.map((c) => {
      const namespace = namespaceFor(c.id);
      return {
        queryKey: queryKeys.search(c.id, q, namespace),
        queryFn: ({ signal }: { signal: AbortSignal }) => api.search(c.id, q, namespace, signal),
        enabled,
        staleTime: 30_000,
        retry: false,
        // While the next letter's answer comes, keep showing this one's.
        placeholderData: keepPreviousData,
      };
    }),
  });
  if (!enabled) return [];
  return asked.map((cluster, i) => {
    const r = results[i];
    return {
      cluster,
      data: r?.data,
      error: r?.error ?? null,
      // Still showing the last query's hits while this one's come.
      pending: r ? r.isPending || r.isPlaceholderData : true,
    };
  });
}

const WORKLOADS = new Set(["deployments", "statefulsets", "daemonsets", "jobs", "cronjobs"]);

/** The page of a found object, inside its cluster. */
export function hitPath(cluster: string, h: SearchHit): string {
  const base = `/c/${encodeURIComponent(cluster)}`;
  const ns = encodeURIComponent(h.namespace ?? "");
  const name = encodeURIComponent(h.name);
  if (h.group === "" && h.resource === "nodes") return `${base}/nodes/${name}`;
  if (WORKLOADS.has(h.resource) && h.namespace) {
    return `${base}/workloads/${h.resource}/${ns}/${name}`;
  }
  return base + objectPagePath(h, h.namespace ?? "", h.name);
}

const ICONS: Record<string, Icon> = {
  pods: IconBox,
  deployments: IconBox,
  statefulsets: IconBox,
  daemonsets: IconBox,
  jobs: IconBox,
  cronjobs: IconBox,
  services: IconShare,
  ingresses: IconShare,
  persistentvolumeclaims: IconDatabase,
  configmaps: IconAdjustmentsHorizontal,
  secrets: IconAdjustmentsHorizontal,
};

/** The icon of the section a found object lives in. */
export function hitIcon(h: SearchHit): Icon {
  return ICONS[h.resource] ?? IconLayoutGrid;
}
