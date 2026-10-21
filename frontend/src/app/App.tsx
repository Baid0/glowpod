import { createBrowserRouter, RouterProvider } from "react-router";

import { createQueryClient } from "../lib/queries";
import { Providers } from "./Providers";
import { routes } from "./routes";

const router = createBrowserRouter(routes);
const queryClient = createQueryClient();

export function App() {
  return (
    <Providers client={queryClient}>
      <RouterProvider router={router} />
    </Providers>
  );
}
