import { lazy, Suspense } from "react";
import type { RouteObject } from "react-router";

import { AppPage } from "../features/apps/AppPage";
import { AppsPage, LogsPage } from "../features/apps/AppsPage";
import { OverviewPage } from "../features/overview/OverviewPage";
import { PodPage } from "../features/pod/PodPage";
import { ComingSoon, NotFound } from "../features/placeholder/ComingSoon";
import { GenericDetail, NodeDetail, WorkloadDetail } from "../features/resources/DetailRoutes";
import { GenericList } from "../features/resources/GenericList";
import { ResourceListPage } from "../features/resources/ResourceListPage";
import { ResourceTypesPage } from "../features/resources/ResourceTypesPage";
import { allPages, type NavItem } from "./navigation";
import { Shell, type RouteHandle } from "./Shell";

// Component sheets are for development only and are left out of release builds.
const GaugeDemo = import.meta.env.DEV
  ? lazy(() => import("../features/demo/GaugeDemo").then((m) => ({ default: m.GaugeDemo })))
  : null;

const handle = (title: string): RouteHandle => ({ title });

function pageElement(p: NavItem) {
  if (p.path === "/apps") return <AppsPage />;
  if (p.path === "/logs") return <LogsPage />;
  // key: a fresh page (search, sort) per resource.
  if (p.resource) return <ResourceListPage key={p.path} resource={p.resource} />;
  if (p.types)
    return <ResourceTypesPage key={p.path} {...(p.types === "custom" ? { only: "custom" } : {})} />;
  return <ComingSoon title={p.label} phase={p.phase} />;
}

const pages: RouteObject[] = allPages()
  .filter((p) => p.path !== "/")
  .map((p) => ({ path: p.path.slice(1), element: pageElement(p), handle: handle(p.label) }));

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <Shell />,
    children: [
      { index: true, element: <OverviewPage />, handle: handle("Overview") },
      ...pages,
      {
        path: "workloads/pods/:namespace/:name",
        element: <PodPage />,
        handle: handle("Pod"),
      },
      {
        path: "workloads/:kind/:namespace/:name",
        element: <WorkloadDetail />,
        handle: handle("Details"),
      },
      {
        path: "resources/:group/:version/:resource",
        element: <GenericList />,
        handle: { title: (params) => params.resource ?? "Resources" } satisfies RouteHandle,
      },
      {
        path: "resources/:group/:version/:resource/:namespace/:name",
        element: <GenericDetail />,
        handle: handle("Details"),
      },
      {
        path: "nodes/:name",
        element: <NodeDetail />,
        handle: handle("Node"),
      },
      {
        path: "apps/:namespace/:name",
        element: <AppPage />,
        handle: handle("App"),
      },
      { path: "*", element: <NotFound />, handle: handle("Not found") },
    ],
  },
  ...(GaugeDemo
    ? [
        {
          path: "/dev/gauges",
          element: (
            <Suspense>
              <GaugeDemo />
            </Suspense>
          ),
        },
      ]
    : []),
];
