import { IconCheck, IconCopy } from "@tabler/icons-react";
import { useMemo, useState } from "react";

import { Button } from "../../components/Button";
import { tokenizeLine } from "./highlight";
import styles from "./YamlView.module.css";

/** Read-only, highlighted YAML with line numbers and Copy. */
export function YamlView({ yaml, label = "YAML" }: { yaml: string; label?: string }) {
  const lines = useMemo(() => yaml.replace(/\n$/, "").split("\n"), [yaml]);
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(yaml);
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, 1500);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className={styles.view}>
      <div className={styles.toolbar}>
        <span className={styles.meta}>{lines.length} lines · read-only</span>
        <Button
          size="sm"
          icon={
            copied ? <IconCheck size={16} stroke={1.75} /> : <IconCopy size={16} stroke={1.75} />
          }
          onClick={() => void copy()}
        >
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <pre className={styles.code} tabIndex={0} aria-label={label}>
        <code>
          {lines.map((line, i) => (
            <span key={i} className={styles.line}>
              <span className={styles.number} aria-hidden="true">
                {i + 1}
              </span>
              <span className={styles.text}>
                {tokenizeLine(line).map((t, j) => (
                  <span key={j} data-token={t.kind}>
                    {t.text}
                  </span>
                ))}
                {"\n"}
              </span>
            </span>
          ))}
        </code>
      </pre>
    </div>
  );
}
