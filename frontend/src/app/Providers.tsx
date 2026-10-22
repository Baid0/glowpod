import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { ToastProvider } from "../components/Toast";
import { LiveProvider } from "../lib/LiveProvider";
import { PreferencesProvider } from "../lib/PreferencesProvider";

export function Providers({ client, children }: { client: QueryClient; children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PreferencesProvider>
        <LiveProvider>
          <ToastProvider>{children}</ToastProvider>
        </LiveProvider>
      </PreferencesProvider>
    </QueryClientProvider>
  );
}
