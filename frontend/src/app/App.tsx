import { lazy, Suspense } from "react";
import { createBrowserRouter, RouterProvider, type RouteObject } from "react-router";

import { Home } from "./Home";

// Component sheets are for development only and are left out of release builds.
const GaugeDemo = import.meta.env.DEV
  ? lazy(() => import("../features/demo/GaugeDemo").then((m) => ({ default: m.GaugeDemo })))
  : null;

const routes: RouteObject[] = [
  { path: "/", element: <Home /> },
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

const router = createBrowserRouter(routes);

export function App() {
  return <RouterProvider router={router} />;
}
