/**
 * Where new namespaced objects go when their YAML doesn't say: the picked
 * namespace, the first of several, or "default" across all of them.
 */
export function targetNamespace(namespaces: readonly string[]): string {
  return namespaces[0] ?? "default";
}
