import { describe, expect, it } from "vitest";

import { decodeSecretValue } from "./secret";

describe("decodeSecretValue", () => {
  it.each([
    ["aHVudGVyMg==", { text: "hunter2", bytes: 7 }],
    ["", { text: "", bytes: 0 }],
    ["4pyTIMOgIGxhIG1vZGU=", { text: "✓ à la mode", bytes: 14 }],
    ["//79", { bytes: 3 }],
    ["not base64!!", { bytes: 0 }],
  ])("%s", (b64, want) => {
    expect(decodeSecretValue(b64)).toEqual(want);
  });
});
