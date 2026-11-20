// Group G: OCPP 2.1 against cs-gocpp only (ocpp-go has no 2.1). Each case
// records pass/fail checks (Ctx.check); the report prints a table per flow.
import type { Case } from "./cases";
import type { Ctx } from "./ctx";
import { norm, sleep } from "./lib";
import type { Sim } from "./lib";

const ts = "2026-01-01T00:00:00Z";
const tok = (t: string) => ({ idToken: t, type: "ISO14443" });
const call = (action: string, payload: unknown, id = "id1") => JSON.stringify([2, id, action, payload]);
const send = (action: string, payload: unknown, id = "s1") => JSON.stringify([6, id, action, payload]);

const add = (list: Case[], c: Omit<Case, "version" | "libs" | "group">) => list.push({ ...c, group: "G", version: "2.1", libs: ["gocpp"] });
export const cases21: Case[] = [];

// ------------------------------------------------------------ helpers

/** Server events as [kind, action] for the CP. */
async function evs(c: Ctx) {
  const e = await c.server.events();
  return e.filter((x: any) => x.kind === "request" || x.kind === "response_sent").map((x: any) => `${x.kind}:${x.action}`);
}
const normFrames = (frames: any[]) => frames.map((f) => (Array.isArray(f) && typeof f[1] === "string" && /^[0-9A-Za-z-]{20,}$/.test(f[1]) ? [f[0], "<id>", ...f.slice(2)] : f));

// ------------------------------------------------------------ simulator flows

add(cases21, {
  id: "G-sim-flow", desc: "TypeScript simulator, OCPP 2.1: boot, status, authorize, start tx, meter values, stop tx, heartbeat", timeoutMs: 40000,
  async run(c) {
    const s = c.sim("a");
    c.check("connect + BootNotification accepted", await c.boot(s));
    const ok = async (name: string, cmd: string, params: unknown = {}) => {
      const r = await s.cmd(cmd, params, 10000);
      c.check(name, r.ok === true && !r.timeout, r.error ?? r.data);
      return r;
    };
    await ok("StatusNotification (update_connector_status)", "update_connector_status", { connector: 1, status: "Available" });
    await ok("Authorize", "authorize", { tagId: "TAG1" });
    await ok("transaction start (TransactionEvent Started)", "start_transaction", { connector: 1, tagId: "TAG1" });
    await sleep(500);
    await ok("MeterValues", "send_meter_value", { connector: 1 });
    await ok("transaction stop (TransactionEvent Ended)", "stop_transaction", { connector: 1 });
    await ok("Heartbeat", "heartbeat");
    await sleep(800);
    const e = await evs(c);
    for (const a of ["BootNotification", "StatusNotification", "Authorize", "TransactionEvent", "MeterValues", "Heartbeat"]) c.check(`server received ${a}`, e.includes(`request:${a}`));
    const txs = (await c.server.events()).filter((x: any) => x.kind === "request" && x.action === "TransactionEvent").map((x: any) => x.payload?.eventType);
    c.check("TransactionEvent Started then Ended", txs[0] === "Started" && txs.includes("Ended"), txs);
    const bad = (await c.server.events()).filter((x: any) => x.kind === "log" || x.kind === "error");
    c.check("no gocpp warn/error log during the flow", bad.length === 0, bad.map((b: any) => b.msg));
  },
});

add(cases21, {
  id: "G-sim-idtag-rules", desc: "simulator, OCPP 2.1: Authorize with INVALID/BLOCKED/EXPIRED tokens",
  async run(c) {
    const s = c.sim("a");
    c.check("boot", await c.boot(s));
    for (const [t, want] of [["INVALID1", "Invalid"], ["BLOCKED1", "Blocked"], ["EXPIRED1", "Expired"], ["OK1", "Accepted"]] as const) {
      const r = await s.cmd("send_ocpp_call", { action: "Authorize", payload: { idToken: tok(t) } }, 8000);
      const st = (r.data as any)?.payload?.idTokenInfo?.status;
      c.check(`Authorize ${t} -> ${want}`, st === want, r.data);
    }
  },
});

