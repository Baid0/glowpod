import { describe, expect, it } from "vitest";

import * as fx from "../../test/fixtures";
import { defaultContainer, levelOf, splitMatches, stripAnsi } from "./lines";

describe("stripAnsi", () => {
  it("removes terminal colors", () => {
    expect(stripAnsi("\u001b[31mERROR\u001b[0m boom")).toBe("ERROR boom");
    expect(stripAnsi("plain")).toBe("plain");
  });
});

describe("levelOf", () => {
  it.each([
    ["ERROR could not connect", "error"],
    ["panic: runtime error", "error"],
    ['{"level":"error","msg":"x"}', "error"],
    ["level=fatal msg=bye", "error"],
    ["Traceback (most recent call last):", "error"],
    ["WARN slow query", "warn"],
    ["level=warn msg=x", "warn"],
    ["GET /healthz 200", undefined],
    ["errors_total=0", undefined],
    // A stated level wins over words in the message or field names.
    [
      '{"level":"WARN","msg":"cluster request failed","code":"forbidden","err":"pods is forbidden"}',
      "warn",
    ],
    ['{"level":"INFO","msg":"retrying","err":"timeout"}', undefined],
    ['{"severity":"CRITICAL","message":"disk full"}', "error"],
    ['{"level":"debug","msg":"ERROR budget fine"}', undefined],
    ['level=info msg="slow" err="WARN: context canceled"', undefined],
    ["time=1 level=WARNING msg=x", "warn"],
    // Kubernetes components (klog).
    ['E1022 10:00:01.123456       1 controller.go:42] "sync failed"', "error"],
    ["W1022 10:00:01.123456       1 reflector.go:7] watch closed", "warn"],
    ["I1022 10:00:01.123456       1 main.go:9] ERROR-free start", undefined],
    // No stated level: a key named err isn't an error by itself.
    ['{"msg":"done","err":null}', undefined],
    ["request failed err=timeout", undefined],
    ["ERROR: could not connect", "error"],
    ["panic: boom", "error"],
    ["Warning: deprecated flag", "warn"],
  ])("%s → %s", (line, want) => {
    expect(levelOf(line)).toBe(want);
  });
});

describe("splitMatches", () => {
  it("splits around case-insensitive matches", () => {
    expect(splitMatches("DB timeout, db down", "db")).toEqual([
      { text: "DB", match: true },
      { text: " timeout, ", match: false },
      { text: "db", match: true },
      { text: " down", match: false },
    ]);
    expect(splitMatches("abc", "")).toEqual([{ text: "abc", match: false }]);
  });
});

describe("defaultContainer", () => {
  it("opens on the struggling container, else the first app container", () => {
    const d = fx.podDetail();
    expect(defaultContainer(d.containers)).toBe("app");
    const healthy = d.containers.map((c) => ({ ...c, restarts: 0, state: "running" as const }));
    // Sidecar listed first: still opens on the app container.
    expect(defaultContainer(healthy.toReversed())).toBe("app");
  });
});
