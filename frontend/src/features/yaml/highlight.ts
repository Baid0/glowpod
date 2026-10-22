/** Token kinds the YAML view colors. */
export type TokenKind =
  "key" | "string" | "number" | "bool" | "null" | "comment" | "punct" | "plain";

export interface Token {
  kind: TokenKind;
  text: string;
}

// indent, optional "- ", key, colon, rest
const KEY_LINE = /^(\s*)(-\s+)?((?:"[^"]*"|'[^']*'|[^\s#:][^:#]*?)):(\s+|$)(.*)$/;
const LIST_ITEM = /^(\s*)(-)(\s+|$)(.*)$/;

/** Splits one line of YAML into colored tokens. Good enough for reading; not a parser. */
export function tokenizeLine(line: string): Token[] {
  if (/^\s*#/.test(line)) return [{ kind: "comment", text: line }];
  if (/^\s*(---|\.\.\.)\s*$/.test(line)) return [{ kind: "punct", text: line }];

  const key = KEY_LINE.exec(line);
  if (key) {
    const [, indent = "", dash = "", name = "", gap = "", rest = ""] = key;
    return compact([
      { kind: "plain", text: indent },
      { kind: "punct", text: dash },
      { kind: "key", text: name },
      { kind: "punct", text: ":" },
      { kind: "plain", text: gap },
      ...value(rest),
    ]);
  }
  const item = LIST_ITEM.exec(line);
  if (item) {
    const [, indent = "", dash = "", gap = "", rest = ""] = item;
    return compact([
      { kind: "plain", text: indent },
      { kind: "punct", text: dash },
      { kind: "plain", text: gap },
      ...value(rest),
    ]);
  }
  return [{ kind: "plain", text: line }];
}

/** A scalar value, with any trailing comment split off. */
function value(text: string): Token[] {
  if (!text) return [];
  let body = text;
  let comment = "";
  const hash = text.search(/\s#/);
  if (hash >= 0 && !/^["']/.test(text)) {
    body = text.slice(0, hash);
    comment = text.slice(hash);
  }
  const trimmed = body.trim();
  let kind: TokenKind = "plain";
  if (/^(["']).*\1$/.test(trimmed) || /^[|>][-+]?$/.test(trimmed)) kind = "string";
  else if (/^-?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$/.test(trimmed)) kind = "number";
  else if (/^(true|false)$/.test(trimmed)) kind = "bool";
  else if (/^(null|~)$/.test(trimmed)) kind = "null";
  else if (trimmed && !/^[{[]/.test(trimmed)) kind = "string";
  return compact([
    { kind, text: body },
    { kind: "comment", text: comment },
  ]);
}

function compact(tokens: Token[]): Token[] {
  return tokens.filter((t) => t.text !== "");
}
