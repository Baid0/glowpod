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
  count?: CountKey;
}

export interface Section {
  id: string;
  label: string;
  icon: Icon;
  items: NavItem[];
}

const overviewItem: NavItem = { label: "Overview", path: "/", phase: 1 };
const eventsItem: NavItem = { label: "Events", path: "/events", phase: 4, count: "events" };

export const deepSections: Section[] = [
  {
    id: "cluster",
    label: "Cluster",
    icon: IconLayoutGrid,
    items: [
      overviewItem,
      { label: "Nodes", path: "/nodes", phase: 4, count: "nodes" },
      { label: "Namespaces", path: "/namespaces", phase: 4, count: "namespaces" },
      eventsItem,
    ],
  },
  {
    id: "workloads",
    label: "Workloads",
    icon: IconBox,
    items: [
      { label: "Deployments", path: "/workloads/deployments", phase: 2, count: "deployments" },
      { label: "StatefulSets", path: "/workloads/statefulsets", phase: 2, count: "statefulSets" },
      { label: "DaemonSets", path: "/workloads/daemonsets", phase: 2, count: "daemonSets" },
      { label: "Jobs", path: "/workloads/jobs", phase: 2, count: "jobs" },
      { label: "CronJobs", path: "/workloads/cronjobs", phase: 2 },
      { label: "Pods", path: "/workloads/pods", phase: 2, count: "pods" },
    ],
  },
  {
    id: "network",
    label: "Network",
    icon: IconShare,
    items: [
      { label: "Services", path: "/network/services", phase: 4 },
      { label: "Ingresses", path: "/network/ingresses", phase: 4 },
      { label: "Network policies", path: "/network/networkpolicies", phase: 4 },
    ],
  },
  {
    id: "storage",
    label: "Storage",
    icon: IconDatabase,
    items: [
      { label: "Volume claims", path: "/storage/persistentvolumeclaims", phase: 4 },
      { label: "Volumes", path: "/storage/persistentvolumes", phase: 4 },
      { label: "Storage classes", path: "/storage/storageclasses", phase: 4 },
    ],
  },
  {
    id: "config",
    label: "Config",
    icon: IconAdjustmentsHorizontal,
    items: [
      { label: "ConfigMaps", path: "/config/configmaps", phase: 4 },
      { label: "Secrets", path: "/config/secrets", phase: 4 },
    ],
  },
  {
    id: "extensions",
    label: "Extensions",
    icon: IconPuzzle,
    items: [
      { label: "Custom resources", path: "/extensions/crds", phase: 4 },
      { label: "Helm releases", path: "/extensions/helm", phase: 4 },
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
