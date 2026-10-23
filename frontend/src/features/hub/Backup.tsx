import { IconDownload, IconUpload } from "@tabler/icons-react";
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { api, ApiError } from "../../lib/api";
import { useNavigate } from "../../lib/clusterPaths";
import { saveFile } from "../../lib/download";
import access from "../access/Access.module.css";
import styles from "./HubSettings.module.css";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** Hub settings: download a backup, or restore one. */
export function BackupCard() {
  const [dialog, setDialog] = useState<"backup" | "restore" | undefined>();
  const close = () => {
    setDialog(undefined);
  };
  return (
    <Card title="Backup and restore">
      <p className={access.intro}>
        A backup holds Glowpod accounts, sign-in settings, the server URL and the imported clusters,
        encrypted with a passphrase you choose. It doesn&apos;t hold sign-ins, kubeconfigs or the
        HTTPS certificate. Restore it here, or on a new Hub at first start to move there.
      </p>
      <div className={styles.buttons}>
        <Button
          icon={<IconDownload size={16} stroke={1.75} />}
          onClick={() => {
            setDialog("backup");
          }}
        >
          Download backup
        </Button>
        <Button
          icon={<IconUpload size={16} stroke={1.75} />}
          onClick={() => {
            setDialog("restore");
          }}
        >
          Restore a backup
        </Button>
      </div>
      {dialog === "backup" && <BackupDialog onClose={close} />}
      {dialog === "restore" && <RestoreDialog onClose={close} />}
    </Card>
  );
}

function BackupDialog({ onClose }: { onClose: () => void }) {
  const [passphrase, setPassphrase] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const passId = useId();
  const againId = useId();

  const submit = async () => {
    if (passphrase !== again) {
      setError("The two passphrases don't match.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const { text, name } = await api.hubBackup(passphrase);
      saveFile(text, name, "application/json");
      setSaved(name);
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title="Download a backup"
      onSubmit={() => {
        if (saved) onClose();
        else void submit();
      }}
      footer={
        saved ? (
          <Button type="submit" variant="primary">
            Done
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? "Making it…" : "Download backup"}
            </Button>
          </>
        )
      }
    >
      {saved ? (
        <p className={access.intro} role="status">
          Saved {saved}. Keep it, and the passphrase, somewhere safe and apart: anyone with both can
          read the sign-in secrets in it.
        </p>
      ) : (
        <div className={access.form}>
          <div className={access.field}>
            <label htmlFor={passId} className={access.label}>
              Passphrase
            </label>
            <input
              id={passId}
              type="password"
              className={access.input}
              autoComplete="new-password"
              minLength={12}
              required
              value={passphrase}
              onChange={(e) => {
                setPassphrase(e.target.value);
              }}
            />
            <p className={access.hint}>
              At least 12 characters. Without it the backup can&apos;t be restored; Glowpod
              doesn&apos;t keep it.
            </p>
          </div>
          <div className={access.field}>
            <label htmlFor={againId} className={access.label}>
              The same passphrase again
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

/** A restore's fields, state and submit, for a dialog or a page. */
function useRestore(confirm: boolean) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [file, setFile] = useState<File | undefined>();
  const [passphrase, setPassphrase] = useState("");
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const ready = file !== undefined && passphrase !== "" && (!confirm || typed === "restore");

  const submit = async () => {
    if (!file || !ready) return;
    setBusy(true);
    setError("");
    try {
      const res = await api.hubRestore(passphrase, await file.text());
      // Everyone was signed out: sign in again with the backup's accounts.
      client.clear();
      await navigate(`/sign-in?restored=${String(res.users)}-${String(res.clusters)}`, {
        replace: true,
      });
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  };
  return { file, setFile, passphrase, setPassphrase, typed, setTyped, busy, error, ready, submit };
}

function RestoreFields({ r, confirm }: { r: ReturnType<typeof useRestore>; confirm: boolean }) {
  const fileId = useId();
  const passId = useId();
  const typedId = useId();
  return (
    <div className={access.form}>
      <div className={access.field}>
        <label htmlFor={fileId} className={access.label}>
          Backup file
        </label>
        <input
          id={fileId}
          type="file"
          accept="application/json,.json"
          onChange={(e) => {
            r.setFile(e.target.files?.[0]);
          }}
        />
      </div>
      <div className={access.field}>
        <label htmlFor={passId} className={access.label}>
          Passphrase
        </label>
        <input
          id={passId}
          type="password"
          className={access.input}
          autoComplete="off"
          value={r.passphrase}
          onChange={(e) => {
            r.setPassphrase(e.target.value);
          }}
        />
      </div>
      {confirm && (
        <div className={access.field}>
          <label htmlFor={typedId} className={access.label}>
            Type <span className={access.mono}>restore</span> to confirm
          </label>
          <input
            id={typedId}
            className={access.input}
            autoComplete="off"
            spellCheck={false}
            value={r.typed}
            onChange={(e) => {
              r.setTyped(e.target.value);
            }}
          />
        </div>
      )}
      {r.error && (
        <p className={access.error} role="alert">
          {r.error}
        </p>
      )}
    </div>
  );
}

/** Restore on a Hub in use: asks to type "restore" first. */
function RestoreDialog({ onClose }: { onClose: () => void }) {
  const r = useRestore(true);
  return (
    <Dialog
      open
      onClose={onClose}
      title="Restore a backup"
      description={
        <p>
          The Hub becomes what the backup holds: its accounts, sign-in settings and clusters, and no
          others. Everyone is signed out, you too, and kubeconfigs stop working.
        </p>
      }
      onSubmit={() => void r.submit()}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={r.busy || !r.ready}>
            {r.busy ? "Restoring…" : "Restore"}
          </Button>
        </>
      }
    >
      <RestoreFields r={r} confirm />
    </Dialog>
  );
}

/** Restore at a new Hub's first start. */
export function RestoreForm({ onCancel }: { onCancel?: () => void }) {
  const r = useRestore(false);
  return (
    <form
      aria-label="Restore a backup"
      onSubmit={(e) => {
        e.preventDefault();
        void r.submit();
      }}
    >
      <RestoreFields r={r} confirm={false} />
      <div className={styles.buttons}>
        {onCancel && <Button onClick={onCancel}>Cancel</Button>}
        <Button type="submit" variant="primary" disabled={r.busy || !r.ready}>
          {r.busy ? "Restoring…" : "Restore"}
        </Button>
      </div>
    </form>
  );
}