const actions21: [string, unknown, boolean?][] = [
  ["RequestStartTransaction", { idToken: tok("TAG1"), remoteStartId: 1, evseId: 1 }],
  ["RequestStopTransaction", { transactionId: "<tx>" }, true],
  ["Reset", { type: "Immediate" }],
  ["GetVariables", { getVariableData: [{ component: { name: "OCPPCommCtrlr" }, variable: { name: "HeartbeatInterval" } }] }],
  ["SetVariables", { setVariableData: [{ attributeValue: "60", component: { name: "OCPPCommCtrlr" }, variable: { name: "HeartbeatInterval" } }] }],
  ["ChangeAvailability", { operationalStatus: "Inoperative", evse: { id: 1 } }],
  ["UnlockConnector", { evseId: 1, connectorId: 1 }],
  ["ClearCache", {}],
  ["DataTransfer", { vendorId: "acme", messageId: "m1", data: "hello" }],
  ["TriggerMessage", { requestedMessage: "Heartbeat" }],
  ["SetChargingProfile", { evseId: 1, chargingProfile: { id: 1, stackLevel: 0, chargingProfilePurpose: "TxDefaultProfile", chargingProfileKind: "Absolute", chargingSchedule: [{ id: 1, chargingRateUnit: "W", chargingSchedulePeriod: [{ startPeriod: 0, limit: 1000 }] }] } }],
  ["GetBaseReport", { requestId: 1, reportBase: "FullInventory" }],
  ["GetCompositeSchedule", { duration: 60, evseId: 1 }],
  ["GetLocalListVersion", {}],
  ["UsePriorityCharging", { transactionId: "<tx>", activate: true }, true],
  ["GetTariffs", { evseId: 1 }],
  ["ClearTariffs", {}],
  ["SetDefaultTariff", { evseId: 1, tariff: { tariffId: "T1", currency: "EUR", energy: { prices: [{ priceKwh: 0.3 }] } } }],
  ["NotifyAllowedEnergyTransfer", { transactionId: "<tx>", allowedEnergyTransfer: ["AC_single_phase"] }, true],
];
async function resolve(c: Ctx, p: unknown) {
  const j = JSON.stringify(p);
  if (!j.includes("<tx>")) return p;
  let tx = "none";
  for (let i = 0; i < 20; i++) {
    const e = (await c.server.events()).find((x: any) => x.kind === "request" && x.action === "TransactionEvent" && x.payload?.transactionInfo?.transactionId);
    if (e) { tx = e.payload.transactionInfo.transactionId; break; }
    await sleep(100);
  }
  return JSON.parse(j.replace("<tx>", tx));
}
for (const [action, payload, needsTx] of actions21) {
  add(cases21, {
    id: `G-sim-csms-${action}`, desc: `CSMS-initiated ${action} answered by the simulator's 2.1 engine`, timeoutMs: 40000,
    async run(c) {
      const s = c.sim("a");
      c.check("boot", await c.boot(s));
      if (needsTx) { await s.cmd("start_transaction", { connector: 1, tagId: "TAG1" }, 8000); await sleep(500); }
      const r: any = await c.server.call(c.cpId, action, await resolve(c, payload));
      const j = r.json;
      c.check(`${action}: CP answer relayed (CALLRESULT or CP-side CALLERROR)`, j?.ok === true || !!j?.callError, j?.callError ?? j?.error ?? j?.result);
      if (j?.callError) c.rec("cpCallError", j.callError.code);
      c.rec("control", c.ctl(r));
      await sleep(500);
    },
  });
}


// ------------------------------------------------------------ frames

