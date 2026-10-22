import { useParams } from "react-router";

import { NotFound } from "../placeholder/ComingSoon";
import { ResourceDetailPage } from "./ResourceDetailPage";

/** /resources/:group/:version/:resource/:namespace/:name ("_" = no namespace). */
export function GenericDetail() {
  const { group = "", version = "", resource = "", namespace = "_", name = "" } = useParams();
  return (
    <ResourceDetailPage
      key={`${group}/${version}/${resource}/${namespace}/${name}`}
      resource={{ group: group === "core" ? "" : group, version, resource }}
      namespace={namespace === "_" ? "" : namespace}
      name={name}
    />
  );
}

const WORKLOADS: Record<string, { group: string; version: string }> = {
  deployments: { group: "apps", version: "v1" },
  statefulsets: { group: "apps", version: "v1" },
  daemonsets: { group: "apps", version: "v1" },
  replicasets: { group: "apps", version: "v1" },
  jobs: { group: "batch", version: "v1" },
  cronjobs: { group: "batch", version: "v1" },
};

/** /workloads/:kind/:namespace/:name for workload kinds other than pods. */
export function WorkloadDetail() {
  const { kind = "", namespace = "", name = "" } = useParams();
  const gv = WORKLOADS[kind];
  if (!gv) return <NotFound />;
  return (
    <ResourceDetailPage
      key={`${kind}/${namespace}/${name}`}
      resource={{ ...gv, resource: kind }}
      namespace={namespace}
      name={name}
    />
  );
}

/** /nodes/:name until Phase 4's node page. */
export function NodeDetail() {
  const { name = "" } = useParams();
  return (
    <ResourceDetailPage
      key={name}
      resource={{ group: "", version: "v1", resource: "nodes" }}
      namespace=""
      name={name}
    />
  );
}
