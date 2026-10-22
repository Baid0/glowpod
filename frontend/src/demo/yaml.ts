// A small YAML writer and reader for the demo's objects. The writer prints the way the Go server
// does (keys sorted, sequences flush with their key); the reader takes that back plus the edits
// people make by hand: block maps and lists, quoted and plain scalars, | and > blocks, comments
// and simple flow lists like [a, b].

type Value = null | boolean | number | string | Value[] | { [key: string]: Value };

export class YamlError extends Error {
  constructor(
    message: string,
    readonly line: number,
  ) {
    super(`line ${String(line)}: ${message}`);
    this.name = "YamlError";
  }
}

const RESERVED = /^(?:true|false|yes|no|on|off|y|n|null|~)$/i;
const NUMBER =
  /^[-+]?(?:\d[\d_]*(?:\.\d*)?(?:e[-+]?\d+)?|\.\d+(?:e[-+]?\d+)?|0x[\da-f]+|0o[0-7]+|\.inf|\.nan)$/i;

function needsQuotes(s: string): boolean {
  return (
    s === "" ||
    RESERVED.test(s) ||
    NUMBER.test(s) ||
    /^[\s,[\]{}#&*!|>'"%@`]/.test(s) ||
    /^[-?:](?: |$)/.test(s) ||
    /\s$/.test(s) ||
    /: |:$| #|[\t\n\r]/.test(s)
  );
}

function scalar(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (typeof v === "string") return needsQuotes(v) ? JSON.stringify(v) : v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return JSON.stringify(v);
}

const isMap = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

function isBlock(v: unknown): boolean {
  return (isMap(v) && Object.keys(v).length > 0) || (Array.isArray(v) && v.length > 0);
}

function inline(v: unknown): string {
  if (Array.isArray(v)) return "[]";
  if (isMap(v)) return "{}";
  return scalar(v);
}

/** A multi-line string as a | block, indented to `pad`. */
function literal(s: string, pad: string): string {
  const chomp = s.endsWith("\n") ? "" : "-";
  const body = s.replace(/\n$/, "").split("\n");
  return `|${chomp}\n` + body.map((l) => (l ? pad + l : "")).join("\n");
}

function value(v: unknown, pad: string): string {
  if (typeof v === "string" && v.includes("\n") && !/^\s/.test(v)) return " " + literal(v, pad);
  return " " + inline(v);
}

function writeMap(m: Record<string, unknown>, pad: string): string[] {
  const out: string[] = [];
  for (const key of Object.keys(m).sort()) {
    const v = m[key];
    if (v === undefined) continue;
    const k = scalar(key);
    if (isMap(v) && isBlock(v)) {
      out.push(`${pad}${k}:`, ...writeMap(v, pad + "  "));
    } else if (Array.isArray(v) && v.length > 0) {
      out.push(`${pad}${k}:`, ...writeList(v, pad));
    } else {
      out.push(`${pad}${k}:${value(v, pad + "  ")}`);
    }
  }
  return out;
}

function writeList(list: unknown[], pad: string): string[] {
  const out: string[] = [];
  for (const item of list) {
    if (isBlock(item)) {
      const lines = isMap(item)
        ? writeMap(item, pad + "  ")
        : writeList(item as unknown[], pad + "  ");
      const [first = "", ...rest] = lines;
      out.push(`${pad}- ${first.slice(pad.length + 2)}`, ...rest);
    } else {
      out.push(`${pad}-${value(item, pad + "  ")}`);
    }
  }
  return out;
}

/** The object as YAML, ending in a newline. */
export function toYaml(obj: Record<string, unknown>): string {
  return writeMap(obj, "").join("\n") + "\n";
}

interface Line {
  n: number;
  indent: number;
  text: string;
}

/** Drops a trailing # comment that sits outside quotes. */
function stripComment(s: string): string {
  let quote = "";
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (quote) {
      if (c === quote) quote = "";
      else if (c === "\\" && quote === '"') i++;
    } else if (c === '"' || c === "'") {
      if (i === 0 || /[\s:[{,-]/.test(s[i - 1] ?? "")) quote = c;
    } else if (c === "#" && (i === 0 || /\s/.test(s[i - 1] ?? ""))) {
      return s.slice(0, i).trimEnd();
    }
  }
  return s.trimEnd();
}

class Reader {
  private i = 0;

  constructor(
    private readonly raw: string[],
    private readonly lines: Line[],
  ) {}

  static of(text: string): Reader {
    const raw = text.replace(/\r\n?/g, "\n").split("\n");
    const lines: Line[] = [];
    raw.forEach((l, idx) => {
      if (l.slice(0, l.length - l.trimStart().length).includes("\t")) {
        throw new YamlError("use spaces, not tabs, to indent", idx + 1);
      }
      const text = stripComment(l);
      if (text.trim() === "" || text.trim() === "---") return;
      lines.push({ n: idx + 1, indent: text.length - text.trimStart().length, text: text.trim() });
    });
    return new Reader(raw, lines);
  }

  document(): Value {
    if (this.lines.length === 0) return null;
    const first = this.lines[0];
    if (!first) return null;
    const v = this.node(first.indent);
    const extra = this.peek();
    if (extra) throw new YamlError("unexpected indentation", extra.n);
    return v;
  }

  private peek(): Line | undefined {
    return this.lines[this.i];
  }

  private node(indent: number): Value {
    const line = this.peek();
    if (!line) return null;
    if (line.text === "-" || line.text.startsWith("- ")) return this.list(indent);
    if (keyOf(line.text)) return this.map(indent);
    this.i++;
    return this.plain(line.text, line);
  }

  private map(indent: number): Value {
    const out: Record<string, Value> = {};
    for (let line = this.peek(); line?.indent === indent; line = this.peek()) {
      const kv = keyOf(line.text);
      if (!kv) throw new YamlError("expected key: value", line.n);
      if (Object.hasOwn(out, kv.key)) throw new YamlError(`"${kv.key}" appears twice`, line.n);
      this.i++;
      out[kv.key] = this.after(kv.rest, line, indent);
    }
    const next = this.peek();
    if (next && next.indent > indent) throw new YamlError("unexpected indentation", next.n);
    return out;
  }

  private list(indent: number): Value {
    const out: Value[] = [];
    for (let line = this.peek(); line?.indent === indent; line = this.peek()) {
      if (!(line.text === "-" || line.text.startsWith("- "))) break;
      const rest = line.text.slice(1).trimStart();
      if (rest === "") {
        this.i++;
        const next = this.peek();
        out.push(next && next.indent > indent ? this.node(next.indent) : null);
        continue;
      }
      // "- key: value" starts a map whose keys line up after the dash.
      const inner = indent + (line.text.length - rest.length);
      if (keyOf(rest) || rest.startsWith("- ")) {
        this.lines[this.i] = { n: line.n, indent: inner, text: rest };
        out.push(this.node(inner));
      } else {
        this.i++;
        out.push(this.after(rest, line, indent, true));
      }
    }
    return out;
  }

  /** The value after "key:" or "- ": inline, a block scalar, or a nested block. */
  private after(rest: string, line: Line, indent: number, inList = false): Value {
    if (rest === "") {
      const next = this.peek();
      if (next && next.indent > indent) return this.node(next.indent);
      // Kubernetes style: a list under a key sits at the key's own indent.
      if (!inList && next?.indent === indent && (next.text === "-" || next.text.startsWith("- ")))
        return this.list(indent);
      return null;
    }
    if (/^[|>][-+]?$/.test(rest)) return this.block(rest, line, indent);
    return this.plain(rest, line);
  }

  private block(header: string, line: Line, indent: number): string {
    // Block text keeps its blank lines, so read the raw lines rather than the cleaned ones.
    const body: string[] = [];
    let n = line.n;
    let pad = -1;
    while (n < this.raw.length) {
      const raw = this.raw[n] ?? "";
      const lead = raw.length - raw.trimStart().length;
      if (raw.trim() !== "") {
        if (pad < 0) pad = lead;
        if (lead < pad || lead <= indent) break;
      }
      body.push(raw.trim() === "" ? "" : raw.slice(pad));
      n++;
    }
    while (body.length > 0 && body.at(-1) === "") body.pop();
    while (this.peek() && (this.peek()?.n ?? 0) <= n) this.i++;
    const text = header.startsWith(">")
      ? body.join("\n").replace(/([^\n])\n(?=[^\n])/g, "$1 ")
      : body.join("\n");
    if (header.endsWith("-")) return text;
    return text + "\n";
  }

  private plain(text: string, line: Line): Value {
    if (text.startsWith('"')) {
      if (!/^"(?:[^"\\]|\\.)*"$/.test(text)) throw new YamlError("unclosed quote", line.n);
      try {
        return JSON.parse(text) as string;
      } catch {
        throw new YamlError("bad escape in quoted text", line.n);
      }
    }
    if (text.startsWith("'")) {
      if (!/^'(?:[^']|'')*'$/.test(text)) throw new YamlError("unclosed quote", line.n);
      return text.slice(1, -1).replace(/''/g, "'");
    }
    if (text === "{}") return {};
    if (text.startsWith("[")) {
      if (!text.endsWith("]")) throw new YamlError("unclosed [", line.n);
      const inner = text.slice(1, -1).trim();
      return inner === "" ? [] : inner.split(",").map((part) => this.plain(part.trim(), line));
    }
    if (text.startsWith("{"))
      throw new YamlError("write maps as indented key: value lines", line.n);
    if (/^[&*!]/.test(text)) throw new YamlError("anchors and tags aren't supported here", line.n);
    if (/^(?:null|~)$/i.test(text)) return null;
    if (/^(?:true|false)$/i.test(text)) return text.toLowerCase() === "true";
    if (/^[-+]?\d+$/.test(text)) return Number(text);
    if (/^[-+]?(?:\d+\.\d*|\.\d+|\d+)(?:e[-+]?\d+)?$/i.test(text)) return Number(text);
    return text;
  }
}

function keyOf(text: string): { key: string; rest: string } | undefined {
  const quoted = /^("(?:[^"\\]|\\.)*"|'(?:[^']|'')*')\s*:(?:\s+(.*))?$/.exec(text);
  if (quoted) {
    const k = quoted[1] ?? "";
    const key = k.startsWith('"') ? (JSON.parse(k) as string) : k.slice(1, -1).replace(/''/g, "'");
    return { key, rest: (quoted[2] ?? "").trim() };
  }
  const plain = /^([^\s"'#\-[{][^:]*?|-[^\s:][^:]*?)\s*:(?:\s+(.*))?$/.exec(text);
  if (!plain) return undefined;
  return { key: plain[1] ?? "", rest: (plain[2] ?? "").trim() };
}

/** Reads one YAML document. Throws YamlError with a line number when it can't. */
export function fromYaml(text: string): unknown {
  return Reader.of(text).document();
}
