import { useEffect, useState } from "react";
import { Outlet, useMatches } from "react-router";

import { ApiError } from "../lib/api";
import { usePreferences } from "../lib/preferences";
import { useOverview, useSession } from "../lib/queries";
import { CommandPalette } from "./CommandPalette";
import { Logo } from "./Logo";
import { Rail } from "./Rail";
import { SectionPanel } from "./SectionPanel";
import styles from "./Shell.module.css";
import { TopBar } from "./TopBar";

export interface RouteHandle {
  title: string;
}

function usePageTitle(): string {
  const matches = useMatches();
  for (let i = matches.length - 1; i >= 0; i--) {
    const handle = matches[i]?.handle as RouteHandle | undefined;
    if (handle?.title) return handle.title;
  }
  return "Glowpod";
}

/** Rail + collapsible section panel + top bar around the current page. */
export function Shell() {
  const { panelCollapsed } = usePreferences();
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

  // A 401 on any call means the session ended, e.g. glowpod was restarted.
  const signedOut = [session.error, overview.error].find(
    (e): e is ApiError => e instanceof ApiError && e.status === 401,
  );
  if (signedOut) {
    return <SignInNeeded message={signedOut.message} />;
  }

  return (
    <div className={styles.shell} data-panel={panelCollapsed ? "collapsed" : "open"}>
      <a href="#main" className={styles.skip}>
        Skip to content
      </a>
      <Rail />
      {!panelCollapsed && <SectionPanel />}
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
  );
}

function SignInNeeded({ message }: { message: string }) {
  return (
    <main className={styles.signIn}>
      <Logo size={40} />
      <h1 className={styles.signInTitle}>Sign in to Glowpod</h1>
      <p className={styles.signInText}>{message}</p>
      <p className={styles.signInText}>
        Look for the line starting with <code>http://127.0.0.1</code> where you ran{" "}
        <code>glowpod</code>.
      </p>
    </main>
  );
}
