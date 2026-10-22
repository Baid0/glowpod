import { useMemo } from "react";
import { Navigate, useLocation, useParams } from "react-router";

import { NotFound } from "../features/placeholder/NotFound";
import { ApiError, DEFAULT_CLUSTER } from "../lib/api";
import { LiveProvider } from "../lib/LiveProvider";
import { forCluster, PreferencesContext, usePreferences } from "../lib/preferences";
import { useClusters, useOverview, useSession } from "../lib/queries";
import { Shell } from "./Shell";

/**
 * The pages of one cluster, /c/<cluster>/…: the shell, the live stream and
 * the namespaces picked on this cluster.
 */
export function ClusterScope() {
  const { cluster = DEFAULT_CLUSTER } = useParams();
  const prefs = usePreferences();
  const scoped = useMemo(() => forCluster(prefs, cluster), [prefs, cluster]);
  const { data: clusters } = useClusters();

  if (clusters && !clusters.some((c) => c.id === cluster)) {
    return (
      <Shell home>
        <NotFound what="cluster" />
      </Shell>
    );
  }
  return (
    <PreferencesContext value={scoped}>
      {/* Keyed: another cluster's last message doesn't make this one look fresh. */}
      <LiveProvider key={cluster}>
        <SignInAgainIfRejected />
        <Shell />
      </LiveProvider>
    </PreferencesContext>
  );
}

/**
 * In the cluster, the cluster itself refusing the user's token means it
 * expired or was revoked. (Locally it means a bad kubeconfig; the page says so.)
 */
function SignInAgainIfRejected() {
  const session = useSession();
  const overview = useOverview();
  const location = useLocation();
  if (
    session.data?.mode === "in-cluster" &&
    overview.error instanceof ApiError &&
    overview.error.code === "cluster_unauthorized"
  ) {
    const here = location.pathname + location.search;
    return (
      <Navigate to={`/sign-in?error=cluster_rejected&return=${encodeURIComponent(here)}`} replace />
    );
  }
  return null;
}

/** "/": the Clusters page, or straight into the only cluster there is. */
export function Home({ list }: { list: React.ReactNode }) {
  const { data: clusters } = useClusters();
  const only = clusters?.length === 1 ? clusters[0] : undefined;
  if (only) return <Navigate to={`/c/${encodeURIComponent(only.id)}/`} replace />;
  return list;
}

/**
 * Links from before there were many clusters (/workloads/pods, bookmarks,
 * the docs) open on the current cluster.
 */
export function OnCurrentCluster() {
  const location = useLocation();
  const { data: clusters, error } = useClusters();
  if (error instanceof ApiError && error.status === 401) {
    const here = location.pathname + location.search;
    return <Navigate to={`/sign-in?return=${encodeURIComponent(here)}`} replace />;
  }
  if (!clusters) return null;
  const current = clusters.find((c) => c.current) ?? clusters[0];
  if (!current) return <Navigate to="/" replace />;
  return (
    <Navigate
      to={`/c/${encodeURIComponent(current.id)}${location.pathname}${location.search}${location.hash}`}
      replace
    />
  );
}
