import type { Icon } from "@tabler/icons-react";

export interface Command {
  id: string;
  label: string;
  /** A muted line after the label, e.g. where a found object is. */
  detail?: string;
  group: string;
  icon?: Icon;
  /** Extra words that should match, e.g. the Kubernetes term. */
  keywords?: string;
  run: () => void;
}

/** Every word of the query must appear in the label, group or keywords. */
export function filterCommands(commands: Command[], query: string): Command[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return commands;
  return commands.filter((c) => {
    const hay = `${c.label} ${c.group} ${c.keywords ?? ""}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}
