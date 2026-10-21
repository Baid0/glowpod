import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Button } from "./Button";

describe("Button", () => {
  it("is a real button that defaults to type=button and secondary", () => {
    render(<Button>View</Button>);
    const button = screen.getByRole("button", { name: "View" });
    expect(button).toHaveAttribute("type", "button");
    expect(button).toHaveAttribute("data-variant", "secondary");
    expect(button).toHaveAttribute("data-size", "md");
  });

  it.each(["primary", "secondary", "ghost", "danger"] as const)("renders the %s variant", (v) => {
    render(<Button variant={v}>Go</Button>);
    expect(screen.getByRole("button")).toHaveAttribute("data-variant", v);
  });

  it("calls onClick and is reachable by keyboard", async () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Retry</Button>);
    await userEvent.tab();
    expect(screen.getByRole("button")).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("does not fire when disabled", async () => {
    const onClick = vi.fn();
    render(
      <Button disabled onClick={onClick}>
        Apply
      </Button>,
    );
    await userEvent.click(screen.getByRole("button"));
    expect(onClick).not.toHaveBeenCalled();
  });

  it("hides the icon from screen readers", () => {
    render(<Button icon={<svg data-testid="icon" />}>Retry</Button>);
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.getByTestId("icon").parentElement).toHaveAttribute("aria-hidden", "true");
  });
});
