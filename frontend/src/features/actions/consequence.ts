/** What deleting does, in a sentence, for the kinds where it isn't obvious. */
export function deleteConsequence(resource: string, ownerKind?: string): string {
  switch (resource) {
    case "pods":
      return ownerKind
        ? `Its ${ownerKind} starts a replacement right away, so this is a way to restart one pod.`
        : "Nothing will replace it. This can't be undone.";
    case "deployments":
    case "replicasets":
    case "daemonsets":
      return "Its pods stop and are removed. This can't be undone.";
    case "statefulsets":
      return "Its pods stop. Their volumes (PersistentVolumeClaims) stay. This can't be undone.";
    case "namespaces":
      return "Everything inside it is deleted too. This can't be undone.";
    case "persistentvolumeclaims":
      return "The data on it may be lost, depending on its reclaim policy. This can't be undone.";
    default:
      return "This can't be undone.";
  }
}
