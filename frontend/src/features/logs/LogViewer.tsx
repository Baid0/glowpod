import { IconArrowDown, IconDownload } from "@tabler/icons-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";

import { Button } from "../../components/Button";
import { api } from "../../lib/api";
import { cx } from "../../lib/cx";
import { usePreferences } from "../../lib/preferences";
import { useClusterId } from "../../lib/queries";
import type { PodContainer } from "../../lib/types";
import { defaultContainer, shortTime, splitMatches } from "./lines";
import styles from "./LogViewer.module.css";
import { MAX_LINES, useLogStream, type LogStatus } from "./useLogStream";

const LINE_HEIGHT = 20;

const STATUS_WORDS: Record<LogStatus, string> = {
  connecting: "Connecting…",
  streaming: "Streaming",
  reconnecting: "Reconnecting…",
  ended: "Ended",
  error: "Stopped",
};

/**
 * Live tail of one container: follow, search, container picker. Deep adds
 * the previous run, timestamps and download. The container and previous
 * run live in the URL so other pages can link straight to a crash's log.
 */
export function LogViewer({
  namespace,
  pod,
  containers,
}: {
  namespace: string;
  pod: string;
  containers: PodContainer[];
}) {
  const { mode } = usePreferences();
  const cluster = useClusterId();
  const [params, setParams] = useSearchParams();
  const container = params.get("container") ?? defaultContainer(containers);
  const previous = params.get("previous") === "1";
  const current = containers.find((c) => c.name === container);
  const [search, setSearch] = useState("");
  const [timestamps, setTimestamps] = useState(false);
  const [follow, setFollow] = useState(true);

  const { lines, status, message, dropped } = useLogStream(namespace, pod, { container, previous });
  const shown = useMemo(() => {
    if (!search) return lines;
    const q = search.toLowerCase();
    return lines.filter((l) => l.x.toLowerCase().includes(q));
  }, [lines, search]);

  const setParam = (name: string, value: string | null) => {
    const next = new URLSearchParams(params);
    if (value === null) next.delete(name);
    else next.set(name, value);
    setParams(next, { replace: true });
  };

  const scroller = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: shown.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => LINE_HEIGHT,
    overscan: 20,
    initialRect: { width: 1200, height: 600 },
  });

  // Follow: keep the newest line in view while following.
  useEffect(() => {
    if (follow && shown.length > 0) virtualizer.scrollToIndex(shown.length - 1, { align: "end" });
  }, [follow, shown.length, virtualizer]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < LINE_HEIGHT * 2;
    if (atBottom !== follow) setFollow(atBottom);
  };

  const items = virtualizer.getVirtualItems();
  const canPrevious = (current?.restarts ?? 0) > 0;

  return (
    <section className={styles.viewer} aria-label="Logs">
      <div className={styles.toolbar}>
        {containers.length > 1 && (
          <label className={styles.field}>
            <span className={styles.fieldLabel}>Container</span>
            <select
              className={styles.select}
              value={container}
              onChange={(e) => {
                const next = new URLSearchParams(params);
                next.set("container", e.target.value);
                next.delete("previous");
                setParams(next, { replace: true });
              }}
            >
              {containers.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                  {c.role !== "app" ? ` (${c.role})` : ""}
                </option>
              ))}
            </select>
          </label>
        )}
        <input
          type="search"
          className={styles.search}
          aria-label="Search the log"
          placeholder="Search the log…"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
          }}
        />
        <Button
          size="sm"
          variant={follow ? "primary" : "secondary"}
          aria-pressed={follow}
          onClick={() => {
            setFollow(!follow);
          }}
        >
          Follow
        </Button>
        {mode === "deep" && (
          <>
            <label className={cx(styles.check, !canPrevious && !previous && styles.disabled)}>
              <input
                type="checkbox"
                checked={previous}
                disabled={!canPrevious && !previous}
                onChange={(e) => {
                  setParam("previous", e.target.checked ? "1" : null);
                }}
              />
              Previous run
            </label>
            <label className={styles.check}>
              <input
                type="checkbox"
                checked={timestamps}
                onChange={(e) => {
                  setTimestamps(e.target.checked);
                }}
              />
              Timestamps
            </label>
            <a
              className={styles.download}
              href={api.logsDownloadURL(cluster, namespace, pod, { container, previous })}
              download
            >
              <IconDownload size={16} stroke={1.75} aria-hidden="true" />
              Download
            </a>
          </>
        )}
        <span className={styles.status} data-status={status} role="status">
          <span className={styles.statusDot} aria-hidden="true" />
          {STATUS_WORDS[status]}
          {previous && status !== "error" ? " · previous run" : ""}
          {search && ` · ${shown.length} of ${lines.length} lines`}
        </span>
      </div>

      {mode === "glance" && canPrevious && !previous && (
        <p className={styles.hint}>
          {container} has restarted {current?.restarts} {current?.restarts === 1 ? "time" : "times"}
          . The previous run's log usually shows why.{" "}
          <button
            type="button"
            className={styles.linkButton}
            onClick={() => {
              setParam("previous", "1");
            }}
          >
            Show the previous run
          </button>
        </p>
      )}
      {message && status !== "streaming" && <p className={styles.message}>{message}</p>}
      {dropped > 0 && (
        <p className={styles.note}>Showing the last {MAX_LINES.toLocaleString("en")} lines.</p>
      )}

      <div
        ref={scroller}
        className={styles.scroller}
        onScroll={onScroll}
        tabIndex={0}
        aria-label="Log lines"
      >
        {shown.length === 0 ? (
          <p className={styles.empty}>
            {status === "connecting"
              ? "Connecting to the log…"
              : search
                ? `No lines match “${search}”.`
                : "No log lines yet."}
          </p>
        ) : (
          <div className={styles.inner} style={{ height: virtualizer.getTotalSize() }}>
            {items.map((item) => {
              const line = shown[item.index];
              if (!line) return null;
              return (
                <div
                  key={line.id}
                  className={styles.line}
                  data-level={line.level}
                  style={{ transform: `translateY(${String(item.start)}px)` }}
                >
                  {timestamps && line.t && (
                    <span className={styles.time} title={line.t}>
                      {shortTime(line.t)}
                    </span>
                  )}
                  <span className={styles.text}>
                    {splitMatches(line.x, search).map((part, i) =>
                      part.match ? (
                        <mark key={i}>{part.text}</mark>
                      ) : (
                        <span key={i}>{part.text}</span>
                      ),
                    )}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {!follow && shown.length > 0 && (
        <Button
          size="sm"
          className={styles.jump}
          icon={<IconArrowDown size={16} stroke={1.75} />}
          onClick={() => {
            setFollow(true);
          }}
        >
          Jump to latest
        </Button>
      )}
    </section>
  );
}
