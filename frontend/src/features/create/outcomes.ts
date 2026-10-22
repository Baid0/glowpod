import type { StatusTone } from "../../components/Badge";
import type { ApplyItem, ApplyOutcome } from "../../lib/types";

/** The word for an outcome, as it will be (dry run) or as it was. */
export function outcomeWord(result: ApplyOutcome, dryRun: boolean): string {
  if (dryRun) {
    return {
      created: "Will be created",
      configured: "Will be updated",
      unchanged: "No change",
      applied: "Will be applied",
      failed: "Can't apply",
    }[result];
  }
  return {
    created: "Created",
    configured: "Updated",
    unchanged: "No change",
    applied: "Applied",
    failed: "Failed",
  }[result];
}

/** Status tone for each outcome; the word always goes with it. */
export const OUTCOME_TONE: Record<ApplyOutcome, StatusTone> = {
  created: "ok",
  configured: "ok",
  unchanged: "neutral",
  applied: "ok",
  failed: "critical",
};

/** "3 created, 1 updated, 1 failed" */
export function summarize(items: ApplyItem[]): string {
  const counts = new Map<string, number>();
  for (const it of items) {
    const word = outcomeWord(it.result, false).toLowerCase();
    counts.set(word, (counts.get(word) ?? 0) + 1);
  }
  return [...counts].map(([word, n]) => `${String(n)} ${word}`).join(", ");
}
