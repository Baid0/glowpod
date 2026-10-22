import { useEffect, useMemo, useState } from "react";
import { Navigate, Outlet, useLocation, useMatches, type Params } from "react-router";

import { ApiError } from "../lib/api";
import { useMediaQuery } from "../lib/media";
import { usePreferences } from "../lib/preferences";
import { useOverview, useSession } from "../lib/queries";
import { CommandPalette } from "./CommandPalette";
import { DRAWER_QUERY, PanelContext, type PanelState } from "./panel";
import { Rail } from "./Rail";
import { SectionPanel } from "./SectionPanel";
import styles from "./Shell.module.css";
import { TopBar } from "./TopBar";

export interface RouteHandle {
  /** A fixed title, or one built from the route's params. */
  title: string | ((params: Params) => string);
}

function usePageTitle(): string {
  const matches = useMatches();
  for (let i = matches.length - 1; i >= 0; i--) {
    const match = matches[i];
    const handle = match?.handle as RouteHandle | undefined;
    if (handle?.title)
      return typeof handle.title === "function" ? handle.title(match?.params ?? {}) : handle.title;
  }
  return "Glowpod";
}

/** Rail + collapsible section panel + top bar around the current page. */
export function Shell() {
  const location = useLocation();
  const { panelCollapsed, setPanelCollapsed } = usePreferences();
  const narrow = useMediaQuery(DRAWER_QUERY);
  const [drawer, setDrawer] = useState(false);
  const panel = useMemo<PanelState>(
    () =>
      narrow
        ? { open: drawer, setOpen: setDrawer, overlay: true }
        : {
            open: !panelCollapsed,
            setOpen: (o) => {
              setPanelCollapsed(!o);
            },
            overlay: false,
          },
    [narrow, drawer, panelCollapsed, setPanelCollapsed],
  );
  const session = useSession();
  const overview = useOverview();
  const title = usePageTitle();
  const [paletteOpen, setPaletteOpen] = useState(false);

  useEffect(() => {
    document.title = title === "Glowpod" ? title : `${title} · Glowpod`;
  }, [title]);

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  // The session ended (sign-out elsewhere, expiry, a restart): sign in again.
  const here = location.pathname + location.search;
  if (session.error instanceof ApiError && session.error.status === 401) {
    return <Navigate to={`/sign-in?return=${encodeURIComponent(here)}`} replace />;
  }
  // In the cluster, the cluster itself refusing the user's token means it
  // expired or was revoked. (Locally it means a bad kubeconfig; the page says so.)
  if (
    session.data?.mode === "in-cluster" &&
    overview.error instanceof ApiError &&
    overview.error.code === "cluster_unauthorized"
  ) {
    return (
      <Navigate to={`/sign-in?error=cluster_rejected&return=${encodeURIComponent(here)}`} replace />
    );
  }

  const layout = panel.overlay ? "drawer" : panel.open ? "open" : "collapsed";
  return (
    <PanelContext value={panel}>
      <div className={styles.shell} data-panel={layout}>
        <a href="#main" className={styles.skip}>
          Skip to content
        </a>
        <Rail />
        {panel.open &&
          (panel.overlay ? (
            <div
              className={styles.drawer}
              onKeyDown={(e) => {
                if (e.key === "Escape") setDrawer(false);
              }}
            >
              <SectionPanel />
              <button
                type="button"
                className={styles.scrim}
                aria-label="Close panel"
                onClick={() => {
                  setDrawer(false);
                }}
              />
            </div>
          ) : (
            <SectionPanel />
          ))}
        <div className={styles.main}>
          <TopBar
            title={title}
            onSearch={() => {
              setPaletteOpen(true);
            }}
          />
          <main id="main" className={styles.content} tabIndex={-1}>
            <Outlet />
          </main>
        </div>
        <CommandPalette
          open={paletteOpen}
          onClose={() => {
            setPaletteOpen(false);
          }}
        />
      </div>
    </PanelContext>
  );
}
