import { lazy, Suspense } from "react";
import type { RouteObject } from "react-router";

import { OverviewPage } from "../features/overview/OverviewPage";
import { ComingSoon, NotFound } from "../features/placeholder/ComingSoon";
import { allPages } from "./navigation";
import { Shell, type RouteHandle } from "./Shell";

// Component sheets are for development only and are left out of release builds.
const GaugeDemo = import.meta.env.DEV
  ? lazy(() => import("../features/demo/GaugeDemo").then((m) => ({ default: m.GaugeDemo })))
  : null;

const handle = (title: string): RouteHandle => ({ title });

const pages: RouteObject[] = allPages()
  .filter((p) => p.path !== "/")
  .map((p) => ({
    path: p.path.slice(1),
    element: <ComingSoon title={p.label} phase={p.phase} />,
    handle: handle(p.label),
  }));

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <Shell />,
    children: [
      { index: true, element: <OverviewPage />, handle: handle("Overview") },
      ...pages,
      {
        path: "apps/:namespace/:name",
        element: <ComingSoon title="App details" phase={2} />,
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
