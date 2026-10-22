import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useToast } from "../../components/Toast";
import { useAccess, useResourceType } from "../../lib/access";
import { ApiError } from "../../lib/api";
import type { AccessCheck, ResourceRef } from "../../lib/types";

const RESTARTABLE = new Set(["deployments", "statefulsets", "daemonsets"]);

/** Which actions to show for one object: the API supports them and RBAC allows them. */
export interface Allowed {
  edit: boolean;
  scale: boolean;
  restart: boolean;
  delete: boolean;
  exec: boolean;
}

export function useAllowedActions(ref: ResourceRef, namespace: string, name: string): Allowed {
  const type = useResourceType(ref);
  const group = ref.group === "core" ? "" : ref.group;
  const isPod = group === "" && ref.resource === "pods";
  const base = { group, resource: ref.resource, ...(namespace ? { namespace } : {}), name };
  const checks: AccessCheck[] = type
    ? [
        { ...base, verb: "update" },
        { ...base, verb: "patch", subresource: "scale" },
        { ...base, verb: "patch" },
        { ...base, verb: "delete" },
        // exec is a "create" on pods/exec (and some clusters still check "get").
        { ...base, verb: "create", subresource: "exec" },
      ]
    : [];
  const [update, scale, patch, del, exec] = useAccess(checks);
  const verbs = type?.verbs ?? [];
  const subs = type?.subresources ?? [];
  return {
    edit: verbs.includes("update") && !!update,
    scale: subs.includes("scale") && !!scale,
    restart: group === "apps" && RESTARTABLE.has(ref.resource) && !!patch,
    delete: verbs.includes("delete") && !!del,
    exec: isPod && subs.includes("exec") && !!exec,
  };
}

/** A plain-language message for a failed change, with the cluster's detail. */
export function failure(err: unknown): { message: string; detail?: string } {
  if (err instanceof ApiError) {
    return { message: err.message, ...(err.detail ? { detail: err.detail } : {}) };
  }
  return { message: "Something went wrong." };
}

/**
 * Runs a change, then refreshes what it may have touched and says how it
 * went. Errors are shown by the caller (in its dialog); success is a toast.
 */
export function useChange<V>(run: (v: V) => Promise<unknown>, done: (v: V) => string) {
  const client = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: run,
    onSuccess: async (_, v) => {
      toast({ tone: "ok", message: done(v) });
      await client.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== "session" && q.queryKey[0] !== "resourceTypes",
      });
    },
  });
}
