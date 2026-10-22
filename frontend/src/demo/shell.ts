// A pretend shell for the demo's terminal: enough of sh to look around a container (ls, cat,
// env, ps, df, top) without running anything.

export interface ShellContext {
  pod: string;
  namespace: string;
  container: string;
  image: string;
  node: string;
  ip: string;
  env: Record<string, string>;
  /** Memory the container uses and may use, in bytes. */
  memory: { used: number; limit: number };
  startedAt: number;
  now: () => number;
}

const DIM = "\x1b[2m";
const LIME = "\x1b[38;2;200;255;0m";
const BOLD = "\x1b[1m";
const RESET = "\x1b[0m";

type Dir = Record<string, string | null>;

function files(ctx: ShellContext): Record<string, Dir> {
  const app = ctx.image.split("/").pop()?.split(":")[0] ?? "app";
  return {
    "/": {
      app: null,
      bin: null,
      dev: null,
      etc: null,
      home: null,
      proc: null,
      tmp: null,
      usr: null,
      var: null,
    },
    "/app": {
      [app]: "binary",
      "config.yaml": `# ${app} defaults; the environment overrides these\nlisten: :8080\nlog:\n  level: info\n  format: json\nshutdown_timeout: 20s\n`,
      VERSION: `${ctx.image.split(":").pop() ?? "latest"}\n`,
    },
    "/etc": {
      hostname: `${ctx.pod}\n`,
      hosts: `127.0.0.1\tlocalhost\n${ctx.ip}\t${ctx.pod}\n`,
      "os-release":
        'NAME="Alpine Linux"\nID=alpine\nVERSION_ID=3.22.2\nPRETTY_NAME="Alpine Linux v3.22"\nHOME_URL="https://alpinelinux.org/"\n',
      "resolv.conf": `search ${ctx.namespace}.svc.cluster.local svc.cluster.local cluster.local eu-west-1.compute.internal\nnameserver 172.20.0.10\noptions ndots:5\n`,
      passwd: "root:x:0:0:root:/root:/bin/sh\napp:x:1000:1000:app:/home/app:/bin/sh\n",
    },
    "/tmp": {},
    "/home": { app: null },
    "/home/app": {},
    "/proc": {
      cpuinfo: "processor\t: 0\nmodel name\t: Intel(R) Xeon(R) Platinum 8375C CPU @ 2.90GHz\n",
      loadavg: "0.42 0.38 0.35 2/311 74\n",
      uptime: "",
    },
    "/var/run/secrets/kubernetes.io/serviceaccount": {
      "ca.crt": "-----BEGIN CERTIFICATE-----\n(demo certificate)\n-----END CERTIFICATE-----\n",
      namespace: ctx.namespace,
      token: "(demo token: there is no real cluster behind this shell)\n",
    },
  };
}

function resolve(cwd: string, path: string): string {
  const parts = (path.startsWith("/") ? path : `${cwd}/${path}`).split("/");
  const out: string[] = [];
  for (const p of parts) {
    if (!p || p === ".") continue;
    if (p === "..") out.pop();
    else out.push(p);
  }
  return "/" + out.join("/");
}

