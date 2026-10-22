import "@xterm/xterm/css/xterm.css";

import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { Terminal } from "@xterm/xterm";
import { useEffect, useRef } from "react";

import { parseExecMessage, type TerminalStatus } from "./protocol";
import { withStyleNonce } from "./styleNonce";
import styles from "./Terminal.module.css";

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/**
 * One shell session: xterm.js on a WebSocket. A shell can't be resumed, so
 * a dropped connection ends the session; the caller offers a new one.
 */
export default function XTerm({
  url,
  csrf,
  label,
  onStatus,
}: {
  url: string;
  csrf: string;
  label: string;
  onStatus: (status: TerminalStatus, message?: string) => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const report = useRef(onStatus);
  useEffect(() => {
    report.current = onStatus;
  });

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: cssVar("--font-mono") || "monospace",
      fontSize: 13,
      lineHeight: 1.25,
      scrollback: 5000,
      theme: {
        background: cssVar("--canvas"),
        foreground: cssVar("--body"),
        cursor: cssVar("--ink"),
        cursorAccent: cssVar("--canvas"),
        selectionBackground: cssVar("--surface-elevated"),
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    withStyleNonce(() => {
      term.open(el);
    });
    // Canvas rendering keeps 24-bit colors within the CSP (the DOM renderer
    // would need inline style attributes for them).
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => {
        webgl.dispose();
      });
      term.loadAddon(webgl);
    } catch {
      // No WebGL: the DOM renderer still works, with 256 colors.
    }
    term.textarea?.setAttribute("aria-label", label);
    fit.fit();

    const encoder = new TextEncoder();
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    let finished = false;
    const finish = (status: TerminalStatus, message?: string) => {
      if (finished) return;
      finished = true;
      report.current(status, message);
    };

    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "start", csrf, cols: term.cols, rows: term.rows }));
    };
    ws.onmessage = (e: MessageEvent<ArrayBuffer | string>) => {
      if (typeof e.data !== "string") {
        term.write(new Uint8Array(e.data));
        return;
      }
      const m = parseExecMessage(e.data);
      if (!m) return;
      if (m.type === "ready") {
        report.current("connected");
        term.focus();
      } else if (m.type === "exit") {
        finish("ended", m.message);
      } else {
        finish("failed", m.message);
      }
    };
    ws.onclose = () => {
      finish("ended", "The connection closed.");
    };

    const input = term.onData((d) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(d));
    });
    const resized = term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols, rows }));
    });
    const observer = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        // detached while resizing
      }
    });
    observer.observe(el);

    return () => {
      finished = true;
      observer.disconnect();
      input.dispose();
      resized.dispose();
      ws.close();
      term.dispose();
    };
  }, [url, csrf, label]);

  return <div ref={host} className={styles.screen} />;
}
