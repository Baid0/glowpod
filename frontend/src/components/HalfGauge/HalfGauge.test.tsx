import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { HalfGauge, type HalfGaugeProps } from "./HalfGauge";

const cpu: HalfGaugeProps = {
  label: "CPU",
  value: 24.8,
  max: 40,
  unit: "cores",
  requested: 30,
  trend: 0.04,
};

function mockReducedMotion(reduce: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      matches: reduce && query.includes("reduce"),
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
}

describe("HalfGauge (live)", () => {
  it.each([
    { value: 24.8, want: "CPU 62 percent, OK", zone: "ok", word: "OK" },
    { value: 31.2, want: "CPU 78 percent, High", zone: "high", word: "High" },
    { value: 36.8, want: "CPU 92 percent, Critical", zone: "critical", word: "Critical" },
  ])("is a meter that reads $want", ({ value, want, zone, word }) => {
    const { container } = render(<HalfGauge {...cpu} value={value} />);

    const meter = screen.getByRole("meter", { name: "CPU" });
    expect(meter).toHaveAttribute("aria-valuenow", String(value));
    expect(meter).toHaveAttribute("aria-valuemin", "0");
    expect(meter).toHaveAttribute("aria-valuemax", "40");
    expect(meter).toHaveAttribute("aria-valuetext", want);
    expect(container.firstElementChild).toHaveAttribute("data-zone", zone);
    // The header badge repeats the zone as a word, never color alone.
    expect(screen.getByText(word)).toBeInTheDocument();
  });

  it("shows value, used of total, requested share and trend", () => {
    render(<HalfGauge {...cpu} />);
    expect(screen.getByText("62%")).toBeInTheDocument();
    expect(screen.getByText("24.8 of 40 cores")).toBeInTheDocument();
    expect(screen.getByText(/75% requested/)).toBeInTheDocument();
    expect(screen.getByText(/4% in 1h/)).toHaveTextContent("up4% in 1h");
  });

  it("respects custom thresholds", () => {
    render(<HalfGauge {...cpu} thresholds={[0.5, 0.6]} />);
    expect(screen.getByRole("meter")).toHaveAttribute("aria-valuetext", "CPU 62 percent, Critical");
  });

  it("supports raw values, notes and custom trend text", () => {
    render(
      <HalfGauge
        label="Network"
        value={120}
        max={200}
        display="value"
        detail="MB/s in + out"
        note="of 7-day peak 200"
        trend={-0.02}
        trendLabel="2 MB/s in 1h"
      />,
    );
    expect(screen.getByText("120")).toBeInTheDocument();
    expect(screen.getByText("MB/s in + out")).toBeInTheDocument();
    expect(screen.getByText("of 7-day peak 200")).toBeInTheDocument();
    expect(screen.getByText(/2 MB\/s in 1h/)).toHaveTextContent("down2 MB/s in 1h");
  });

  it.each([
    { value: 98.2, want: "98" },
    { value: 120, want: "120" },
    { value: 4.25, want: "4.3" },
  ])("shows raw value $value as $want", ({ value, want }) => {
    render(<HalfGauge label="Network" value={value} max={200} display="value" />);
    expect(screen.getByText(want)).toBeInTheDocument();
  });

  it("clamps values above max", () => {
    render(<HalfGauge {...cpu} value={50} />);
    expect(screen.getByText("100%")).toBeInTheDocument();
  });

  it("jumps straight to the value with reduced motion", () => {
    mockReducedMotion(true);
    const { container } = render(<HalfGauge {...cpu} />);
    const arcs = container.querySelectorAll("path[stroke-dasharray]");
    // outline + value arc + requested arc, already at their final lengths
    expect([...arcs].map((p) => p.getAttribute("stroke-dasharray"))).toEqual([
      "62 100",
      "62 100",
      "75 100",
    ]);
  });

  it("sweeps in from zero when motion is allowed", async () => {
    mockReducedMotion(false);
    const { container } = render(<HalfGauge {...cpu} />);
    await waitFor(
      () => {
        const arc = container.querySelector("path[stroke-dasharray]");
        expect(arc?.getAttribute("stroke-dasharray")).toBe("62 100");
      },
      { timeout: 2000 },
    );
  });

  it("has no header or footer at size S", () => {
    render(<HalfGauge {...cpu} size="S" />);
    expect(screen.getByText("62%")).toBeInTheDocument();
    expect(screen.queryByText("OK")).not.toBeInTheDocument();
    expect(screen.queryByText(/requested/)).not.toBeInTheDocument();
  });
});

describe("HalfGauge (other states)", () => {
  it("loading shows a skeleton, no value and no spinner", () => {
    render(<HalfGauge {...cpu} state="loading" />);
    const dial = screen.getByRole("img", { name: "CPU: loading" });
    expect(dial).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByRole("meter")).not.toBeInTheDocument();
    expect(screen.queryByText("62%")).not.toBeInTheDocument();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("stale mutes the arc and says how old the data is", () => {
    const { container } = render(<HalfGauge {...cpu} state="stale" updated="updated 3 min ago" />);
    expect(container.firstElementChild).toHaveAttribute("data-state", "stale");
    expect(screen.getByText("updated 3 min ago")).toBeInTheDocument();
    expect(screen.getByText("Stale")).toBeInTheDocument();
    expect(screen.getByRole("meter")).toHaveAttribute(
      "aria-valuetext",
      "CPU 62 percent, OK, updated 3 min ago",
    );
  });

  it("no-metrics shows a dotted track and the install link", () => {
    const { container } = render(<HalfGauge {...cpu} state="no-metrics" />);
    expect(screen.getByRole("img", { name: "CPU: no data" })).toBeInTheDocument();
    // Once in the header badge, once inside the dial.
    expect(screen.getAllByText("No data")).toHaveLength(2);
    expect(screen.getByRole("link", { name: "Install metrics-server" })).toHaveAttribute(
      "href",
      expect.stringContaining("metrics-server"),
    );
    expect(container.querySelector("path[stroke-dasharray='0 5.5']")).toBeInTheDocument();
    expect(screen.queryByText("62%")).not.toBeInTheDocument();
  });

  it("error shows Couldn't load and a working Retry button", async () => {
    const onRetry = vi.fn();
    render(<HalfGauge {...cpu} state="error" onRetry={onRetry} />);
    expect(screen.getByRole("img", { name: "CPU: couldn't load" })).toBeInTheDocument();
    expect(screen.getByText("Couldn't load")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
