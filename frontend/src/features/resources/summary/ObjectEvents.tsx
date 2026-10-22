import { useQuery } from "@tanstack/react-query";

import { Card } from "../../../components/Card";
import { api } from "../../../lib/api";
import { useClusterId } from "../../../lib/queries";
import { queryKeys } from "../../../lib/queryKeys";
import { PodEvents } from "../../pod/PodEvents";
import styles from "./Summary.module.css";

/** What the cluster said about this one object. */
export function ObjectEvents({
  namespace,
  kind,
  name,
}: {
  namespace: string;
  kind: string;
  name: string;
}) {
  const cluster = useClusterId();
  const { data, error } = useQuery({
    queryKey: queryKeys.objectEvents(cluster, namespace, kind, name),
    queryFn: ({ signal }) => api.objectEvents(cluster, namespace, kind, name, signal),
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
  if (error) return null; // events are extra; the page works without them
  if (!data) return <div className={styles.skeleton} aria-label="Loading events" />;
  if (data.length === 0) {
    return (
      <Card title="Events">
        <p className={styles.muted}>No recent events. Kubernetes keeps them for about an hour.</p>
      </Card>
    );
  }
  return <PodEvents events={data} title="Events" />;
}
