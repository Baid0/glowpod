import { describe, expect, it } from "vitest";

import { fromYaml, toYaml, YamlError } from "./yaml";

describe("toYaml and fromYaml", () => {
  const object = {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: {
      name: "web",
      labels: { "app.kubernetes.io/name": "web", tier: "front" },
      annotations: { "deployment.kubernetes.io/revision": "7", empty: "" },
    },
    spec: {
      replicas: 3,
      paused: false,
      template: {
        spec: {
          containers: [
            {
              name: "app",
              image: "ghcr.io/acme/web:1.2.3",
              args: ["--port", "8080", "true", "-v"],
              env: [{ name: "GREETING", value: "hello: world # not a comment" }],
              resources: { requests: { cpu: "250m", memory: "512Mi" }, limits: {} },
            },
          ],
          volumes: [],
          matrix: [
            [1, 2],
            [3, 4],
          ],
        },
      },
    },
    data: { Corefile: ".:53 {\n    errors\n\n    cache 30\n}\n", "no-newline": "a\nb" },
    status: { conditions: null, ratio: 0.25 },
  };

  it("reads back what it writes", () => {
    expect(fromYaml(toYaml(object))).toEqual(object);
  });

  it("writes like kubectl: sorted keys, lists flush with their key, quoted look-alikes", () => {
    const y = toYaml(object);
    expect(y.startsWith("apiVersion: apps/v1\ndata:\n")).toBe(true);
    expect(y).toContain(
      '      containers:\n      - args:\n        - --port\n        - "8080"\n        - "true"',
    );
    expect(y).toContain('deployment.kubernetes.io/revision: "7"');
    expect(y).toContain('empty: ""');
    expect(y).toContain("Corefile: |\n    .:53 {");
    expect(y).toContain("no-newline: |-\n    a\n    b");
  });

  it("reads hand edits: comments, quotes, flow lists and folded text", () => {
    const y = [
      "# edited by hand",
      "kind: Pod   # a comment",
      "'quoted key': 'it''s'",
      'list: [a, "b c", 3]',
      "folded: >",
      "  one",
      "  two",
      "nested:",
      "  - name: x",
      "    value: ~",
      "",
    ].join("\n");
    expect(fromYaml(y)).toEqual({
      kind: "Pod",
      "quoted key": "it's",
      list: ["a", "b c", 3],
      folded: "one two\n",
      nested: [{ name: "x", value: null }],
    });
  });

  it.each([
    ["kind: Pod\n\tname: x\n", /line 2: use spaces/],
    ["a: 1\n   b: 2\n", /line 2: unexpected indentation/],
    ['a: "open\n', /line 1: unclosed quote/],
    ["a: 1\na: 2\n", /line 2: "a" appears twice/],
  ])("says where %j goes wrong", (y, message) => {
    expect(() => fromYaml(y)).toThrow(YamlError);
    expect(() => fromYaml(y)).toThrow(message);
  });
});
