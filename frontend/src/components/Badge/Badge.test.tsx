import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Badge } from "./Badge";

describe("Badge", () => {
  it.each([
    { tone: "ok", word: "OK" },
    { tone: "high", word: "High" },
    { tone: "critical", word: "CrashLoopBackOff" },
    { tone: "neutral", word: "Stale" },
  ] as const)("shows the word and a $tone dot", ({ tone, word }) => {
    const { container } = render(<Badge tone={tone}>{word}</Badge>);
    const badge = screen.getByText(word).parentElement;
    expect(badge).toHaveAttribute("data-tone", tone);
    // The dot is decorative; the word carries the meaning.
    expect(container.querySelector('[aria-hidden="true"]')).toBeInTheDocument();
    expect(badge).toHaveTextContent(word);
  });

  it("renders an optional icon hidden from screen readers", () => {
    render(
      <Badge tone="critical" icon={<svg data-testid="icon" />}>
        Failed
      </Badge>,
    );
    expect(screen.getByTestId("icon").parentElement).toHaveAttribute("aria-hidden", "true");
  });
});
