import {
  IconKey,
  IconLock,
  IconLogout,
  IconSearch,
  IconSettings,
  IconTerminal2,
} from "@tabler/icons-react";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router";

import { KubeconfigDialog } from "../features/clusters/KubeconfigDialog";
import { PasswordDialog } from "../features/hub/PasswordDialog";
import { api, ApiError } from "../lib/api";
import { initials } from "../lib/format";
import { useLive } from "../lib/live";
import { usePreferences, type Mode } from "../lib/preferences";
import { useOverview, useSession } from "../lib/queries";
import { formatAge, useNow } from "../lib/time";
import type { SignInMethod } from "../lib/types";
import { NamespacePicker } from "./NamespacePicker";
import styles from "./TopBar.module.css";
import { useNavigate } from "../lib/clusterPaths";

/** How old data may get before the live dot goes grey. */
const STALE_MS = 60_000;

/** home: the Clusters page: the title, the mode, search and who you are. */
export function TopBar({
  title,
  home = false,
  onSearch,
  onShell,
}: {
  title: string;
  home?: boolean;
  onSearch: () => void;
  /** On a Hub: opens a shell for the cluster on screen. */
  onShell?: () => void;
}) {
  if (home) {
    return (
      <header className={styles.bar}>
        <div className={styles.left}>
          <h1 className={styles.title}>{title}</h1>
        </div>
        <div className={styles.right}>
          <ModeTabs />
          <span className={styles.divider} aria-hidden="true" />
          <SearchButton onClick={onSearch} />
          <Avatar />
        </div>
      </header>
    );
  }
  return (
    <header className={styles.bar}>
      <div className={styles.left}>
        <h1 className={styles.title}>{title}</h1>
        <Updated />
      </div>
      <div className={styles.right}>
        <NamespacePicker />
        <span className={styles.divider} aria-hidden="true" />
        <ModeTabs />
        <span className={styles.divider} aria-hidden="true" />
        {onShell && (
          <button
            type="button"
            className={styles.search}
            aria-label="Open a shell for this cluster"
            title="kubectl, helm and friends, as you"
            onClick={onShell}
          >
            <IconTerminal2 size={18} stroke={1.75} aria-hidden="true" />
            <span className={styles.kbd}>Shell</span>
          </button>
        )}
        <SearchButton onClick={onSearch} />
        <Avatar />
      </div>
    </header>
  );
}

function SearchButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      className={styles.search}
      aria-label="Search and commands"
      aria-keyshortcuts="Meta+K Control+K"
      onClick={onClick}
    >
      <IconSearch size={18} stroke={1.75} aria-hidden="true" />
      <kbd className={styles.kbd} aria-hidden="true">
        ⌘K
      </kbd>
    </button>
  );
}

function Updated() {
  const { status, lastMessageAt } = useLive();
  const { dataUpdatedAt, isError, error } = useOverview();
  const now = useNow();
  const last = Math.max(dataUpdatedAt, lastMessageAt ?? 0);

  // Never loaded: say why, even if the live stream is talking (it can
  // connect while the cluster refuses every list).
  // Not an error: the Overview asks them to pick a namespace.
  if (isError && error instanceof ApiError && error.code === "namespace_needed") return null;
  if (isError && dataUpdatedAt === 0) {
    return (
      <span className={styles.updated}>
        <span className={styles.dot} data-tone="critical" aria-hidden="true" />
        {error.message}
      </span>
    );
  }
  if (last === 0) return <span className={styles.updated}>Loading…</span>;

  const age = now - last;
  const fresh = status === "live" && age < STALE_MS && !isError;
  return (
    <span className={styles.updated} title={new Date(last).toLocaleString()}>
      <span className={styles.dot} data-tone={fresh ? "live" : "stale"} aria-hidden="true" />
      {status === "reconnecting" ? "Reconnecting… " : ""}updated {formatAge(age)} ago
    </span>
  );
}

const modes: { id: Mode; label: string }[] = [
  { id: "glance", label: "Glance" },
  { id: "deep", label: "Deep" },
];

