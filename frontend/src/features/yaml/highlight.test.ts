import { describe, expect, it } from "vitest";

import { tokenizeLine, type Token } from "./highlight";

const kinds = (tokens: Token[]) => tokens.map((t) => `${t.kind}:${t.text}`);

describe("tokenizeLine", () => {
  it.each([
    ["apiVersion: apps/v1", ["key:apiVersion", "punct::", "plain: ", "string:apps/v1"]],
    ["  replicas: 3", ["plain:  ", "key:replicas", "punct::", "plain: ", "number:3"]],
    ["  paused: false", ["plain:  ", "key:paused", "punct::", "plain: ", "bool:false"]],
    ["  hostIP: null", ["plain:  ", "key:hostIP", "punct::", "plain: ", "null:null"]],
    ['    revision: "1"', ["plain:    ", "key:revision", "punct::", "plain: ", 'string:"1"']],
    ["metadata:", ["key:metadata", "punct::"]],
    ["  - name: app", ["plain:  ", "punct:- ", "key:name", "punct::", "plain: ", "string:app"]],
    ["  - 8080", ["plain:  ", "punct:-", "plain: ", "number:8080"]],
    ["# a comment", ["comment:# a comment"]],
    [
      "  image: nginx # pinned",
      ["plain:  ", "key:image", "punct::", "plain: ", "string:nginx", "comment: # pinned"],
    ],
    ["  script: |", ["plain:  ", "key:script", "punct::", "plain: ", "string:|"]],
    ["    echo hi", ["plain:    echo hi"]],
    ["  labels: {}", ["plain:  ", "key:labels", "punct::", "plain: ", "plain:{}"]],
    [
      "  url: http://x:8080/a",
      ["plain:  ", "key:url", "punct::", "plain: ", "string:http://x:8080/a"],
    ],
    ["---", ["punct:---"]],
  ])("%s", (line, want) => {
    expect(kinds(tokenizeLine(line))).toEqual(want);
  });

  it("keeps every character", () => {
    for (const line of ["  a: b # c", "- x", "k: 'v: w'", "  ports:", ""]) {
      expect(
        tokenizeLine(line)
          .map((t) => t.text)
          .join(""),
      ).toBe(line);
    }
  });
});
