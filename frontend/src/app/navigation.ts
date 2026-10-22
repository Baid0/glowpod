import {
  IconActivity,
  IconAdjustmentsHorizontal,
  IconApps,
  IconBox,
  IconDatabase,
  IconFileText,
  IconLayoutGrid,
  IconPuzzle,
  IconSettings,
  IconShare,
  type Icon,
} from "@tabler/icons-react";

import type { Mode } from "../lib/preferences";
import type { ResourceRef } from "../lib/types";

/** Keys for the live counts the panel shows next to an item. */
export type CountKey =
  | "nodes"
  | "namespaces"
  | "events"
  | "deployments"
  | "statefulSets"
  | "daemonSets"
  | "jobs"
  | "pods";

export interface NavItem {
  label: string;
  path: string;
  /** The phase that builds this page; pages not built yet show a placeholder. */
  phase: number;
  /** The resource this page lists; such pages are live lists from day one. */
  resource?: ResourceRef;
  /** Shows the cluster's resource types instead of one list. */
  types?: "custom" | "all";
  count?: CountKey;
}

export interface Section {
  id: string;
  label: string;
  icon: Icon;
  items: NavItem[];
}

const overviewItem: NavItem = { label: "Overview", path: "/", phase: 1 };
const eventsItem: NavItem = {
  label: "Events",
  path: "/events",
  phase: 4,
  resource: { group: "", version: "v1", resource: "events" },
  count: "events",
};

export const deepSections: Section[] = [
  {
    id: "cluster",
    label: "Cluster",
    icon: IconLayoutGrid,
    items: [
      overviewItem,
      {
        label: "Nodes",
        path: "/nodes",
        phase: 4,
        resource: { group: "", version: "v1", resource: "nodes" },
        count: "nodes",
      },
      {
        label: "Namespaces",
        path: "/namespaces",
        phase: 4,
        resource: { group: "", version: "v1", resource: "namespaces" },
        count: "namespaces",
      },
      eventsItem,
    ],
  },
  {
    id: "workloads",
    label: "Workloads",
    icon: IconBox,
    items: [
      {
        label: "Deployments",
        path: "/workloads/deployments",
        phase: 2,
        resource: { group: "apps", version: "v1", resource: "deployments" },
        count: "deployments",
      },
      {
        label: "StatefulSets",
        path: "/workloads/statefulsets",
        phase: 2,
        resource: { group: "apps", version: "v1", resource: "statefulsets" },
        count: "statefulSets",
      },
      {
        label: "DaemonSets",
        path: "/workloads/daemonsets",
        phase: 2,
        resource: { group: "apps", version: "v1", resource: "daemonsets" },
        count: "daemonSets",
      },
      {
        label: "Jobs",
        path: "/workloads/jobs",
        phase: 2,
        resource: { group: "batch", version: "v1", resource: "jobs" },
        count: "jobs",
      },
      {
        label: "CronJobs",
        path: "/workloads/cronjobs",
        phase: 2,
        resource: { group: "batch", version: "v1", resource: "cronjobs" },
      },
      {
        label: "Pods",
        path: "/workloads/pods",
        phase: 2,
        resource: { group: "", version: "v1", resource: "pods" },
        count: "pods",
      },
    ],
  },
  {
    id: "network",
    label: "Network",
    icon: IconShare,
    items: [
      {
        label: "Services",
        path: "/network/services",
        phase: 4,
        resource: { group: "", version: "v1", resource: "services" },
      },
      {
        label: "Ingresses",
        path: "/network/ingresses",
        phase: 4,
        resource: { group: "networking.k8s.io", version: "v1", resource: "ingresses" },
      },
      {
        label: "Network policies",
        path: "/network/networkpolicies",
        phase: 4,
        resource: { group: "networking.k8s.io", version: "v1", resource: "networkpolicies" },
      },
    ],
  },
  {
    id: "storage",
    label: "Storage",
    icon: IconDatabase,
    items: [
      {
        label: "Volume claims",
        path: "/storage/persistentvolumeclaims",
        phase: 4,
        resource: { group: "", version: "v1", resource: "persistentvolumeclaims" },
      },
      {
        label: "Volumes",
        path: "/storage/persistentvolumes",
        phase: 4,
        resource: { group: "", version: "v1", resource: "persistentvolumes" },
      },
      {
        label: "Storage classes",
        path: "/storage/storageclasses",
        phase: 4,
        resource: { group: "storage.k8s.io", version: "v1", resource: "storageclasses" },
      },
    ],
  },
  {
    id: "config",
    label: "Config",
    icon: IconAdjustmentsHorizontal,
    items: [
      {
        label: "ConfigMaps",
        path: "/config/configmaps",
        phase: 4,
        resource: { group: "", version: "v1", resource: "configmaps" },
      },
      {
        label: "Secrets",
        path: "/config/secrets",
        phase: 4,
        resource: { group: "", version: "v1", resource: "secrets" },
      },
    ],
  },
  {
    id: "extensions",
    label: "Extensions",
    icon: IconPuzzle,
    items: [
      { label: "Custom resources", path: "/extensions/crds", phase: 4, types: "custom" },
      { label: "Helm releases", path: "/extensions/helm", phase: 4 },
      { label: "All resource types", path: "/resources", phase: 2, types: "all" },
    ],
  },
];

export const glanceSections: Section[] = [
  { id: "overview", label: "Overview", icon: IconLayoutGrid, items: [overviewItem] },
  {
    id: "apps",
    label: "Apps",
    icon: IconApps,
    items: [{ label: "Apps", path: "/apps", phase: 2 }],
  },
  {
    id: "logs",
    label: "Logs",
    icon: IconFileText,
    items: [{ label: "Logs", path: "/logs", phase: 2 }],
  },
  { id: "events", label: "Events", icon: IconActivity, items: [eventsItem] },
];

export const settingsSection: Section = {
  id: "settings",
  label: "Settings",
  icon: IconSettings,
  items: [{ label: "Settings", path: "/settings", phase: 6 }],
};

export function sectionsFor(mode: Mode): Section[] {
  return mode === "glance" ? glanceSections : deepSections;
}

/** Every page, once, for routing and the command palette. */
export function allPages(): NavItem[] {
  const seen = new Map<string, NavItem>();
  for (const s of [...deepSections, ...glanceSections, settingsSection]) {
    for (const item of s.items) if (!seen.has(item.path)) seen.set(item.path, item);
  }
  return [...seen.values()];
}

function matches(path: string, pathname: string): boolean {
  if (path === "/") return pathname === "/";
  return pathname === path || pathname.startsWith(path + "/");
}

/**
 * The section a URL belongs to in this mode. Pages outside the mode's
 * sections stay reachable (nothing is locked in Glance); then no rail icon
 * is active and the panel falls back to the first section.
 */
export function sectionFor(pathname: string, mode: Mode): Section | undefined {
  if (matches(settingsSection.items[0]?.path ?? "", pathname)) return settingsSection;
  return sectionsFor(mode).find((s) => s.items.some((i) => matches(i.path, pathname)));
}

/** The first page of a section, where its rail icon links to. */
export function sectionHome(s: Section): string {
  return s.items[0]?.path ?? "/";
}

/** The list page for a resource type: its nav page if it has one, else the generic list. */
export function listPathFor(ref: ResourceRef): string {
  const group = ref.group === "core" ? "" : ref.group;
  const item = allPages().find(
    (p) =>
      p.resource?.group === group &&
      p.resource.version === ref.version &&
      p.resource.resource === ref.resource,
  );
  return item?.path ?? `/resources/${group || "core"}/${ref.version}/${ref.resource}`;
}
