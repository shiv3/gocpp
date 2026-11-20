// Shared helpers for the parity runner: server and simulator process wrappers
// and the normalisation applied to observations.
import { spawn, type ChildProcess } from "node:child_process";
import { closeSync, mkdirSync, openSync, readFileSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, join, resolve } from "node:path";

export const PARITY_DIR = resolve(import.meta.dir, "..");
export const SIM_DIR = process.env.SIM_DIR ?? "/work/wt/ocpp-cp-sim-parity";
export type Lib = "gocpp" | "ocpp-go";
export type Version = "1.6" | "2.0.1" | "2.1";
export const SUBPROTO: Record<Version, string> = { "1.6": "ocpp1.6", "2.0.1": "ocpp2.0.1", "2.1": "ocpp2.1" };
export const SIM_VER: Record<Version, string> = { "1.6": "OCPP-1.6J", "2.0.1": "OCPP-2.0.1", "2.1": "OCPP-2.1" };
export const LIBS: Lib[] = ["gocpp", "ocpp-go"];
export const BIN: Record<Lib, string> = {
  gocpp: join(PARITY_DIR, "bin/cs-gocpp"),
  "ocpp-go": join(PARITY_DIR, "bin/cs-ocppgo"),
};

export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

// ---------------------------------------------------------------- normalise

const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/g;
const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$/;
function nearDates(): string[] {
  const out: string[] = [];
  for (const d of [-1, 0, 1]) out.push(new Date(Date.now() + d * 864e5).toISOString().slice(0, 10));
  return out;
}

/** Deep-normalise a JSON value: sorted keys, uuids and "now" timestamps
 *  replaced, very long strings summarised. */
export function norm(v: unknown): unknown {
  if (typeof v === "string") {
    if (ISO.test(v) && nearDates().includes(v.slice(0, 10))) return "<now>";
    if (v.length > 200) return `<str len=${v.length} sha=${createHash("sha1").update(v).digest("hex").slice(0, 8)}>`;
    return v.replace(UUID, "<uuid>").replace(/127\.0\.0\.1:\d+/g, "127.0.0.1:<port>");
  }
  if (Array.isArray(v)) return v.map(norm);
  if (v && typeof v === "object") {
    const o: Record<string, unknown> = {};
    for (const k of Object.keys(v as object).sort()) o[k] = norm((v as Record<string, unknown>)[k]);
    return o;
  }
  return v;
}

/** Normalise an OCPP frame (array); CallError descriptions are stripped and
 *  returned separately. */
export function normFrame(f: unknown, side?: (s: string) => void): unknown {
  if (Array.isArray(f) && f[0] === 4 && f.length >= 4) {
    if (typeof f[3] === "string" && f[3] !== "") side?.(String(f[3]));
    const c = [...f];
    c[3] = typeof f[3] === "string" ? (f[3] === "" ? "" : "<desc>") : f[3];
    return norm(c);
  }
  return norm(f);
}

export const stable = (v: unknown) => JSON.stringify(v);

// ------------------------------------------------------------------- server

export class Server {
  proc?: ChildProcess;
  exited: { code: number | null; signal: string | null } | null = null;
  constructor(
    public lib: Lib,
    public port: number,
    public dir: string,
    public extraArgs: string[] = [],
    public version: Version = "1.6",
  ) {}
  get base() { return `http://127.0.0.1:${this.port}`; }
  get eventsFile() { return join(this.dir, "server.jsonl"); }

  async start(): Promise<void> {
    mkdirSync(this.dir, { recursive: true });
    this.exited = null;
    const fd = openSync(join(this.dir, "server.log"), "a");
    this.proc = spawn(BIN[this.lib], ["-listen", `127.0.0.1:${this.port}`, "-version", this.version, "-events", this.eventsFile, "-call-timeout", "5s", ...this.extraArgs], {
      stdio: ["ignore", fd, fd],
    });
    closeSync(fd);
    this.proc.on("exit", (code, signal) => { this.exited = { code, signal }; });
    for (let i = 0; i < 100; i++) {
      if (this.exited) throw new Error(`server exited at start: ${JSON.stringify(this.exited)}`);
      if (await this.healthy()) return;
      await sleep(50);
    }
    throw new Error("server did not become healthy");
  }