function human(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)}G`;
  return `${String(Math.round(bytes / 1024 ** 2))}M`;
}

export class FakeShell {
  private line = "";
  private cwd = "/app";
  private history: string[] = [];
  private back = 0;
  private done = false;

  constructor(
    private readonly ctx: ShellContext,
    private readonly out: (text: string) => void,
    private readonly exit: (code: number) => void,
  ) {}

  private get prompt(): string {
    return `${LIME}${this.ctx.container}${RESET}:${BOLD}${this.cwd === "/home/app" ? "~" : this.cwd}${RESET} $ `;
  }

  start() {
    this.out(
      `${DIM}Demo shell in ${this.ctx.pod}. Nothing runs here: try ls, cat config.yaml, env, ps, df -h, top or exit.${RESET}\r\n`,
    );
    this.out(this.prompt);
  }

  /** Keystrokes from the terminal. */
  input(data: string) {
    if (this.done) return;
    for (let i = 0; i < data.length; i++) {
      const ch = data[i] ?? "";
      if (ch === "\x1b") {
        // Arrow keys: up and down walk the history; the others do nothing here.
        const seq = data.slice(i, i + 3);
        if (seq === "\x1b[A" || seq === "\x1b[B") this.recall(seq === "\x1b[A" ? 1 : -1);
        i += 2;
        continue;
      }
      if (ch === "\r" || ch === "\n") {
        this.out("\r\n");
        const cmd = this.line.trim();
        this.line = "";
        this.back = 0;
        if (cmd) this.history.unshift(cmd);
        this.run(cmd);
        if (this.finished()) return;
        this.out(this.prompt);
      } else if (ch === "\x7f" || ch === "\b") {
        if (this.line) {
          this.line = this.line.slice(0, -1);
          this.out("\b \b");
        }
      } else if (ch === "\x03") {
        this.line = "";
        this.out("^C\r\n" + this.prompt);
      } else if (ch === "\x04") {
        if (!this.line) {
          this.out("exit\r\n");
          this.quit(0);
          return;
        }
      } else if (ch === "\x0c") {
        this.out("\x1b[2J\x1b[H" + this.prompt + this.line);
      } else if (ch >= " ") {
        this.line += ch;
        this.out(ch);
      }
    }
  }

  private recall(step: number) {
    const next = Math.min(this.history.length, Math.max(0, this.back + step));
    if (next === this.back) return;
    this.back = next;
    this.out("\b \b".repeat(this.line.length));
    this.line = next === 0 ? "" : (this.history[next - 1] ?? "");
    this.out(this.line);
  }

  /** Whether the last command ended the shell. A method, so the check isn't narrowed away. */
  private finished(): boolean {
    return this.done;
  }

  private quit(code: number) {
    this.done = true;
    this.exit(code);
  }

  private print(text: string) {
    this.out(text.replace(/\n/g, "\r\n"));
  }

  private run(cmd: string) {
    if (!cmd) return;
    const [name = "", ...args] = cmd.split(/\s+/);
    const fs = files(this.ctx);
    const env = {
      HOSTNAME: this.ctx.pod,
      HOME: "/home/app",
      PATH: "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
      KUBERNETES_SERVICE_HOST: "172.20.0.1",
      KUBERNETES_SERVICE_PORT: "443",
      ...this.ctx.env,
    };
    const now = this.ctx.now();
    switch (name) {
      case "help":
        this.print(
          "Commands in this demo: ls, cd, pwd, cat, env, echo, ps, top, df, free, uptime, date,\nhostname, whoami, id, uname, clear, exit.\n",
        );
        return;
      case "exit":
      case "logout":
        this.quit(Number(args[0] ?? 0) || 0);
        return;
      case "clear":
        this.out("\x1b[2J\x1b[H");
        return;
      case "pwd":
        this.print(this.cwd + "\n");
        return;
      case "cd": {
        const to = resolve(this.cwd, args[0] ?? "/home/app");
        if (fs[to]) this.cwd = to;
        else this.print(`sh: cd: can't cd to ${args[0] ?? ""}: No such file or directory\n`);
        return;
      }
      case "ls": {
        const target = resolve(this.cwd, args.find((x) => !x.startsWith("-")) ?? ".");
        const dir = fs[target];
        if (!dir) {
          const parent = fs[resolve(target, "..")];
          const base = target.split("/").pop() ?? "";
          if (parent && base in parent) this.print(base + "\n");
          else this.print(`ls: ${target}: No such file or directory\n`);
          return;
        }
        const names = Object.keys(dir).sort();
        if (args.some((x) => x.startsWith("-l"))) {
          for (const n of names) {
            const isDir = dir[n] === null;
            const size = isDir ? 4096 : (dir[n] ?? "").length;
            this.print(
              `${isDir ? "drwxr-xr-x" : "-rw-r--r--"}    1 app      app       ${String(size).padStart(8)} Oct 22 09:14 ${n}\n`,
            );
          }
        } else {
          this.print(
            names.map((n) => (dir[n] === null ? `\x1b[1;34m${n}${RESET}` : n)).join("  ") + "\n",
          );
        }
        return;
      }
      case "cat": {
        if (!args.length) return;
        for (const a of args) {
          const path = resolve(this.cwd, a);
          const dir = fs[resolve(path, "..")];
          const base = path.split("/").pop() ?? "";
          const content = dir?.[base];
          if (fs[path]) this.print(`cat: read error: Is a directory\n`);
          else if (content === "binary")
            this.print(`cat: ${a}: that's the app's binary; it doesn't print well.\n`);
          else if (typeof content === "string") this.print(content);
          else this.print(`cat: can't open '${a}': No such file or directory\n`);
        }
        return;
      }
      case "env":
      case "printenv":
        this.print(
          Object.entries(env)
            .map(([k, v]) => `${k}=${v}`)
            .join("\n") + "\n",
        );
        return;
      case "echo":
        this.print(
          args
            .map((a) =>
              a.replace(
                /\$\{?([A-Z_][A-Z0-9_]*)\}?/g,
                (_, k: string) => (env as Record<string, string | undefined>)[k] ?? "",
              ),
            )
            .join(" ") + "\n",
        );
        return;
      case "hostname":
        this.print(this.ctx.pod + "\n");
        return;
      case "whoami":
        this.print("app\n");
        return;
      case "id":
        this.print("uid=1000(app) gid=1000(app) groups=1000(app)\n");
        return;
      case "uname":
        this.print(
          args.includes("-a")
            ? `Linux ${this.ctx.pod} 6.12.46-66.121.amzn2023.x86_64 #1 SMP x86_64 Linux\n`
            : "Linux\n",
        );
        return;
      case "date":
        this.print(new Date(now).toUTCString().replace("GMT", "UTC") + "\n");
        return;
      case "uptime": {
        const mins = Math.floor((now - this.ctx.startedAt) / 60_000);
        this.print(
          ` ${new Date(now).toISOString().slice(11, 19)} up ${String(Math.floor(mins / 60))}:${String(mins % 60).padStart(2, "0")},  0 users,  load average: 0.42, 0.38, 0.35\n`,
        );
        return;
      }
      case "ps":
      case "top": {
        const bin = this.ctx.image.split("/").pop()?.split(":")[0] ?? "app";
        const rss = human(this.ctx.memory.used);
        if (name === "top") {
          this.print(
            `Mem: ${human(this.ctx.memory.used)} used, ${human(Math.max(0, this.ctx.memory.limit - this.ctx.memory.used))} free (limit ${human(this.ctx.memory.limit)})\nLoad average: 0.42 0.38 0.35 2/311 74\n`,
          );
        }
        this.print(
          `PID   USER     TIME  ${name === "top" ? "%CPU  " : ""}RSS   COMMAND\n    1 app       4:12 ${name === "top" ? "  3.1 " : ""}${rss.padStart(5)} /app/${bin}\n   61 app       0:00 ${name === "top" ? "  0.0 " : ""}   1M /bin/sh\n   74 app       0:00 ${name === "top" ? "  0.0 " : ""}   1M ${name}\n`,
        );
        return;
      }
      case "df":
        this.print(
          `Filesystem                Size      Used Available Use% Mounted on\noverlay                  80.0G     21.4G     58.6G  27% /\ntmpfs                    64.0M         0     64.0M   0% /dev\n/dev/nvme0n1p1           80.0G     21.4G     58.6G  27% /etc/hosts\ntmpfs                    14.5G     12.0K     14.5G   0% /var/run/secrets/kubernetes.io/serviceaccount\n`,
        );
        return;
      case "free": {
        const mb = (b: number) => String(Math.round(b / 1024 ** 2)).padStart(10);
        this.print(
          `              total       used       free\nMem:     ${mb(this.ctx.memory.limit)} ${mb(this.ctx.memory.used)} ${mb(Math.max(0, this.ctx.memory.limit - this.ctx.memory.used))}\n`,
        );
        return;
      }
      case "kubectl":
        this.print(
          "sh: kubectl: not found (you're inside the container; Glowpod is your kubectl)\n",
        );
        return;
      case "curl":
      case "wget":
      case "ping":
      case "nc":
        this.print(
          `${name}: the demo has no network. In a real cluster this would reach ${args.at(-1) ?? "the address"}.\n`,
        );
        return;
      case "sudo":
      case "su":
        this.print(`sh: ${name}: not found\n`);
        return;
      case "vi":
      case "vim":
      case "nano":
        this.print(`sh: ${name}: not found (the image ships without an editor, like most do)\n`);
        return;
      case "rm":
      case "mv":
      case "cp":
      case "touch":
      case "mkdir":
        this.print(`${name}: Read-only file system\n`);
        return;
      default:
        this.print(`sh: ${name}: not found\n`);
    }
  }
}