const BOOTP = { reason: "PowerUp", chargingStation: { model: "M", vendorName: "V" } };
const TXP = (over: object = {}, tx: object = {}) => ({ eventType: "Started", timestamp: ts, triggerReason: "Authorized", seqNo: 0, transactionInfo: { transactionId: "tx1", ...tx }, ...over });
interface FrameCase { id: string; desc: string; frame: string; expect: (reply: any) => boolean; why: string }
const frameCases: FrameCase[] = [
  { id: "boot", desc: "valid BootNotification", frame: call("BootNotification", BOOTP, "f1"), expect: (r) => r?.[0] === 3 && r[2]?.status === "Accepted", why: "CALLRESULT Accepted" },
  { id: "tx-idtoken", desc: "TransactionEvent with idToken", frame: call("TransactionEvent", TXP({ idToken: tok("BLOCKED1") }), "f1"), expect: (r) => r?.[0] === 3 && r[2]?.idTokenInfo?.status === "Blocked", why: "CALLRESULT with idTokenInfo Blocked" },
  { id: "tx-seqno-string", desc: 'TransactionEvent seqNo "0"', frame: call("TransactionEvent", TXP({ seqNo: "0" }), "f1"), expect: (r) => r?.[0] === 4 || r?.[0] === 3, why: "CALLERROR, or CALLRESULT if the lenient schema mode coerces \"0\" to 0" },
  { id: "tx-ts-nanos", desc: "TransactionEvent timestamp with 9 fractional digits", frame: call("TransactionEvent", TXP({ timestamp: "2026-01-01T00:00:00.123456789Z" }), "f1"), expect: (r) => r?.[0] === 3 || r?.[0] === 4, why: "an answer (lenient accept or CALLERROR)" },
  { id: "boot-missing-reason", desc: "BootNotification without reason", frame: call("BootNotification", { chargingStation: BOOTP.chargingStation }, "f1"), expect: (r) => r?.[0] === 4, why: "CALLERROR" },
  { id: "unknown-action", desc: "unknown action Foo", frame: call("Foo", {}, "f1"), expect: (r) => r?.[0] === 4 && r[2] === "NotImplemented", why: "CALLERROR NotImplemented" },
  { id: "unsupported-in-handler", desc: "SecurityEventNotification (no handler in the test server)", frame: call("SecurityEventNotification", { type: "FirmwareUpdated", timestamp: ts }, "f1"), expect: (r) => r?.[0] === 4 && r[2] === "NotSupported", why: "CALLERROR NotSupported" },
  { id: "v16-action", desc: "StartTransaction (1.6 only action) on a 2.1 connection", frame: call("StartTransaction", { connectorId: 1, idTag: "T", meterStart: 0, timestamp: ts }, "f1"), expect: (r) => r?.[0] === 4 && (r[2] === "NotSupported" || r[2] === "NotImplemented"), why: "CALLERROR NotSupported/NotImplemented" },
  { id: "not-json", desc: "text frame that is not JSON", frame: "not json", expect: (r) => r == null || r?.[0] === 4, why: "ignored or CALLERROR" },
  { id: "callresulterror-type-5-as-first", desc: "unsolicited CALLRESULTERROR [5, id, ...] (no pending call)", frame: '[5,"zz","FormatViolation","bad",{}]', expect: (r) => r == null, why: "no reply, connection stays" },
];
for (const f of frameCases) {
  add(cases21, {
    id: `G-frame-${f.id}`, desc: `${f.desc} [simulator send_raw_frame]; expected: ${f.why}`,
    async run(c) {
      const s = c.sim("a");
      c.check("boot", await c.boot(s));
      const r: any = await s.cmd("send_raw_frame", { frame: f.frame, waitMs: 900 }, 6000);
      const reply = r.data?.reply ?? null;
      c.check(`reply is ${f.why}`, f.expect(reply), reply);
      c.rec("reply", norm(reply));
      const p: any = await s.cmd("send_raw_frame", { frame: call("Heartbeat", {}, "probe1"), waitMs: 900 }, 6000);
      c.check("connection still usable (Heartbeat answered)", p.data?.reply?.[0] === 3, p.data);
    },
  });
}

// ------------------------------------------------------------ SEND (message type 6)

