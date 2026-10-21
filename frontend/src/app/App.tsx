import { useEffect, useState } from "react";

import styles from "./App.module.css";

type ServerStatus = "checking" | "ok" | "down";

const statusText: Record<ServerStatus, string> = {
  checking: "Checking the Glowpod server…",
  ok: "Connected to the Glowpod server.",
  down: "Can't reach the Glowpod server. Is it running?",
};

// Placeholder until the app shell lands in step 4. It proves the build,
// the embed and the dev proxy all reach the Go server.
export function App() {
  const [status, setStatus] = useState<ServerStatus>("checking");

  useEffect(() => {
    const controller = new AbortController();
    fetch("/healthz", { signal: controller.signal })
      .then((res) => {
        setStatus(res.ok ? "ok" : "down");
      })
      .catch(() => {
        if (!controller.signal.aborted) setStatus("down");
      });
    return () => {
      controller.abort();
    };
  }, []);

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>Glowpod</h1>
      <p role="status" className={styles.status}>
        {statusText[status]}
      </p>
    </main>
  );
}
