import { useParams } from "react-router";

import { ResourceListPage } from "./ResourceListPage";

/** Any resource by URL, e.g. /resources/argoproj.io/v1alpha1/applications. */
export function GenericList() {
  const { group = "", version = "", resource = "" } = useParams();
  const ref = { group: group === "core" ? "" : group, version, resource };
  return <ResourceListPage key={`${group}/${version}/${resource}`} resource={ref} />;
}
