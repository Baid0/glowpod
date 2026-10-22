// The entry point of the website demo (glowpod.dev/demo/): the real app, answered by an
// in-memory cluster instead of the Go server. Only `vite build --mode demo` uses it.
import "@fontsource-variable/inter";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "../styles/tokens.css";
import "../styles/base.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router";

import { Providers } from "../app/Providers";
import { routes } from "../app/routes";
import { createQueryClient } from "../lib/queries";
import { DemoBanner } from "./DemoBanner";
import { installDemo } from "./install";

installDemo();

const router = createBrowserRouter(routes, {
  basename: import.meta.env.BASE_URL.replace(/\/$/, ""),
});
const queryClient = createQueryClient();

const root = document.getElementById("root");
if (!root) {
  throw new Error("Glowpod: #root element is missing from index.html");
}

createRoot(root).render(
  <StrictMode>
    <DemoBanner />
    <Providers client={queryClient}>
      <RouterProvider router={router} />
    </Providers>
  </StrictMode>,
);
