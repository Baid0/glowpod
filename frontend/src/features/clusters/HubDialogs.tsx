import { IconCheck, IconCopy } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState } from "react";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { api, ApiError } from "../../lib/api";
import { useClusters } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { ClusterInfo, ImportCommand } from "../../lib/types";
import styles from "./HubDialogs.module.css";

/** While the import dialog waits for the agent, the list refreshes this often. */
const WAIT_MS = 2_000;

/**
 * Import a cluster, as in Rancher: name it, run one kubectl command there,
 * and watch the agent connect. Given a cluster, shows a fresh command for it.
 */
export function ImportDialog({
  open,
  onClose,
  cluster,
}: {
  open: boolean;
  onClose: () => void;
  /** An imported cluster whose agent hasn't connected: a new command for it. */
  cluster?: ClusterInfo;
}) {
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [owner, setOwner] = useState(true);
  const nameId = useId();
  const ownerHint = useId();
  const create = useMutation({
    mutationFn: () =>
      cluster ? api.reimportCluster(cluster.id, owner) : api.importCluster(name.trim(), owner),
    onSuccess: () => void client.invalidateQueries({ queryKey: queryKeys.clusters }),
  });
  const result = create.data;
  const close = () => {
    setName("");
    setOwner(true);
    create.reset();
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      size="wide"
      title={cluster ? `Connect ${cluster.name}` : "Import a cluster"}
      description={
        result ? (
          <p>
            Run this where <code>kubectl</code> reaches the cluster, as someone who can create
            ClusterRoles there. The link works for one hour.
          </p>
        ) : (
          <p>
            Glowpod installs a small agent in the cluster. It connects out to this Hub, so the
            cluster needs no inbound access, and it can only act as the people who sign in here.
          </p>
        )
      }
      {...(result
        ? {}
        : {
            onSubmit: () => {
              create.mutate();
            },
          })}
      footer={
        result ? (
          <Button variant="primary" onClick={close}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button
              type="submit"
              variant="primary"
              disabled={(!cluster && !name.trim()) || create.isPending}
            >
              {create.isPending ? "Preparing…" : cluster ? "Show the command" : "Import"}
            </Button>
          </>
        )
      }
    >
      {result ? (
        <ImportSteps result={result} />
      ) : (
        <div className={styles.stack}>
          {!cluster && (
            <div className={styles.field}>
              <label htmlFor={nameId} className={styles.label}>
                Name
              </label>
              <input
                id={nameId}
                className={styles.input}
                autoComplete="off"
                autoFocus
                maxLength={63}
                placeholder="prod-eu"
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                }}
              />
            </div>
          )}
          <label className={styles.check}>
            <input
              type="checkbox"
              checked={owner}
              aria-describedby={ownerHint}
              onChange={(e) => {
                setOwner(e.target.checked);
              }}
            />
            <span>
              Make me an owner of this cluster
              <span id={ownerHint} className={styles.hint}>
                {" "}
                (a cluster-admin binding for you, created by the command you run)
              </span>
            </span>
          </label>
          {create.error && (
            <p className={styles.error} role="alert">
              {create.error instanceof ApiError ? create.error.message : "Something went wrong."}
            </p>
          )}
        </div>
      )}
    </Dialog>
  );
}

function ImportSteps({ result }: { result: ImportCommand }) {
  const { data: clusters } = useClusters({ refetchInterval: WAIT_MS });
  const live = clusters?.find((c) => c.id === result.cluster.id) ?? result.cluster;
  return (
    <div className={styles.stack}>
      <Command text={result.command} label="Install command" />
      {result.insecureCommand && (
        <>
          <p className={styles.hint}>
            If the Hub's certificate isn't trusted where you run kubectl:
          </p>
          <Command
            text={result.insecureCommand}
            label="Install command without checking the certificate"
          />
        </>
      )}
      <p className={styles.waiting} aria-live="polite">
        {live.state === "connected" ? (
          <>
            <Badge tone="ok">Connected</Badge> {live.name} is ready.
          </>
        ) : (
          <>
            <Badge tone="neutral">Waiting for agent</Badge> This updates by itself when the agent
            connects.
          </>
        )}
      </p>
    </div>
  );
}

export function Command({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => {
      setCopied(false);
    }, 1500);
    return () => {
      clearTimeout(t);
    };
  }, [copied]);
  return (
    <div className={styles.command}>
      <code aria-label={label}>{text}</code>
      <Button
        size="sm"
        icon={copied ? <IconCheck size={16} stroke={1.75} /> : <IconCopy size={16} stroke={1.75} />}
        onClick={() => {
          navigator.clipboard.writeText(text).then(
            () => {
              setCopied(true);
            },
            () => {
              setCopied(false);
            },
          );
        }}
      >
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}

/** Remove a cluster from the Hub: its agent is refused from then on. */
export function RemoveDialog({
  cluster,
  onClose,
}: {
  cluster: ClusterInfo | undefined;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const remove = useMutation({
    mutationFn: (id: string) => api.removeCluster(id),
    onSuccess: () => void client.invalidateQueries({ queryKey: queryKeys.clusters }),
  });
  const close = () => {
    setTyped("");
    remove.reset();
    onClose();
  };
  const matches = typed === cluster?.name;
  const cleanup = remove.data?.cleanup;

  return (
    <Dialog
      open={cluster !== undefined}
      onClose={close}
      title={
        cleanup ? `Removed ${cluster?.name ?? ""}` : `Remove ${cluster?.name ?? ""} from the Hub?`
      }
      description={
        cleanup ? (
          <p>The Hub no longer accepts its agent. To remove the agent from the cluster too, run:</p>
        ) : (
          <p>
            Its agent will be refused. Nothing in the cluster changes until you remove the agent
            yourself; the Hub shows how.
          </p>
        )
      }
      {...(cleanup
        ? {}
        : {
            onSubmit: () => {
              if (cluster && matches) remove.mutate(cluster.id);
            },
          })}
      footer={
        cleanup ? (
          <Button variant="primary" onClick={close}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button type="submit" variant="danger" disabled={!matches || remove.isPending}>
              {remove.isPending ? "Removing…" : "Remove"}
            </Button>
          </>
        )
      }
    >
      {cleanup ? (
        <Command text={cleanup} label="Clean-up command" />
      ) : (
        <div className={styles.stack}>
          <div className={styles.field}>
            <label htmlFor={inputId} className={styles.label}>
              Type <span className={styles.mono}>{cluster?.name}</span> to confirm
            </label>
            <input
              id={inputId}
              className={styles.input}
              autoComplete="off"
              spellCheck={false}
              autoFocus
              value={typed}
              onChange={(e) => {
                setTyped(e.target.value);
              }}
            />
          </div>
          {remove.error && (
            <p className={styles.error} role="alert">
              {remove.error instanceof ApiError ? remove.error.message : "Something went wrong."}
            </p>
          )}
        </div>
      )}
    </Dialog>
  );
}
