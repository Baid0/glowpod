import { Card } from "../../../components/Card";
import { usePreferences } from "../../../lib/preferences";
import type { ResourceRef } from "../../../lib/types";
import { Conditions } from "./Conditions";
import { Chips } from "./Fields";
import {
  ClaimSummary,
  ConfigMapSummary,
  CrdSummary,
  IngressSummary,
  NetworkPolicySummary,
  SecretSummary,
  ServiceSummary,
  StorageClassSummary,
  VolumeSummary,
  WorkloadSummary,
} from "./kinds";
import { isWorkload } from "./logic";
import { ObjectEvents } from "./ObjectEvents";
import { record, str, at } from "./read";
import styles from "./Summary.module.css";

function KindSummary({
  resource,
  object,
  namespace,
}: {
  resource: ResourceRef;
  object: unknown;
  namespace: string;
}) {
  const props = { object, namespace };
  const id = `${resource.group || "core"}/${resource.resource}`;
  switch (id) {
    case "core/services":
      return <ServiceSummary {...props} />;
    case "networking.k8s.io/ingresses":
      return <IngressSummary {...props} />;
    case "networking.k8s.io/networkpolicies":
      return <NetworkPolicySummary {...props} />;
    case "core/persistentvolumeclaims":
      return <ClaimSummary {...props} />;
    case "core/persistentvolumes":
      return <VolumeSummary {...props} />;
    case "storage.k8s.io/storageclasses":
      return <StorageClassSummary {...props} />;
    case "core/configmaps":
      return <ConfigMapSummary {...props} />;
    case "core/secrets":
      return <SecretSummary {...props} />;
    case "apiextensions.k8s.io/customresourcedefinitions":
      return <CrdSummary {...props} />;
    default:
      if (resource.group === "apps" && isWorkload(resource.resource)) {
        return <WorkloadSummary {...props} resource={resource.resource} />;
      }
      return null;
  }
}

/**
 * The Overview tab of any object: a plain summary for kinds Glowpod knows,
 * then conditions (which most CRDs report too), annotations and events.
 */
export function Summary({
  resource,
  object,
  namespace,
  name,
}: {
  resource: ResourceRef;
  object: unknown;
  namespace: string;
  name: string;
}) {
  const { mode } = usePreferences();
  const kind = str(at(object, "kind")) ?? "";
  const annotations = Object.entries(record(at(object, "metadata", "annotations"))).filter(
    // The last-applied copy is the whole manifest again; the YAML tab has it.
    ([k]) => k !== "kubectl.kubernetes.io/last-applied-configuration",
  );
  return (
    <div className={styles.stack}>
      <KindSummary resource={resource} object={object} namespace={namespace} />
      <Conditions object={object} />
      {mode === "deep" && annotations.length > 0 && (
        <Card title="Annotations">
          <Chips
            items={annotations.map(([k, v]) => `${k}=${v.length > 80 ? v.slice(0, 80) + "…" : v}`)}
          />
        </Card>
      )}
      {kind && <ObjectEvents namespace={namespace} kind={kind} name={name} />}
    </div>
  );
}