function ModeTabs() {
  const { mode, setMode } = usePreferences();
  return (
    <div className={styles.modes} role="group" aria-label="View mode">
      {modes.map((m) => (
        <button
          key={m.id}
          type="button"
          className={styles.mode}
          aria-pressed={mode === m.id}
          onClick={() => {
            setMode(m.id);
          }}
        >
          {m.label}
        </button>
      ))}
    </div>
  );
}

const METHOD_WORDS: Record<SignInMethod, string> = {
  kubeconfig: "Using your kubeconfig",
  token: "Signed in with a token",
  oidc: "Signed in with SSO",
  ldap: "Signed in with your directory account",
  password: "Signed in with a Glowpod account",
};

/** Who you are, how you signed in, and Sign out. */
function Avatar() {
  const { data } = useSession();
  const { mode } = usePreferences();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [kubeconfig, setKubeconfig] = useState(false);
  const [password, setPassword] = useState(false);
  const [error, setError] = useState("");
  const box = useRef<HTMLDivElement>(null);
  const { cluster } = useParams();
  const name = data?.user.username ?? "";
  const groups = data?.user.groups ?? [];

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("mousedown", onDown);
    };
  }, [open]);

  const signOut = async () => {
    try {
      await api.signOut();
      client.clear();
      await navigate("/sign-in", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Couldn't sign out.");
    }
  };

  return (
    <div
      ref={box}
      className={styles.user}
      onKeyDown={(e) => {
        if (e.key === "Escape") setOpen(false);
      }}
    >
      <button
        type="button"
        className={styles.avatar}
        aria-label={name ? `Signed in as ${name}` : "Account"}
        aria-expanded={open}
        aria-controls="user-menu"
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        {initials(name)}
      </button>
      {open && (
        <div id="user-menu" className={styles.menu} role="region" aria-label="Account">
          <p className={styles.menuName}>{name || "Signed in"}</p>
          {data && <p className={styles.menuMeta}>{METHOD_WORDS[data.method]}</p>}
          {mode === "deep" && groups.length > 0 && (
            <ul className={styles.menuGroups} aria-label="Groups">
              {groups.slice(0, 8).map((g) => (
                <li key={g}>{g}</li>
              ))}
              {groups.length > 8 && <li>+{groups.length - 8} more</li>}
            </ul>
          )}
          {data?.hub?.role === "admin" && (
            <button
              type="button"
              className={styles.menuButton}
              onClick={() => {
                setOpen(false);
                void navigate("/hub/settings");
              }}
            >
              <IconSettings size={16} stroke={1.75} aria-hidden="true" />
              Hub settings
            </button>
          )}
          {data?.hub && data.method === "password" && (
            <button
              type="button"
              className={styles.menuButton}
              onClick={() => {
                setOpen(false);
                setPassword(true);
              }}
            >
              <IconLock size={16} stroke={1.75} aria-hidden="true" />
              Change password
            </button>
          )}
          {data?.hub && (
            <button
              type="button"
              className={styles.menuButton}
              onClick={() => {
                setOpen(false);
                setKubeconfig(true);
              }}
            >
              <IconKey size={16} stroke={1.75} aria-hidden="true" />
              Kubeconfig
            </button>
          )}
          {data?.mode === "in-cluster" && (
            <button type="button" className={styles.menuButton} onClick={() => void signOut()}>
              <IconLogout size={16} stroke={1.75} aria-hidden="true" />
              Sign out
            </button>
          )}
          {error && (
            <p className={styles.menuMeta} role="alert">
              {error}
            </p>
          )}
        </div>
      )}
      {password && (
        <PasswordDialog
          onClose={() => {
            setPassword(false);
          }}
        />
      )}
      {data?.hub && (
        <KubeconfigDialog
          open={kubeconfig}
          onClose={() => {
            setKubeconfig(false);
          }}
          cluster={cluster}
        />
      )}
    </div>
  );
}
