import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { api, ApiError } from "../../lib/api";
import access from "../access/Access.module.css";

/** Change your own Glowpod account's password. */
export function PasswordDialog({ onClose }: { onClose: () => void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const curId = useId();
  const newId = useId();
  const againId = useId();

  const submit = async () => {
    if (next !== again) {
      setError("The two new passwords don't match.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.changePassword(current, next);
      setDone(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title="Change your password"
      onSubmit={() => {
        if (done) onClose();
        else void submit();
      }}
      footer={
        done ? (
          <Button type="submit" variant="primary">
            Done
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? "Saving…" : "Change password"}
            </Button>
          </>
        )
      }
    >
      {done ? (
        <p className={access.intro} role="status">
          Your password is changed. Use it next time you sign in.
        </p>
      ) : (
        <div className={access.form}>
          <div className={access.field}>
            <label htmlFor={curId} className={access.label}>
              Current password
            </label>
            <input
              id={curId}
              type="password"
              className={access.input}
              autoComplete="current-password"
              required
              value={current}
              onChange={(e) => {
                setCurrent(e.target.value);
              }}
            />
          </div>
          <div className={access.field}>
            <label htmlFor={newId} className={access.label}>
              New password
            </label>
            <input
              id={newId}
              type="password"
              className={access.input}
              autoComplete="new-password"
              minLength={12}
              required
              value={next}
              onChange={(e) => {
                setNext(e.target.value);
              }}
            />
            <p className={access.hint}>At least 12 characters.</p>
          </div>
          <div className={access.field}>
            <label htmlFor={againId} className={access.label}>
              The new password again
            </label>
            <input
              id={againId}
              type="password"
              className={access.input}
              autoComplete="new-password"
              required
              value={again}
              onChange={(e) => {
                setAgain(e.target.value);
              }}
            />
          </div>
          {error && (
            <p className={access.error} role="alert">
              {error}
            </p>
          )}
        </div>
      )}
    </Dialog>
  );
}