  async http(path: string, body?: unknown, timeoutMs = 8000): Promise<{ status: number; json: any; text: string } | { error: string }> {
    try {
      const r = await fetch(this.base + path, {
        method: body === undefined ? "GET" : "POST",
        headers: { "content-type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: AbortSignal.timeout(timeoutMs),
      });
      const text = await r.text();
      let json: any = null;
      try { json = JSON.parse(text); } catch { /* keep text */ }
      return { status: r.status, json, text };
    } catch (e) {
      return { error: String((e as Error).message ?? e) };
    }
  }
  async healthy(): Promise<boolean> {
    const r = await this.http("/control/health", undefined, 1000);
    return !("error" in r) && r.status === 200;
  }
  /** Alive = process still running and control API answers. */
  async alive(): Promise<boolean> {
    if (this.exited) return false;
    return this.healthy();
  }
  async events(): Promise<any[]> {
    const r = await this.http("/control/events");
    return "error" in r ? [] : (r.json ?? []);
  }
  async connections(): Promise<string[] | null> {
    const r = await this.http("/control/connections");
    return "error" in r ? null : (r.json?.connections ?? r.json ?? null);
  }
  call(cpId: string, action: string, payload: unknown) {
    return this.http("/control/call", { cpId, action, payload }, 12000);
  }
  async stop() {
    if (!this.proc || this.exited) return;
    this.proc.kill("SIGKILL");
    for (let i = 0; i < 40 && !this.exited; i++) await sleep(50);
  }
}

// ---------------------------------------------------------------- simulator

export interface SimResp { ok?: boolean; data?: any; error?: string; timeout?: true }

export class Sim {
  proc!: ChildProcess;
  events: { t: number; event: string; data: any }[] = [];
  responses = new Map<string, (r: SimResp) => void>();
  stdoutRaw = "";
  logLines: string[] = [];
  exited = false;
  private buf = "";
  private n = 0;
  readonly tracePath: string;
  constructor(public name: string, public cpId: string, public url: string, public dir: string, public extraArgs: string[] = [], public version: Version = "1.6") {
    this.tracePath = join(dir, `sim-${name}.trace.jsonl`);
  }

  /** Command line of the child process . */
  protected spawnArgs(): { cmd: string; args: string[]; cwd?: string } {
    return {
      cmd: process.execPath,
      cwd: SIM_DIR,
      args: [
        "apps/cli/src/main.ts", "--cp-id", this.cpId, "--ws-url", this.url,
        "--ocpp-version", SIM_VER[this.version], "--json", "--trace-output", this.tracePath,
        "--reconnect-max-attempts", "0", "--reconnect-mode", "fixed", "--reconnect-base-delay-ms", "3600000", ...this.extraArgs,
      ],
    };
  }

  start() {
    mkdirSync(this.dir, { recursive: true });
    const sp = this.spawnArgs();
    this.proc = spawn(sp.cmd, sp.args, { cwd: sp.cwd, stdio: ["pipe", "pipe", "pipe"] });
    this.proc.stdout!.on("data", (d: Buffer) => this.onData(d.toString()));
    this.proc.stderr!.on("data", (d: Buffer) => { this.stdoutRaw += "[stderr] " + d.toString(); });
    this.proc.on("exit", () => {
      this.exited = true;
      for (const r of this.responses.values()) r({ error: "sim exited" });
      this.responses.clear();
    });
    this.proc.stdin!.on("error", () => {});
  }

  private onData(s: string) {
    this.stdoutRaw += s;
    this.buf += s;
    let i: number;
    while ((i = this.buf.indexOf("\n")) >= 0) {
      const line = this.buf.slice(0, i);
      this.buf = this.buf.slice(i + 1);
      if (!line.startsWith("{")) { this.logLines.push(line); continue; }
      let j: any;
      try { j = JSON.parse(line); } catch { this.logLines.push(line); continue; }
      if (typeof j.event === "string") this.events.push({ t: Date.now(), event: j.event, data: j.data });
      else if ("ok" in j && "id" in j) {
        const r = this.responses.get(String(j.id));
        if (r) { this.responses.delete(String(j.id)); r(j); }
      }
    }
  }

