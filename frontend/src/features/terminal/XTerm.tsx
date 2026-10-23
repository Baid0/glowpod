import "@xterm/xterm/css/xterm.css";

import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { Terminal } from "@xterm/xterm";
import { useEffect, useRef } from "react";

import { usePreferences } from "../../lib/preferences";
import { parseExecMessage, type TerminalStatus } from "./protocol";
import { withStyleNonce } from "./styleNonce";
import styles from "./Terminal.module.css";

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** Terminal colors from the design tokens of the theme in effect. */
function termTheme() {
  return {
    background: cssVar("--canvas"),
    foreground: cssVar("--body"),
    cursor: cssVar("--ink"),
    cursorAccent: cssVar("--canvas"),
    selectionBackground: cssVar("--surface-elevated"),
  };
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
  className,
}: {
  url: string;
  csrf: string;
  label: string;
  onStatus: (status: TerminalStatus, message?: string) => void;
  /** Sizes the screen instead of the pod page's default height. */
  className?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const terminal = useRef<Terminal | null>(null);
  const { resolvedTheme } = usePreferences();
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
      theme: termTheme(),
    });
    terminal.current = term;
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
      terminal.current = null;
    };
  }, [url, csrf, label]);

  // Recolor in place when the theme changes; the shell keeps running. The
  // page's tokens switch in the same commit, so read them a frame later.
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      if (terminal.current) terminal.current.options.theme = termTheme();
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [resolvedTheme]);

  return <div ref={host} className={className ?? styles.screen} />;
}
