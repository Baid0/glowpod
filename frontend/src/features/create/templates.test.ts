import { describe, expect, it } from "vitest";

import { fromYaml } from "../../demo/yaml";
import { summarize } from "./outcomes";
import { targetNamespace } from "./namespace";
import { templateFor } from "./templates";

interface Parsed {
  apiVersion: string;
  kind: string;
  metadata: { name: string; namespace?: string; labels?: Record<string, string> };
  spec?: unknown;
}

const kinds: [string, string, string, boolean][] = [
  ["apps", "v1", "Deployment", true],
  ["apps", "v1", "StatefulSet", true],
  ["apps", "v1", "DaemonSet", true],
  ["", "v1", "Pod", true],
  ["", "v1", "Service", true],
  ["", "v1", "ConfigMap", true],
  ["", "v1", "Secret", true],
  ["", "v1", "PersistentVolumeClaim", true],
  ["", "v1", "ServiceAccount", true],
  ["", "v1", "Namespace", false],
  ["batch", "v1", "Job", true],
  ["batch", "v1", "CronJob", true],
  ["networking.k8s.io", "v1", "Ingress", true],
  ["networking.k8s.io", "v1", "NetworkPolicy", true],
  ["autoscaling", "v2", "HorizontalPodAutoscaler", true],
  ["policy", "v1", "PodDisruptionBudget", true],
  ["rbac.authorization.k8s.io", "v1", "Role", true],
  ["rbac.authorization.k8s.io", "v1", "RoleBinding", true],
  ["rbac.authorization.k8s.io", "v1", "ClusterRole", false],
  ["rbac.authorization.k8s.io", "v1", "ClusterRoleBinding", false],
  ["example.com", "v1alpha1", "Widget", true],
];

describe("templateFor", () => {
  it.each(kinds)("%s %s %s is valid YAML for its kind", (group, version, kind, namespaced) => {
    const doc = fromYaml(templateFor({ group, version, kind, namespaced }, "shop")) as Parsed;
    expect(doc.apiVersion).toBe(group ? `${group}/${version}` : version);
    expect(doc.kind).toBe(kind);
    expect(doc.metadata.name).toMatch(/^[a-z0-9-]+$/);
    expect(doc.metadata.namespace).toBe(namespaced ? "shop" : undefined);
  });

  it("names workloads my-app and labels them to match their selector", () => {
    const doc = fromYaml(
      templateFor({ group: "apps", version: "v1", kind: "Deployment", namespaced: true }, "shop"),
    ) as Parsed & { spec: { selector: { matchLabels: Record<string, string> } } };
    expect(doc.metadata.name).toBe("my-app");
    expect(doc.metadata.labels).toEqual(doc.spec.selector.matchLabels);
  });

  it("gives any other kind an empty spec", () => {
    const doc = fromYaml(
      templateFor(
        { group: "example.com", version: "v1", kind: "BackupPlan", namespaced: true },
        "",
      ),
    ) as Parsed;
    expect(doc.metadata).toEqual({ name: "my-backup-plan", namespace: "default" });
    expect(doc.spec).toEqual({});
  });
});

describe("helpers", () => {
  it("puts new objects in the picked namespace, else default", () => {
    expect(targetNamespace(["shop"])).toBe("shop");
    expect(targetNamespace(["shop", "pay"])).toBe("shop");
    expect(targetNamespace([])).toBe("default");
  });

  it("summarizes apply results", () => {
    expect(
      summarize([
        { result: "created" },
        { result: "created" },
        { result: "configured" },
        { result: "failed" },
      ]),
    ).toBe("2 created, 1 updated, 1 failed");
  });
});
