import { IconPlayerPlay, IconRefresh } from "@tabler/icons-react";
import { lazy, Suspense, useState } from "react";

import { Badge } from "../../components/Badge";
import type { StatusTone } from "../../components/Badge";
import { Button } from "../../components/Button";
import { api } from "../../lib/api";
import { useClusterId, useSession } from "../../lib/queries";
import type { PodContainer } from "../../lib/types";
import type { TerminalStatus } from "./protocol";
import styles from "./Terminal.module.css";

const XTerm = lazy(() => import("./XTerm"));

const STATUS: Record<TerminalStatus, { tone: StatusTone; word: string }> = {
  connecting: { tone: "neutral", word: "Connecting" },
  connected: { tone: "ok", word: "Connected" },
  ended: { tone: "high", word: "Ended" },
  failed: { tone: "critical", word: "Failed" },
};

/**
 * A shell in one of the pod's running containers. It starts on a click, not
 * on page load: opening a shell is an action, and the cluster records it.
 */
export function TerminalPanel({
  namespace,
  pod,
  containers,
}: {
  namespace: string;
  pod: string;
  containers: PodContainer[];
}) {
  const cluster = useClusterId();
  const { data: session } = useSession();
  const running = containers.filter((c) => c.state === "running" && c.role !== "init");
  const [container, setContainer] = useState(
    running.find((c) => c.role === "app")?.name ?? running[0]?.name ?? "",
  );
  const [run, setRun] = useState(0);
  const [status, setStatus] = useState<{ status: TerminalStatus; message?: string } | null>(null);

  if (running.length === 0) {
    return (
      <p className={styles.empty}>
        No container is running, so there's nothing to open a shell in.
      </p>
    );
  }

  const start = () => {
    setStatus({ status: "connecting" });
    setRun((n) => n + 1);
  };
  const s = status ? STATUS[status.status] : null;

  return (
    <div className={styles.panel}>
      <div className={styles.toolbar}>
        <label className={styles.field}>
          <span>Container</span>
          <select
            className={styles.select}
            value={container}
            disabled={status?.status === "connecting" || status?.status === "connected"}
            onChange={(e) => {
              setContainer(e.target.value);
              setStatus(null);
            }}
          >
            {running.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
        {s && (
          <span role="status">
            <Badge tone={s.tone}>{s.word}</Badge>
          </span>
        )}
        <div className={styles.spacer} />
        {status === null ? (
          <Button
            size="sm"
            variant="primary"
            icon={<IconPlayerPlay size={16} stroke={1.75} />}
            onClick={start}
          >
            Open shell
          </Button>
        ) : status.status === "ended" || status.status === "failed" ? (
          <Button size="sm" icon={<IconRefresh size={16} stroke={1.75} />} onClick={start}>
            New session
          </Button>
        ) : (
          <Button
            size="sm"
            onClick={() => {
              setStatus(null);
            }}
          >
            Disconnect
          </Button>
        )}
      </div>

      {status?.message && <p className={styles.message}>{status.message}</p>}

      {status === null ? (
        <p className={styles.hint}>
          Opens a shell in {container}. You act as the container's own user, and changes to its
          files are lost when the pod is replaced.
        </p>
      ) : (
        status.status !== "failed" && (
          <Suspense fallback={<div className={styles.screen} aria-label="Loading the terminal" />}>
            <XTerm
              key={run}
              url={api.execURL(cluster, namespace, pod, container)}
              csrf={session?.csrfToken ?? ""}
              label={`Shell in ${container}`}
              onStatus={(next, message) => {
                setStatus({ status: next, ...(message ? { message } : {}) });
              }}
            />
          </Suspense>
        )
      )}
    </div>
  );
}
