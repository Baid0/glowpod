import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Outlet, useLocation, useMatches, type Params } from "react-router";

import { ApiError } from "../lib/api";
import { useMediaQuery } from "../lib/media";
import { usePreferences } from "../lib/preferences";
import { useSession } from "../lib/queries";
import { CommandPalette } from "./CommandPalette";
import { DRAWER_QUERY, PanelContext, type PanelState } from "./panel";
import { Rail } from "./Rail";
import { SectionPanel } from "./SectionPanel";
import styles from "./Shell.module.css";
import { TopBar } from "./TopBar";
import { Navigate } from "../lib/clusterLinks";

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

/**
 * Rail + collapsible section panel + top bar around the current page. home
 * is the Clusters page: no cluster yet, so no section panel, namespace
 * picker or search.
 */
export function Shell({ home = false, children }: { home?: boolean; children?: ReactNode }) {
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
  const title = usePageTitle();
  const [paletteOpen, setPaletteOpen] = useState(false);

  useEffect(() => {
    document.title = title === "Glowpod" ? title : `${title} · Glowpod`;
  }, [title]);

  useEffect(() => {
    if (home) return;
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
  }, [home]);

  // The session ended (sign-out elsewhere, expiry, a restart): sign in again.
  const here = location.pathname + location.search;
  if (session.error instanceof ApiError && session.error.status === 401) {
    return <Navigate to={`/sign-in?return=${encodeURIComponent(here)}`} replace />;
  } // A Hub's first start comes before anything else.
  if (session.data?.hub?.setupRequired) return <Navigate to="/setup" replace />;

  const layout = home ? "collapsed" : panel.overlay ? "drawer" : panel.open ? "open" : "collapsed";
  return (
    <PanelContext value={panel}>
      <div className={styles.shell} data-panel={layout}>
        <a href="#main" className={styles.skip}>
          Skip to content
        </a>
        <Rail home={home} />
        {!home &&
          panel.open &&
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
            home={home}
            onSearch={() => {
              setPaletteOpen(true);
            }}
          />
          <main id="main" className={styles.content} tabIndex={-1}>
            {children ?? <Outlet />}
          </main>
        </div>
        {!home && (
          <CommandPalette
            open={paletteOpen}
            onClose={() => {
              setPaletteOpen(false);
            }}
          />
        )}
      </div>
    </PanelContext>
  );
}
