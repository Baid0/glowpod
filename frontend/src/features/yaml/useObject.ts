import { useQuery } from "@tanstack/react-query";

import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { ResourceRef } from "../../lib/types";

export function isSecret(ref: ResourceRef): boolean {
  return (ref.group === "" || ref.group === "core") && ref.resource === "secrets";
}

/**
 * One object as YAML. Revealed Secret values are never cached: they are
 * fetched fresh on each reveal and dropped as soon as nothing shows them.
 */
export function useObject(ref: ResourceRef, namespace: string, name: string, reveal = false) {
  const cluster = useClusterId();
  return useQuery({
    queryKey: queryKeys.object(
      cluster,
      ref.group,
      ref.version,
      ref.resource,
      namespace,
      name,
      reveal,
    ),
    queryFn: ({ signal }) => api.getResource(cluster, ref, namespace, name, { reveal }, signal),
    ...(reveal ? { gcTime: 0, staleTime: 0 } : { staleTime: 10_000 }),
  });
}