  cmd(command: string, params: unknown = {}, timeoutMs = 10000): Promise<SimResp> {
    if (this.exited) return Promise.resolve({ error: "sim exited" });
    const id = `c${++this.n}`;
    return new Promise((resolve) => {
      const to = setTimeout(() => { this.responses.delete(id); resolve({ timeout: true }); }, timeoutMs);
      this.responses.set(id, (r) => { clearTimeout(to); resolve(r); });
      this.proc.stdin!.write(JSON.stringify({ id, command, params }) + "\n");
    });
  }

  async waitEvent(name: string, ms: number, from = 0): Promise<any | null> {
    const end = Date.now() + ms;
    while (Date.now() < end) {
      const e = this.events.slice(from).find((x) => x.event === name);
      if (e) return e;
      await sleep(25);
    }
    return null;
  }

  kill(sig: NodeJS.Signals = "SIGKILL") { this.proc.kill(sig); }

  async end() {
    if (this.exited) return;
    try { this.proc.stdin!.end(); } catch { /* ignore */ }
    for (let i = 0; i < 40 && !this.exited; i++) await sleep(50);
    if (!this.exited) { this.proc.kill("SIGKILL"); for (let i = 0; i < 40 && !this.exited; i++) await sleep(50); }
  }

  /** Trace records, as normalised wire lines from the CP's point of view. */
  wire(side: (s: string) => void): string[] {
    if (!existsSync(this.tracePath)) return [];
    const out: string[] = [];
    // CSMS-initiated calls carry library-specific ids (ULID, number, ...);
    // number them in order of appearance, including the CP's answer.
    const csmsIds = new Map<string, string>();
    for (const line of readFileSync(this.tracePath, "utf8").split("\n")) {
      if (!line.trim()) continue;
      let r: any;
      try { r = JSON.parse(line); } catch { continue; }
      const arrow = r.direction === "cp-to-csms" ? "->" : r.direction === "csms-to-cp" ? "<-" : String(r.direction);
      let body: unknown = r.raw;
      if (typeof r.raw === "string") {
        try {
          const f = JSON.parse(r.raw);
          if (Array.isArray(f) && typeof f[1] === "string") {
            if (r.direction === "csms-to-cp" && f[0] === 2 && !csmsIds.has(f[1])) csmsIds.set(f[1], `<csms-call-${csmsIds.size + 1}>`);
            if (csmsIds.has(f[1])) f[1] = csmsIds.get(f[1]);
          }
          body = normFrame(f, side);
        } catch { body = norm(r.raw); }
      }
      out.push(`${arrow} ${stable(body)}`);
    }
    return out;
  }
}

// ------------------------------------------------------- raw websocket client

export interface RawWsResult { opened: boolean; protocol: string; closeCode: number | null; closeReason: string; error: string | null; replies: unknown[]; serverClosedFirst: boolean }

/** Tiny raw client, used only for handshake-level cases the simulator cannot
 *  express (subprotocol, path variants). */
export async function rawWs(url: string, protocols: string[], send: string[] = [], waitMs = 1500): Promise<RawWsResult> {
  const res: RawWsResult = { opened: false, protocol: "", closeCode: null, closeReason: "", error: null, replies: [], serverClosedFirst: false };
  let weClosed = false;
  await new Promise<void>((done) => {
    let ws: WebSocket;
    try { ws = new WebSocket(url, protocols); } catch (e) { res.error = String(e); return done(); }
    const t = setTimeout(() => { try { ws.close(); } catch { /* */ } done(); }, waitMs + 1500);
    ws.onopen = () => {
      res.opened = true; res.protocol = ws.protocol;
      for (const s of send) ws.send(s);
      setTimeout(() => { weClosed = true; try { ws.close(1000); } catch { /* */ } }, waitMs);
    };
    ws.onmessage = (m) => { try { res.replies.push(norm(JSON.parse(String(m.data)))); } catch { res.replies.push(String(m.data)); } };
    ws.onerror = () => { res.error ??= "error"; };
    ws.onclose = (e) => { res.serverClosedFirst = !weClosed; res.closeCode = e.code; res.closeReason = e.reason; clearTimeout(t); done(); };
  });
  return res;
}

export function ensureDir(p: string) { mkdirSync(dirname(p), { recursive: true }); }
