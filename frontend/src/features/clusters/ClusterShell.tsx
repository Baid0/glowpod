import { IconRefresh, IconTerminal2, IconX } from "@tabler/icons-react";
import { lazy, Suspense, useState } from "react";

import { Badge, type StatusTone } from "../../components/Badge";
import { Button } from "../../components/Button";
import { api } from "../../lib/api";
import { useSession } from "../../lib/queries";
import type { TerminalStatus } from "../terminal/protocol";
import styles from "./ClusterShell.module.css";

const XTerm = lazy(() => import("../terminal/XTerm"));

const STATUS: Record<TerminalStatus, { tone: StatusTone; word: string }> = {
  connecting: { tone: "neutral", word: "Starting" },
  connected: { tone: "ok", word: "Connected" },
  ended: { tone: "high", word: "Ended" },
  failed: { tone: "critical", word: "Failed" },
};

/**
 * A Hub's shell for the cluster on screen: kubectl, helm and friends in a
 * short-lived pod, acting as you. It lives while this drawer is open.
 */
export function ClusterShell({
  cluster,
  namespace,
  onClose,
}: {
  cluster: string;
  /** Where kubectl starts; "" for the cluster's default. */
  namespace: string;
  onClose: () => void;
}) {
  const { data: session } = useSession();
  const [run, setRun] = useState(0);
  const [status, setStatus] = useState<{ status: TerminalStatus; message?: string }>({
    status: "connecting",
  });
  const s = STATUS[status.status];
  const over = status.status === "ended" || status.status === "failed";

  return (
    <section className={styles.drawer} aria-label={`Shell for ${cluster}`}>
      <div className={styles.head}>
        <IconTerminal2 size={18} stroke={1.75} aria-hidden="true" />
        <h2 className={styles.title}>
          Shell <span className={styles.cluster}>{cluster}</span>
        </h2>
        <span role="status">
          <Badge tone={s.tone}>{s.word}</Badge>
        </span>
        <div className={styles.spacer} />
        {over && (
          <Button
            size="sm"
            icon={<IconRefresh size={16} stroke={1.75} />}
            onClick={() => {
              setStatus({ status: "connecting" });
              setRun((n) => n + 1);
            }}
          >
            New shell
          </Button>
        )}
        <Button
          size="sm"
          icon={<IconX size={16} stroke={1.75} />}
          aria-label="Close the shell"
          onClick={onClose}
        >
          Close
        </Button>
      </div>
      {status.message && <p className={styles.message}>{status.message}</p>}
      {status.status !== "failed" && (
        <Suspense fallback={<div className={styles.screen} aria-label="Loading the terminal" />}>
          <XTerm
            key={run}
            className={styles.screen}
            url={api.shellURL(cluster, namespace)}
            csrf={session?.csrfToken ?? ""}
            label={`Shell for ${cluster}`}
            onStatus={(next, message) => {
              setStatus({ status: next, ...(message ? { message } : {}) });
            }}
          />
        </Suspense>
      )}
    </section>
  );
}
