import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { ToastProvider } from "../components/Toast";
import { PreferencesProvider } from "../lib/PreferencesProvider";

export function Providers({ client, children }: { client: QueryClient; children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PreferencesProvider>
        {/* The live stream belongs to one cluster: ClusterScope opens it. */}
        <ToastProvider>{children}</ToastProvider>
      </PreferencesProvider>
    </QueryClientProvider>
  );
}
