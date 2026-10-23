import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState } from "react";
import { Navigate, useNavigate } from "react-router";

import { Button } from "../components/Button";
import { RestoreForm } from "../features/hub/Backup";
import { api, ApiError } from "../lib/api";
import { useSession } from "../lib/queries";
import { queryKeys } from "../lib/queryKeys";
import { Logo } from "./Logo";
import styles from "./SignIn.module.css";

const MIN_PASSWORD = 12;

/**
 * A Hub's first start: admin signed in with the bootstrap password, and now
 * picks their own and confirms the address agents will connect to.
 */
export function HubSetupPage() {
  useEffect(() => {
    document.title = "Set up · Glowpod";
  }, []);
  const { data: session, isPending } = useSession();
  if (isPending) return null;
  if (!session?.hub?.setupRequired) return <Navigate to="/" replace />;
  return <SetupForm suggestedURL={session.hub.serverURL ?? window.location.origin} />;
}

function SetupForm({ suggestedURL }: { suggestedURL: string }) {
  const [restoring, setRestoring] = useState(false);
  if (restoring) {
    return (
      <main className={styles.page}>
        <div className={styles.card}>
          <Logo size={40} />
          <h1 className={styles.title}>Restore a backup</h1>
          <p className={styles.text}>
            Make this Hub a copy of the one the backup came from: its accounts, sign-in settings,
            server URL and clusters. Clusters whose agents can reach this Hub reconnect by
            themselves.
          </p>
          <RestoreForm
            onCancel={() => {
              setRestoring(false);
            }}
          />
        </div>
      </main>
    );
  }
  return (
    <NewHubForm
      suggestedURL={suggestedURL}
      onRestore={() => {
        setRestoring(true);
      }}
    />
  );
}

function NewHubForm({ suggestedURL, onRestore }: { suggestedURL: string; onRestore: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [serverURL, setServerURL] = useState(suggestedURL);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const passId = useId();
  const againId = useId();
  const urlId = useId();
  const urlHint = useId();

  const submit = async () => {
    if (password.length < MIN_PASSWORD) {
      setError(`Use at least ${String(MIN_PASSWORD)} characters.`);
      return;
    }
    if (password !== again) {
      setError("The two passwords don't match.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.hubSetup(password, serverURL.trim());
      await client.invalidateQueries({ queryKey: queryKeys.session });
      await navigate("/", { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
      setBusy(false);
    }
  };

  return (
    <main className={styles.page}>
      <div className={styles.card}>
        <Logo size={40} />
        <h1 className={styles.title}>Set up Glowpod Hub</h1>
        <p className={styles.text}>
          Choose a password for <code>admin</code>. The bootstrap password stops working once you
          save.
        </p>
        <form
          className={styles.form}
          aria-label="Set up the Hub"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <label htmlFor={passId} className={styles.label}>
            New password
          </label>
          <input
            id={passId}
            type="password"
            className={styles.input}
            autoComplete="new-password"
            minLength={MIN_PASSWORD}
            required
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
            }}
          />
          <label htmlFor={againId} className={styles.label}>
            The same password again
          </label>
          <input
            id={againId}
            type="password"
            className={styles.input}
            autoComplete="new-password"
            required
            value={again}
            onChange={(e) => {
              setAgain(e.target.value);
            }}
          />
          <label htmlFor={urlId} className={styles.label}>
            Server URL
          </label>
          <input
            id={urlId}
            type="url"
            className={styles.input}
            aria-describedby={urlHint}
            required
            value={serverURL}
            onChange={(e) => {
              setServerURL(e.target.value);
            }}
          />
          <p id={urlHint} className={styles.hint}>
            The address people open and imported clusters connect to, such as
            https://glowpod.example.com.
          </p>
          {error && (
            <p className={styles.error} role="alert">
              {error}
            </p>
          )}
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Saving…" : "Save and continue"}
          </Button>
        </form>
        <p className={styles.text}>
          Moving from another Hub?{" "}
          <button type="button" className={styles.linkButton} onClick={onRestore}>
            Restore a backup instead
          </button>
        </p>
      </div>
    </main>
  );
}
