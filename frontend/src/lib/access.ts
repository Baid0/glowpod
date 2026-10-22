import { useQuery } from "@tanstack/react-query";

import { api } from "./api";
import { useClusterId } from "./queries";
import { queryKeys } from "./queryKeys";
import type { AccessCheck, ResourceRef, ResourceType } from "./types";

/** Every resource type the cluster serves to this user. */
export function useResourceTypes() {
  const cluster = useClusterId();
  return useQuery({
    queryKey: queryKeys.resourceTypes(cluster),
    queryFn: ({ signal }) => api.resourceTypes(cluster, signal),
    staleTime: 5 * 60_000,
  });
}

/** The discovery entry for ref: its verbs and subresources. */
export function useResourceType(ref: ResourceRef): ResourceType | undefined {
  const { data } = useResourceTypes();
  const group = ref.group === "core" ? "" : ref.group;
  return data?.find(
    (t) => t.group === group && t.version === ref.version && t.resource === ref.resource,
  );
}

/**
 * Asks the cluster which of checks the signed-in user may do. Until it
 * answers, everything reads as not allowed, so buttons appear rather than
 * vanish. The cluster still decides on the real request.
 */
export function useAccess(checks: AccessCheck[]): boolean[] {
  const cluster = useClusterId();
  const key = JSON.stringify(checks);
  const { data } = useQuery({
    queryKey: queryKeys.access(cluster, key),
    queryFn: () => api.access(cluster, checks),
    enabled: checks.length > 0,
    staleTime: 60_000,
  });
  return checks.map((_, i) => data?.[i] ?? false);
}
