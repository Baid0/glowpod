import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Card } from "./Card";

describe("Card", () => {
  it("is a region named by its title", () => {
    render(<Card title="Needs attention">rows</Card>);
    const region = screen.getByRole("region", { name: "Needs attention" });
    expect(region).toHaveTextContent("rows");
    expect(screen.getByRole("heading", { level: 2, name: "Needs attention" })).toBeInTheDocument();
  });

  it("supports h3 titles and header actions", () => {
    render(
      <Card title="Recent events" headingLevel={3} actions={<a href="/events">See all</a>}>
        list
      </Card>,
    );
    expect(screen.getByRole("heading", { level: 3, name: "Recent events" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "See all" })).toBeInTheDocument();
  });

  it("has no header or label without a title", () => {
    const { container } = render(<Card padding="gauge">gauge</Card>);
    const section = container.querySelector("section");
    expect(section).not.toHaveAttribute("aria-labelledby");
    expect(section).toHaveAttribute("data-padding", "gauge");
    expect(section?.querySelector("h2, h3")).toBeNull();
  });
});
