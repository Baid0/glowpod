import { IconDownload } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { api, ApiError } from "../../lib/api";
import { saveFile } from "../../lib/download";
import { queryKeys } from "../../lib/queryKeys";
import { formatAge, useNow } from "../../lib/time";
import type { KubeToken } from "../../lib/types";
import { Command } from "./HubDialogs";
import styles from "./HubDialogs.module.css";

const FILE = "glowpod-kubeconfig.yaml";

/**
 * kubectl through the Hub: download a kubeconfig whose token acts as you,
 * and revoke the ones you no longer need.
 */
export function KubeconfigDialog({
  open,
  onClose,
  cluster,
}: {
  open: boolean;
  onClose: () => void;
  /** The cluster on screen: kubectl starts in its context. */
  cluster?: string;
}) {
  const client = useQueryClient();
  const tokens = useQuery({
    queryKey: queryKeys.kubeconfigTokens,
    queryFn: ({ signal }) => api.kubeconfigTokens(signal),
    enabled: open,
  });
  const download = useMutation({
    mutationFn: () => api.kubeconfig(cluster),
    onSuccess: ({ yaml }) => {
      saveFile(yaml, FILE, "application/yaml");
      void client.invalidateQueries({ queryKey: queryKeys.kubeconfigTokens });
    },
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeKubeconfigToken(id),
    onSettled: () => void client.invalidateQueries({ queryKey: queryKeys.kubeconfigTokens }),
  });
  const hours = Math.round((tokens.data?.ttlSeconds ?? 8 * 3600) / 3600);
  const close = () => {
    download.reset();
    revoke.reset();
    onClose();
  };
  const error = download.error ?? revoke.error ?? tokens.error;

  return (
    <Dialog
      open={open}
      onClose={close}
      size="wide"
      title="Use kubectl"
      description={
        <p>
          Download a kubeconfig with a context for each cluster you can see. kubectl then goes
          through the Hub as you, with your own access, for {hours} hours.
        </p>
      }
      footer={
        <>
          <Button onClick={close}>Close</Button>
          <Button
            variant="primary"
            icon={<IconDownload size={16} stroke={1.75} />}
            disabled={download.isPending}
            onClick={() => {
              download.mutate();
            }}
          >
            {download.isPending ? "Making it…" : "Download kubeconfig"}
          </Button>
        </>
      }
    >
      <div className={styles.stack}>
        {download.isSuccess && (
          <>
            <p className={styles.hint} aria-live="polite">
              Saved {FILE}. Try it:
            </p>
            <Command
              text={`kubectl --kubeconfig ~/Downloads/${FILE} get pods${cluster ? ` --context ${cluster}` : ""}`}
              label="Try the kubeconfig"
            />
          </>
        )}
        <TokenList
          tokens={tokens.data?.tokens}
          revoking={revoke.isPending ? revoke.variables : undefined}
          onRevoke={(id) => {
            revoke.mutate(id);
          }}
        />
        {error && (
          <p className={styles.error} role="alert">
            {error instanceof ApiError ? error.message : "Something went wrong."}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function TokenList({
  tokens,
  revoking,
  onRevoke,
}: {
  tokens?: KubeToken[];
  revoking?: string;
  onRevoke: (id: string) => void;
}) {
  const now = useNow(30_000);
  if (!tokens) return null;
  if (tokens.length === 0) {
    return <p className={styles.hint}>You have no kubeconfigs that still work.</p>;
  }
  return (
    <section aria-label="Your kubeconfigs">
      <p className={styles.label}>Your kubeconfigs</p>
      <ul className={styles.tokens}>
        {[...tokens].reverse().map((t) => (
          <li key={t.id} className={styles.token}>
            <span>
              Made {formatAge(now - Date.parse(t.created))} ago · works{" "}
              {formatAge(Date.parse(t.expires) - now)} more ·{" "}
              {t.lastUsed ? `used ${formatAge(now - Date.parse(t.lastUsed))} ago` : "not used yet"}
            </span>
            <Button
              size="sm"
              disabled={revoking === t.id}
              aria-label={`Revoke the kubeconfig made ${formatAge(now - Date.parse(t.created))} ago`}
              onClick={() => {
                onRevoke(t.id);
              }}
            >
              Revoke
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