const PERIODIC = { id: 1, pending: 0, basetime: ts, data: [{ t: 0, v: "1" }] };
for (const [id, frame, desc, expectEvent] of [
  ["valid", send("NotifyPeriodicEventStream", PERIODIC, "s1"), "valid NotifyPeriodicEventStream SEND", true],
  ["customdata", send("NotifyPeriodicEventStream", { ...PERIODIC, customData: { vendorId: "x" } }, "s1"), "SEND with customData", true],
  ["missing-required", send("NotifyPeriodicEventStream", { id: 1, pending: 0 }, "s1"), "SEND without basetime/data (schema-invalid)", false],
  ["wrong-type", send("NotifyPeriodicEventStream", { ...PERIODIC, id: "1" }, "s1"), 'SEND with id "1" (string; the lenient schema mode coerces it)', "either"],
  ["unknown-action", send("FooStream", {}, "s1"), "SEND of an unknown action", false],
  ["call-action-as-send", send("Heartbeat", {}, "s1"), "SEND whose action is a CALL action (Heartbeat)", false],
  ["no-payload", '[6,"s1","NotifyPeriodicEventStream"]', "SEND with 3 elements", false],
  ["numeric-id", '[6,7,"NotifyPeriodicEventStream",{}]', "SEND with a numeric message id", false],
] as [string, string, string, boolean | "either"][]) {
  add(cases21, {
    id: `G-send-${id}`, desc: `${desc} [simulator send_raw_frame]; a SEND is unconfirmed: no frame must come back and the connection must stay up`,
    async run(c) {
      const s = c.sim("a");
      c.check("boot", await c.boot(s));
      await c.server.http("/control/events/reset", {});
      const r: any = await s.cmd("send_raw_frame", { frame, waitMs: 900 }, 6000);
      const replies = r.data?.reply ? [r.data.reply] : [];
      c.check("no reply frame to a SEND (neither CALLRESULT nor CALLERROR)", replies.length === 0, replies);
      const e = await evs(c);
      if (expectEvent === "either") c.rec("handlerInvoked", e.includes("request:NotifyPeriodicEventStream"));
      else c.check(expectEvent ? "handler invoked (request event recorded)" : "handler not invoked for an invalid SEND", expectEvent ? e.includes("request:NotifyPeriodicEventStream") : !e.includes("request:NotifyPeriodicEventStream"), e);
      const p: any = await s.cmd("send_raw_frame", { frame: call("Heartbeat", {}, "probe1"), waitMs: 900 }, 6000);
      c.check("connection still usable", p.data?.reply?.[0] === 3, p.data);
      c.check("socket not closed by the server", !s.events.some((e) => e.event === "disconnected"));
    },
  });
}

add(cases21, {
  id: "G-server-disconnect", desc: "server-initiated disconnect on a 2.1 connection",
  async run(c) {
    const s = c.sim("a");
    c.check("boot", await c.boot(s));
    const d: any = await c.server.http("/control/disconnect", { cpId: c.cpId });
    c.check("disconnect ok", d.json?.ok === true, d.json);
    await sleep(500);
    c.check("server forgot the connection", !((await c.server.connections()) ?? []).includes(c.cpId));
    const code = s.events.find((e) => e.event === "disconnected")?.data?.code;
    c.check("simulator saw a normal close (1000)", code === 1000, code);
  },
});
add(cases21, {
  id: "G-replace-same-cpid", desc: "second 2.1 connection with the same cp id (last wins)",
  async run(c) {
    const a = c.sim("a"); c.check("boot A", await c.boot(a));
    const b = c.sim("b"); c.check("boot B", await c.boot(b));
    await sleep(150);
    const conns = (await c.server.connections()) ?? [];
    c.check("exactly one connection registered", conns.filter((x) => x === c.cpId).length === 1, conns);
    const p: any = await b.cmd("send_raw_frame", { frame: call("Heartbeat", {}, "p1"), waitMs: 500 }, 5000);
    c.check("second connection usable", p.data?.reply?.[0] === 3, p.data);
  },
});
