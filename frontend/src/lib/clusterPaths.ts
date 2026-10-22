import { useCallback } from "react";
import {
  useNavigate as useRouterNavigate,
  useParams,
  type NavigateOptions,
  type To,
} from "react-router";

// Every page of a cluster lives under /c/<cluster>/. Pages link to each other
// with plain paths ("/workloads/pods"); these add the cluster the page is on.
// clusterLinks.tsx has the matching Link, NavLink and Navigate.

/** Paths that aren't one cluster's pages, so they never get a cluster. */
const OUTSIDE = ["/c/", "/sign-in", "/auth/", "/api/", "/dev/"];

/** path on cluster: "/workloads" → "/c/kind-lab/workloads". */
export function clusterHref(cluster: string | undefined, path: string): string {
  if (!cluster || !path.startsWith("/") || OUTSIDE.some((p) => path.startsWith(p))) return path;
  return `/c/${encodeURIComponent(cluster)}${path}`;
}

/** The path within a cluster: "/c/kind-lab/workloads" → "/workloads". */
export function pathInCluster(pathname: string): string {
  const m = /^\/c\/[^/]+(\/.*)?$/.exec(pathname);
  return m ? (m[1] ?? "/") : pathname;
}

/** to on cluster, for React Router's to= props. */
export function inCluster(to: To, cluster: string | undefined): To {
  if (typeof to === "string") return clusterHref(cluster, to);
  return to.pathname ? { ...to, pathname: clusterHref(cluster, to.pathname) } : to;
}

export type ClusterNavigate = (to: To | number, options?: NavigateOptions) => void | Promise<void>;

/** React Router's useNavigate, on the cluster the page is on. */
export function useNavigate(): ClusterNavigate {
  const navigate = useRouterNavigate();
  const { cluster } = useParams();
  return useCallback<ClusterNavigate>(
    (to, options) =>
      typeof to === "number" ? navigate(to) : navigate(inCluster(to, cluster), options),
    [navigate, cluster],
  );
}
