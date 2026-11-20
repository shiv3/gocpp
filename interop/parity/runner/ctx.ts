// Per-case context: drives simulators against one server and builds the
// normalised observation.
import { join } from "node:path";
import { norm, normFrame, sleep, Server, Sim, type Lib, type SimResp } from "./lib";

export interface Obs {
  steps: [string, unknown][];
  wire: Record<string, string[]>;
  simEvents: Record<string, string[]>;
  serverEvents: unknown[];
  serverAlive: boolean;
}
export interface Observed { obs: Obs; diag: unknown[]; side: Record<string, unknown> }

export class Ctx {
  steps: [string, unknown][] = [];
  side: Record<string, unknown> = {};
  sims: Sim[] = [];
  private sideTexts: string[] = [];
  cpId: string;
  constructor(public lib: Lib, public server: Server, public dir: string, public caseId: string) {
    this.cpId = `P-${caseId}`;
  }
  get url() { return `ws://127.0.0.1:${this.server.port}/ocpp`; }
  get wsBase() { return `ws://127.0.0.1:${this.server.port}/ocpp`; }

  sim(name = "a", cpId = this.cpId, extra: string[] = []): Sim {
    const s = new Sim(name, cpId, this.url, this.dir, extra);
    s.start();
    this.sims.push(s);
    return s;
  }
  rec(label: string, value: unknown) { this.steps.push([label, value]); }
  note(key: string, value: unknown) { this.side[key] = value; }
  noteText(s: string) { this.sideTexts.push(s); }
  private sideFn = (s: string) => { this.sideTexts.push(s); };

  /** Normalise a simulator command response. */
  simResp(r: SimResp): unknown {
    if (r.timeout) return { timeout: true };
    if (r.ok === false || r.error) return { error: norm(String(r.error ?? "").replace(/\s+/g, " ").slice(0, 200)) };
    const d = r.data;
    if (d && typeof d === "object" && "kind" in d) {
      // send_ocpp_call outcome
      const o: any = { kind: d.kind };
      if (d.kind === "callResult") o.payload = norm(d.payload);
      else { o.errorCode = d.errorCode; o.errorDetails = norm(d.errorDetails); this.sideTexts.push(String(d.errorDescription)); }
      return o;
    }
    return { ok: true, data: d === undefined ? undefined : norm(d) };
  }
  rawResp(r: SimResp): unknown {
    if (r.timeout || r.ok === false || r.error) return this.simResp(r);
    const d = r.data;
    return { reply: d.reply == null ? null : normFrame(d.reply, this.sideFn), closed: d.closed };
  }

  async cmd(sim: Sim, command: string, params: unknown = {}, timeoutMs = 10000): Promise<unknown> {
    return this.simResp(await sim.cmd(command, params, timeoutMs));
  }
  async raw(sim: Sim, frame: string, waitMs = 1500): Promise<unknown> {
    return this.rawResp(await sim.cmd("send_raw_frame", { frame, waitMs }, waitMs + 5000));
  }
  /** Connect and wait for the BootNotification answer. */
  async boot(sim: Sim, bootMs = 3000): Promise<boolean> {
    const c = await sim.cmd("connect", {}, 8000);
    if (c.timeout || c.ok === false) return false;
    const b = await sim.waitEvent("boot_notification", bootMs);
    await sleep(300);
    return !!b;
  }
  /** Normalise a control API answer; library-specific timeout texts and CallError
   *  descriptions go to the side field. */
  ctl(r: any): unknown {
    if (!r.json) return r;
    const j = { ...r.json };
    if (j.callError) { this.sideTexts.push(String(j.callError.description)); j.callError = { code: j.callError.code }; }
    if (typeof j.error === "string" && j.error !== "unsupported" && j.kind !== "not_connected") { this.sideTexts.push(`[control error] ${j.error}`); j.error = "<text>"; }
    return norm(j);
  }
  /** Raw-client handshake result; the close reason text is library specific. */
  hs(r: { closeReason: string }): unknown {
    this.sideTexts.push(`[close reason] ${r.closeReason}`);
    return { ...r, closeReason: undefined };
  }
  async settle(ms: number) { await sleep(ms); }

  async finish(): Promise<Observed> {
    const alive = await this.server.alive();
    const evs = alive ? await this.server.events() : await readEventsFile(this.server.eventsFile);
    const serverEvents: unknown[] = [];
    const diag: unknown[] = [];
    for (const e of evs) {
      if (e.kind === "log" || e.kind === "error") {
        diag.push({ kind: e.kind, level: e.level });
        this.sideTexts.push(`[server ${e.kind}] ${e.level ?? ""} ${e.msg ?? e.detail ?? ""} ${JSON.stringify(e.attrs ?? {})}`);
        continue;
      }
      const o: Record<string, unknown> = { kind: e.kind, cpId: e.cpId };
      if (e.action) o.action = e.action;
      if (e.cause) o.cause = e.cause;
      if (e.kind === "request") o.payload = dropEmpty(norm(e.payload));
      else if (e.kind === "response_sent") o.payload = norm(e.payload);
      if (e.kind === "response_sent" && typeof e.detail === "string" && e.detail) {
        o.err = e.detail.startsWith("error");
        this.sideTexts.push(`[response_sent detail] ${e.detail}`);
      }
      if (e.kind === "disconnect" && e.detail) this.sideTexts.push(`[disconnect detail] ${e.cpId}: ${e.detail}`);
      serverEvents.push(o);
    }
    const wire: Record<string, string[]> = {};
    const simEvents: Record<string, string[]> = {};
    for (const s of this.sims) {
      simEvents[s.name] = s.events
        .filter((e) => e.event === "connected" || e.event === "disconnected")
        .map((e) => (e.event === "disconnected" ? `disconnected(${e.data?.code})` : "connected"));
      for (const e of s.events) if (e.event === "disconnected" && e.data?.reason) this.sideTexts.push(`[sim ${s.name} close reason] ${e.data.reason}`);
    }
    for (const s of this.sims) await s.end();
    for (const s of this.sims) wire[s.name] = s.wire(this.sideFn);
    this.side.texts = [...new Set(this.sideTexts)];
    return { obs: { steps: this.steps, wire, simEvents, serverEvents, serverAlive: alive }, diag, side: this.side };
  }
}

async function readEventsFile(path: string): Promise<any[]> {
  try {
    const t = await Bun.file(path).text();
    return t.split("\n").filter(Boolean).map((l) => JSON.parse(l));
  } catch { return []; }
}

/** The request events are the library's decoded struct re-encoded; one library
 *  keeps empty strings, the other drops them (omitempty). Not a wire
 *  difference, so empty strings and nulls are dropped before comparing. */
function dropEmpty(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(dropEmpty);
  if (v && typeof v === "object") {
    const o: Record<string, unknown> = {};
    for (const [k, x] of Object.entries(v as object)) if (x !== "" && x !== null) o[k] = dropEmpty(x);
    return o;
  }
  return v;
}
