import { Button } from "../../components/Button";
import { ApiError } from "../../lib/api";
import styles from "./ObjectYaml.module.css";

/** Why an object couldn't be read, in plain words, with Retry when it may help. */
export function ObjectError({ error, onRetry }: { error: Error; onRetry: () => void }) {
  const status = error instanceof ApiError ? error.status : 0;
  const msg =
    status === 403
      ? "You can't read this. Ask an admin for access."
      : status === 404
        ? "This object is gone."
        : error.message;
  return (
    <div className={styles.error} role="alert">
      <p>{msg}</p>
      {status !== 403 && status !== 404 && (
        <Button size="sm" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  );
}
