// Case table. Every case runs once per server with a fresh server process.
import type { Ctx } from "./ctx";
import { rawWs, sleep, type Lib, type Version } from "./lib";
import { scenarioCases, scenarioCases201, scripts, scripts201 } from "./scenarios";
import { cases21 } from "./cases21";

export interface Case {
  id: string;
  group: "A" | "B" | "C" | "D" | "E" | "G";
  /** wire version the case speaks (default 1.6) */
  version?: Version;
  /** libraries it runs against (default both; the 2.1 cases are gocpp only) */
  libs?: Lib[];
  desc: string;
  slow?: boolean;
  skip?: string;
  timeoutMs?: number;
  run(c: Ctx): Promise<void>;
}

export const cases: Case[] = [];

/** What differs between the wire versions the A-D groups are written for. */
interface Ver {
  v: Version;
  /** id infix: "" for 1.6 (ids unchanged), "201-" for 2.0.1 */
  tag: string;
  /** a subprotocol that is not this version's */
  other: string;
  /** a CSMS-initiated action every charge point answers trivially */
  ping: string;
  /** CSMS-initiated action answered with a fixed payload by the simulator */
  csms: [string, unknown, boolean?][];
}
let cur: Ver;
const add = (c: Case) => {
  cases.push({ ...c, id: cur.tag ? c.id.replace(/^([A-Z])-/, `$1-${cur.tag}`) : c.id, version: cur.v });
};

const pad = (n: number) => "a".repeat(n);
const call = (action: string, payload: unknown, id = "id1") => JSON.stringify([2, id, action, payload]);
const tokObj = (t: string) => ({ idToken: t, type: "ISO14443" });
const ts = "2026-01-01T00:00:00Z";

// ------------------------------------------------------- CSMS-initiated calls

const CP_START = { connectorId: 1, idTag: "TAG1" };
const actions16: [string, unknown, boolean?][] = [
  ["RemoteStartTransaction", CP_START],
  ["RemoteStopTransaction", { transactionId: 1 }, true],
  ["Reset", { type: "Soft" }],
  ["ChangeConfiguration", { key: "HeartbeatInterval", value: "60" }],
  ["GetConfiguration", { key: ["HeartbeatInterval"] }],
  ["ChangeAvailability", { connectorId: 1, type: "Inoperative" }],
  ["UnlockConnector", { connectorId: 1 }],
  ["ClearCache", {}],
  ["DataTransfer", { vendorId: "acme", messageId: "m1", data: "hello" }],
  ["TriggerMessage", { requestedMessage: "Heartbeat" }],
  ["ReserveNow", { connectorId: 1, expiryDate: "2030-01-01T00:00:00Z", idTag: "TAG1", reservationId: 1 }],
  ["CancelReservation", { reservationId: 1 }],
  ["SetChargingProfile", { connectorId: 1, csChargingProfiles: { chargingProfileId: 1, stackLevel: 0, chargingProfilePurpose: "TxDefaultProfile", chargingProfileKind: "Absolute", chargingSchedule: { chargingRateUnit: "W", chargingSchedulePeriod: [{ startPeriod: 0, limit: 1000 }] } } }],
  ["ClearChargingProfile", {}],
  ["GetCompositeSchedule", { connectorId: 1, duration: 60 }],
  ["GetLocalListVersion", {}],
  ["SendLocalList", { listVersion: 1, updateType: "Full", localAuthorizationList: [{ idTag: "TAG1", idTagInfo: { status: "Accepted" } }] }],
  ["UpdateFirmware", { location: "http://127.0.0.1:1/fw.bin", retrieveDate: "2030-01-01T00:00:00Z" }],
  ["GetDiagnostics", { location: "ftp://127.0.0.1/diag" }],
];
/** `{ tx }` in a payload is replaced by the transaction id the CP reported. */
export const actions2x: [string, unknown, boolean?][] = [
  ["RequestStartTransaction", { idToken: tokObj("TAG1"), remoteStartId: 1, evseId: 1 }],
  ["RequestStopTransaction", { transactionId: "<tx>" }, true],
  ["Reset", { type: "Immediate" }],
  ["GetVariables", { getVariableData: [{ component: { name: "OCPPCommCtrlr" }, variable: { name: "HeartbeatInterval" } }] }],
  ["SetVariables", { setVariableData: [{ attributeValue: "60", component: { name: "OCPPCommCtrlr" }, variable: { name: "HeartbeatInterval" } }] }],
  ["ChangeAvailability", { operationalStatus: "Inoperative", evse: { id: 1 } }],
  ["UnlockConnector", { evseId: 1, connectorId: 1 }],
  ["ClearCache", {}],
  ["DataTransfer", { vendorId: "acme", messageId: "m1", data: "hello" }],
  ["TriggerMessage", { requestedMessage: "Heartbeat" }],
  ["ReserveNow", { id: 1, expiryDateTime: "2030-01-01T00:00:00Z", idToken: tokObj("TAG1") }],
  ["CancelReservation", { reservationId: 1 }],
  ["SetChargingProfile", { evseId: 1, chargingProfile: { id: 1, stackLevel: 0, chargingProfilePurpose: "TxDefaultProfile", chargingProfileKind: "Absolute", chargingSchedule: [{ id: 1, chargingRateUnit: "W", chargingSchedulePeriod: [{ startPeriod: 0, limit: 1000 }] }] } }],
  ["ClearChargingProfile", {}],
  ["GetCompositeSchedule", { duration: 60, evseId: 1 }],
  ["GetLocalListVersion", {}],
  ["SendLocalList", { versionNumber: 1, updateType: "Full", localAuthorizationList: [{ idToken: tokObj("TAG1"), idTokenInfo: { status: "Accepted" } }] }],
  ["UpdateFirmware", { requestId: 1, firmware: { location: "http://127.0.0.1:1/fw.bin", retrieveDateTime: "2030-01-01T00:00:00Z" } }],
  ["GetLog", { logType: "DiagnosticsLog", requestId: 1, log: { remoteLocation: "ftp://127.0.0.1/diag" } }],
  ["GetBaseReport", { requestId: 1, reportBase: "FullInventory" }],
  ["GetTransactionStatus", {}],
  ["GetDisplayMessages", { requestId: 1 }],
];
const V16: Ver = { v: "1.6", tag: "", other: "ocpp2.0.1", ping: "GetLocalListVersion", csms: actions16 };
const V201: Ver = { v: "2.0.1", tag: "201-", other: "ocpp1.6", ping: "GetLocalListVersion", csms: actions2x };
export const VERS = [V16, V201];

/** Resolve "<tx>" placeholders with the transaction id from the CP's first TransactionEvent. */
export async function resolvePayload(c: Ctx, p: unknown): Promise<unknown> {
  if (!JSON.stringify(p).includes("<tx>")) return p;
  let tx = "none";
  for (let i = 0; i < 20; i++) {
    const ev = await c.server.events();
    const e = ev.find((x: any) => x.kind === "request" && x.action === "TransactionEvent" && x.payload?.transactionInfo?.transactionId);
    if (e) { tx = e.payload.transactionInfo.transactionId; break; }
    await sleep(100);
  }
  return JSON.parse(JSON.stringify(p).replace("<tx>", tx));
}

function defineABCD(V: Ver) {
  cur = V;
  const is16 = V.v === "1.6";
  const HB = (id = "id1") => call("Heartbeat", {}, id);

  // ---------------------------------------------------------------- group A

  function rawCase(id: string, desc: string, frame: string, waitMs = 1500, quiet = false) {
    add({
      id: `A-${id}`, group: "A", desc,
      async run(c) {
        const s = c.sim("a");
        c.rec("boot", await c.boot(s));
        const fr = await c.raw(s, frame, waitMs);
        // quiet: the simulator sometimes drops the result line for very large replies; the wire trace is the evidence.
        c.rec("frame", quiet ? "<not recorded, see wire>" : fr);
        // Liveness probe: is the connection still usable afterwards?
        c.rec("probe", await c.raw(s, call("Heartbeat", {}, "probe1"), waitMs));
      },
    });
  }

  // frame level (the payload is a Heartbeat or irrelevant): identical for both versions
  rawCase("not-json", "text frame that is not JSON", "not json");
  rawCase("json-object", "JSON object instead of array", '{"a":1}');
  rawCase("empty-array", "empty array", "[]");
  rawCase("only-type", "[2]", "[2]");
  rawCase("type-id", '[2,"id"]', '[2,"id"]');
  rawCase("three-elems", 'Call with 3 elements', '[2,"id","Heartbeat"]');
  rawCase("five-elems", "Call with 5 elements", '[2,"id","Heartbeat",{},"extra"]');
  rawCase("numeric-id", "numeric message id", '[2,123,"Heartbeat",{}]');
  rawCase("action-not-string", "action is a number", '[2,"id",5,{}]');
  rawCase("unknown-action", "unknown action Foo", call("Foo", {}));
  rawCase("unknown-type", "unknown message type 9", '[9,"id","Heartbeat",{}]');
  rawCase("type-5-callresulterror", "OCPP 2.1 CALLRESULTERROR frame [5,...] on a non-2.1 connection", '[5,"id","GenericError","d",{}]');
  rawCase("type-6-send", "OCPP 2.1 SEND frame [6,...] on a non-2.1 connection", '[6,"id","Heartbeat",{}]');
  rawCase("payload-array", "payload is an array", '[2,"id","Heartbeat",[]]');
  rawCase("payload-null", "payload is null", '[2,"id","Heartbeat",null]');
  rawCase("empty-id", "empty message id", '[2,"","Heartbeat",{}]');
  rawCase("long-id", "message id of 40 chars (spec max 36)", call("Heartbeat", {}, "x".repeat(40)));
  rawCase("hb-extra-prop", "Heartbeat with extra property", call("Heartbeat", { x: 1 }));
  rawCase("callresult-unknown-id", "CallResult for unknown id", '[3,"nope",{}]');
  rawCase("callerror-unknown-id", "CallError for unknown id", '[4,"nope","GenericError","d",{}]');
  rawCase("callerror-4-elems", "CallError with only 4 elements", '[4,"nope","GenericError","d"]');
  rawCase("big-frame-1.1MiB", "valid-looking DataTransfer frame of ~1.1 MiB (over 1 MiB read limit)", call("DataTransfer", { vendorId: "v", data: pad(Math.floor(1.1 * 1048576)) }, "big"), 4000);
  rawCase("big-frame-900KiB", "valid DataTransfer frame of ~900 KiB", call("DataTransfer", { vendorId: "v", data: pad(900 * 1024) }, "big"), 4000, true);
  rawCase("datatransfer-data-object", "DataTransfer data as JSON object", call("DataTransfer", { vendorId: "v", data: { a: 1 } }));

  if (is16) {
    const START = { connectorId: 1, idTag: "T", meterStart: 0 };
    rawCase("boot-extra-prop", "BootNotification with extra property", call("BootNotification", { chargePointVendor: "V", chargePointModel: "M", bogus: 1 }));
    rawCase("boot-missing-vendor", "BootNotification missing chargePointVendor", call("BootNotification", { chargePointModel: "M" }));
    rawCase("status-connid-string", 'StatusNotification connectorId "1"', call("StatusNotification", { connectorId: "1", errorCode: "NoError", status: "Available" }));
    rawCase("status-enum-case", 'StatusNotification errorCode "noerror"', call("StatusNotification", { connectorId: 1, errorCode: "noerror", status: "Available" }));
    rawCase("status-enum-unknown", "StatusNotification unknown status", call("StatusNotification", { connectorId: 1, errorCode: "NoError", status: "Foo" }));
    rawCase("authorize-idtag-21", "Authorize idTag 21 chars (maxLength 20)", call("Authorize", { idTag: pad(21) }));
    rawCase("authorize-missing-idtag", "Authorize missing idTag", call("Authorize", {}));
    rawCase("start-ts-space", 'StartTransaction timestamp "2026-01-01 00:00:00"', call("StartTransaction", { ...START, timestamp: "2026-01-01 00:00:00" }));
    rawCase("start-ts-offset-frac", 'StartTransaction timestamp "2026-01-01T09:00:00.123+09:00"', call("StartTransaction", { ...START, timestamp: "2026-01-01T09:00:00.123+09:00" }));
    rawCase("start-ts-no-zone", 'StartTransaction timestamp without zone', call("StartTransaction", { ...START, timestamp: "2026-01-01T00:00:00" }));
    rawCase("start-ts-nanos", 'StartTransaction timestamp with 9 fractional digits', call("StartTransaction", { ...START, timestamp: "2026-01-01T00:00:00.123456789Z" }));
    rawCase("start-ts-ok", "StartTransaction with a valid Z timestamp (control)", call("StartTransaction", { ...START, timestamp: "2026-01-01T00:00:00Z" }));
    rawCase("meter-value-number", "MeterValues sampledValue.value as number", call("MeterValues", { connectorId: 1, meterValue: [{ timestamp: "2026-01-01T00:00:00Z", sampledValue: [{ value: 123 }] }] }));
  } else {
    const TX = (over: object = {}, ev: object = {}) => ({ eventType: "Started", timestamp: ts, triggerReason: "Authorized", seqNo: 0, transactionInfo: { transactionId: "tx1", ...over }, ...ev });
    rawCase("boot-extra-prop", "BootNotification with extra property", call("BootNotification", { reason: "PowerUp", chargingStation: { model: "M", vendorName: "V" }, bogus: 1 }));
    rawCase("boot-missing-reason", "BootNotification missing reason", call("BootNotification", { chargingStation: { model: "M", vendorName: "V" } }));
    rawCase("boot-missing-station", "BootNotification missing chargingStation", call("BootNotification", { reason: "PowerUp" }));
    rawCase("boot-reason-unknown", "BootNotification reason not in the enum", call("BootNotification", { reason: "Foo", chargingStation: { model: "M", vendorName: "V" } }));
    rawCase("boot-ok", "valid BootNotification (control)", call("BootNotification", { reason: "PowerUp", chargingStation: { model: "M", vendorName: "V" } }));
    rawCase("customdata-extra", "customData with an extra property (allowed by the schema)", call("Heartbeat", { customData: { vendorId: "x", foo: 1 } }));
    rawCase("customdata-no-vendor", "customData without vendorId", call("Heartbeat", { customData: { foo: 1 } }));
    rawCase("status-evse-string", 'StatusNotification evseId "1"', call("StatusNotification", { timestamp: ts, connectorStatus: "Available", evseId: "1", connectorId: 1 }));
    rawCase("status-enum-case", 'StatusNotification connectorStatus "available"', call("StatusNotification", { timestamp: ts, connectorStatus: "available", evseId: 1, connectorId: 1 }));
    rawCase("status-enum-unknown", "StatusNotification unknown connectorStatus", call("StatusNotification", { timestamp: ts, connectorStatus: "Foo", evseId: 1, connectorId: 1 }));
    rawCase("authorize-token-37", "Authorize idToken 37 chars (maxLength 36)", call("Authorize", { idToken: { idToken: pad(37), type: "ISO14443" } }));
    rawCase("authorize-missing-token", "Authorize missing idToken", call("Authorize", {}));
    rawCase("authorize-type-unknown", "Authorize idToken.type not in the enum", call("Authorize", { idToken: { idToken: "T", type: "Foo" } }));
    rawCase("authorize-idtoken-string", "Authorize idToken as a plain string", call("Authorize", { idToken: "T" }));
    rawCase("tx-ts-space", 'TransactionEvent timestamp "2026-01-01 00:00:00"', call("TransactionEvent", TX({}, { timestamp: "2026-01-01 00:00:00" })));
    rawCase("tx-ts-offset-frac", 'TransactionEvent timestamp "2026-01-01T09:00:00.123+09:00"', call("TransactionEvent", TX({}, { timestamp: "2026-01-01T09:00:00.123+09:00" })));
    rawCase("tx-ts-no-zone", "TransactionEvent timestamp without zone", call("TransactionEvent", TX({}, { timestamp: "2026-01-01T00:00:00" })));
    rawCase("tx-ts-nanos", "TransactionEvent timestamp with 9 fractional digits", call("TransactionEvent", TX({}, { timestamp: "2026-01-01T00:00:00.123456789Z" })));
    rawCase("tx-ts-ok", "valid TransactionEvent (control)", call("TransactionEvent", TX()));
    rawCase("tx-seqno-string", 'TransactionEvent seqNo "0"', call("TransactionEvent", TX({}, { seqNo: "0" })));
    rawCase("tx-seqno-float", "TransactionEvent seqNo 1.5", call("TransactionEvent", TX({}, { seqNo: 1.5 })));
    rawCase("tx-missing-txid", "TransactionEvent without transactionInfo.transactionId", call("TransactionEvent", TX({ transactionId: undefined })));
    rawCase("tx-txid-37", "TransactionEvent transactionId 37 chars (maxLength 36)", call("TransactionEvent", TX({ transactionId: pad(37) })));
    rawCase("tx-eventtype-unknown", "TransactionEvent eventType not in the enum", call("TransactionEvent", TX({}, { eventType: "Foo" })));
    rawCase("tx-with-idtoken", "TransactionEvent with idToken (answer must carry idTokenInfo)", call("TransactionEvent", TX({}, { idToken: tokObj("TAG1") })));
    rawCase("tx-with-blocked-idtoken", "TransactionEvent with a BLOCKED idToken", call("TransactionEvent", TX({}, { idToken: tokObj("BLOCKED1") })));
    rawCase("meter-value-string", "MeterValues sampledValue.value as string", call("MeterValues", { evseId: 1, meterValue: [{ timestamp: ts, sampledValue: [{ value: "123" }] }] }));
    rawCase("meter-ok", "valid MeterValues (control)", call("MeterValues", { evseId: 1, meterValue: [{ timestamp: ts, sampledValue: [{ value: 123.5 }] }] }));
    rawCase("meter-empty-array", "MeterValues with an empty meterValue array (minItems 1)", call("MeterValues", { evseId: 1, meterValue: [] }));
    rawCase("meter-value-exp", "MeterValues sampledValue.value 1e3", call("MeterValues", { evseId: 1, meterValue: [{ timestamp: ts, sampledValue: [{ value: 1e3 }] }] }));
    rawCase("meter-value-tiny", "MeterValues sampledValue.value 1e-7", '[2,"id1","MeterValues",{"evseId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":1e-7}]}]}]');
    rawCase("meter-value-bignum", "MeterValues sampledValue.value 12345678901234567890.123456789 (beyond float64 precision)", '[2,"id1","MeterValues",{"evseId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":12345678901234567890.123456789}]}]}]');
    rawCase("status-evse-huge", "StatusNotification evseId 9223372036854775807 (int64 max)", '[2,"id1","StatusNotification",{"timestamp":"2026-01-01T00:00:00Z","connectorStatus":"Available","evseId":9223372036854775807,"connectorId":1}]');
    rawCase("status-evse-overflow", "StatusNotification evseId 9223372036854775808 (int64 max + 1)", '[2,"id1","StatusNotification",{"timestamp":"2026-01-01T00:00:00Z","connectorStatus":"Available","evseId":9223372036854775808,"connectorId":1}]');
    rawCase("notifyreport-ok", "valid NotifyReport", call("NotifyReport", { requestId: 1, generatedAt: ts, seqNo: 0 }));
    rawCase("notifyreport-missing-seqno", "NotifyReport without seqNo", call("NotifyReport", { requestId: 1, generatedAt: ts }));
    rawCase("securityevent-unhandled", "SecurityEventNotification (no handler in the harness servers)", call("SecurityEventNotification", { type: "FirmwareUpdated", timestamp: ts }));
    rawCase("unsupported-for-version", "a 1.6-only action (StartTransaction) on a 2.0.1 connection", call("StartTransaction", { connectorId: 1, idTag: "T", meterStart: 0, timestamp: ts }));
  }

  add({
    id: "A-duplicate-id", group: "A", desc: "same Call id sent twice back to back",
    async run(c) {
      const s = c.sim("a");
      c.rec("boot", await c.boot(s));
      const f = call("Heartbeat", {}, "dup1");
      const [r1, r2] = await Promise.all([c.raw(s, f, 1500), c.raw(s, f, 1500)]);
      c.rec("first", r1); c.rec("second", r2);
      c.rec("probe", await c.raw(s, HB("probe1"), 1500));
    },
  });
  add({
    id: "A-duplicate-id-slow", group: "A", desc: "same Call id sent twice while the first is still being handled (DataTransfer sleep 1500)",
    async run(c) {
      const s = c.sim("a");
      c.rec("boot", await c.boot(s));
      const f = call("DataTransfer", { vendorId: "parity.sleep", data: "1500" }, "dup2");
      const p1 = c.raw(s, f, 3500);
      await sleep(300);
      const p2 = c.raw(s, f, 3500);
      c.rec("first", await p1); c.rec("second", await p2);
    },
  });
  if (is16) {
    add({
      id: "A-callerror-answering-csms-request", group: "A", desc: "CallError (4 elements) answering a real CSMS request",
      skip: "the simulator answers CSMS requests from its own engine immediately; send_raw_frame cannot race it, and there is no hook to suppress the automatic answer",
      async run() {},
    });
  }

  // ---------------------------------------------------------------- group B

  for (const [id, vendor, data] of [
    ["panic", "parity.panic", "x"], ["error", "parity.error", "x"], ["reject", "parity.reject", "x"],
  ] as const) {
    add({
      id: `B-datatransfer-${id}`, group: "B", desc: `DataTransfer ${vendor} through the simulator's data_transfer command`,
      async run(c) {
        const s = c.sim("a");
        c.rec("boot", await c.boot(s));
        c.rec("result", await c.cmd(s, "data_transfer", { vendorId: vendor, data }, 5000));
        await c.settle(800);
        c.rec("probe", await c.raw(s, HB("probe1"), 1500));
      },
    });
    add({
      id: `B-datatransfer-${id}-raw`, group: "B", desc: `DataTransfer ${vendor} as a raw frame (what comes back on the wire)`,
      async run(c) {
        const s = c.sim("a");
        c.rec("boot", await c.boot(s));
        c.rec("frame", await c.raw(s, call("DataTransfer", { vendorId: vendor, data }, "dt1"), 2500));
        c.rec("probe", await c.raw(s, HB("probe1"), 1500));
      },
    });
  }
  add({
    id: "B-sleep-concurrent-heartbeat", group: "B", desc: "DataTransfer parity.sleep 5000 on one connection, Heartbeat sent 500 ms later on the same connection",
    timeoutMs: 30000,
    async run(c) {
      const s = c.sim("a");
      c.rec("boot", await c.boot(s));
      const t0 = Date.now();
      const sleepP = c.raw(s, call("DataTransfer", { vendorId: "parity.sleep", data: "5000" }, "sl1"), 7000);
      await sleep(500);
      let hbDoneAt = 0;
      const hbP = c.raw(s, HB("hb1"), 3000).then((r) => { hbDoneAt = Date.now() - t0; return r; });
      const hb = await hbP;
      c.rec("heartbeat", hb);
      c.rec("heartbeatAnsweredWhileSleeping", (hb as any)?.reply != null && hbDoneAt < 5000);
      c.rec("sleep", await sleepP);
      c.note("heartbeatDoneAtMs", hbDoneAt);
    },
  });
  add({
    id: "B-sleep-then-csms-call", group: "B", desc: `DataTransfer parity.sleep 3000 in flight, CSMS-initiated ${V.ping} issued meanwhile`,
    timeoutMs: 30000,
    async run(c) {
      const s = c.sim("a");
      c.rec("boot", await c.boot(s));
      const sleepP = c.raw(s, call("DataTransfer", { vendorId: "parity.sleep", data: "3000" }, "sl1"), 5000);
      await sleep(400);
      const t0 = Date.now();
      const r: any = await c.server.call(c.cpId, V.ping, {});
      c.note("csmsCallMs", Date.now() - t0);
      c.rec("csmsCall", r.json ? { ok: r.json.ok, result: r.json.result, kind: r.json.kind } : r);
      c.rec("sleep", await sleepP);
    },
  });

  // ---------------------------------------------------------------- group C

  add({
    id: "C-replace-same-cpid", group: "C", desc: "second connection with the same cp id (last wins); kept under 1 s because the simulator reconnects after 1 s",
    async run(c) {
      const a = c.sim("a"); c.rec("bootA", await c.boot(a));
      const b = c.sim("b"); c.rec("bootB", await c.boot(b));
      await c.settle(150);
      c.rec("connections", await c.server.connections());
      c.rec("probeB", await c.raw(b, HB("probe1"), 300));
      c.rec("probeA", await c.raw(a, HB("probe2"), 300));
    },
  });
  add({
    id: "C-sim-clean-disconnect", group: "C", desc: "simulator closes the connection cleanly",
    async run(c) {
      const a = c.sim("a"); c.rec("boot", await c.boot(a));
      c.rec("disconnect", await c.cmd(a, "disconnect"));
      await c.settle(1000);
      c.rec("connections", await c.server.connections());
    },
  });
  add({
    id: "C-sim-sigkill", group: "C", desc: "simulator process killed with SIGKILL; does the server notice, and how",
    async run(c) {
      const a = c.sim("a"); c.rec("boot", await c.boot(a));
      const t0 = Date.now();
      a.kill("SIGKILL");
      let noticed: number | null = null;
      for (let i = 0; i < 100; i++) {
        await sleep(100);
        const conns = await c.server.connections();
        if (conns && !conns.includes(c.cpId)) { noticed = Date.now() - t0; break; }
      }
      c.rec("noticed", noticed !== null);
      c.note("noticedAfterMs", noticed);
      c.rec("connections", await c.server.connections());
    },
  });
  add({
    id: "C-server-disconnect", group: "C", desc: "server-initiated /control/disconnect",
    async run(c) {
      const a = c.sim("a"); c.rec("boot", await c.boot(a));
      const r: any = await c.server.http("/control/disconnect", { cpId: c.cpId });
      c.rec("control", c.ctl(r));
      await c.settle(200);
      c.rec("connections", await c.server.connections());
      c.rec("probe", await c.raw(a, HB("probe1"), 400));
    },
  });
  add({
    id: "C-shutdown-then-reconnect", group: "C", desc: "/control/shutdown, then a new connection attempt (kept under 1 s: the simulator auto-reconnects after 1 s)",
    async run(c) {
      const a = c.sim("a"); c.rec("bootA", await c.boot(a));
      const r: any = await c.server.http("/control/shutdown", {});
      c.rec("control", c.ctl(r));
      await c.settle(150);
      c.rec("alive", await c.server.alive());
      const b = c.sim("b");
      const conn = await b.cmd("connect", {}, 3000);
      c.rec("connectB", c.simResp(conn));
      const end = Date.now() + 600;
      while (Date.now() < end && !b.events.some((e) => e.event === "boot_notification" || e.event === "disconnected")) await sleep(25);
      c.rec("bootB", b.events.some((e) => e.event === "boot_notification"));
      c.rec("connections", await c.server.connections());
    },
  });
  add({
    id: "C-call-not-connected", group: "C", desc: "/control/call for a cp id that is not connected",
    async run(c) {
      const r: any = await c.server.call("nobody", "ClearCache", {});
      c.rec("control", c.ctl(r));
    },
  });
  add({
    id: "C-call-no-answer", group: "C", desc: "/control/call while the CP never answers (raw client ignores the request)",
    timeoutMs: 30000,
    async run(c) {
      // The raw client connects, then stays silent when the CSMS calls it.
      let got: unknown[] = [];
      const ws = new WebSocket(`${c.wsBase}/${c.cpId}`, [c.proto]);
      await new Promise<void>((res) => { ws.onopen = () => res(); ws.onerror = () => res(); });
      ws.onmessage = (m) => { try { const f = JSON.parse(String(m.data)); f[1] = "<id>"; got.push(JSON.stringify(f)); } catch { got.push(String(m.data)); } };
      await sleep(300);
      const r: any = await c.server.call(c.cpId, "ClearCache", {});
      c.rec("control", c.ctl(r));
      c.rec("cpReceived", got);
      // A second call after the timeout: is the queue/connection still usable?
      const r2: any = await c.server.call(c.cpId, "ClearCache", {});
      c.rec("control2", c.ctl(r2));
      ws.close();
      await sleep(400);
    },
  });
  for (const [id, protos, desc] of [
    ["none", [], "no subprotocol offered"],
    ["wrong", [V.other], `only ${V.other} offered`],
    ["foo-first", ["foo", `ocpp${V.v}`], `unknown token first, ocpp${V.v} second`],
    ["other-first", [V.other, `ocpp${V.v}`], `${V.other} first, ocpp${V.v} second`],
    ["own-only", [`ocpp${V.v}`], `ocpp${V.v} offered (control)`],
    ["own-and-21", [`ocpp${V.v}`, "ocpp2.1"], `ocpp${V.v} first, ocpp2.1 second`],
    ["case", [`OCPP${V.v}`], `subprotocol in upper case`],
  ] as [string, string[], string][]) {
    add({
      id: `C-subprotocol-${id}`, group: "C", desc: `${desc} [raw client, not the simulator]`,
      async run(c) {
        const boot = is16
          ? call("BootNotification", { chargePointVendor: "V", chargePointModel: "M" }, "b1")
          : call("BootNotification", { reason: "PowerUp", chargingStation: { model: "M", vendorName: "V" } }, "b1");
        const r = await rawWs(`${c.wsBase}/${c.cpId}`, [...protos], [boot]);
        c.rec("handshake", c.hs(r));
      },
    });
  }
  for (const [id, path] of [
    ["no-cpid", "/ocpp"], ["trailing-slash", "/ocpp/"], ["wrong-prefix", "/other/X"], ["extra-segment", "/ocpp/X/extra"], ["root", "/"],
  ] as const) {
    add({
      id: `C-path-${id}`, group: "C", desc: `connect to ${path} [raw client, not the simulator]`,
      async run(c) {
        const base = `ws://127.0.0.1:${c.server.port}`;
        const r = await rawWs(base + path, [c.proto], [HB("h1")]);
        c.rec("handshake", c.hs(r));
      },
    });
  }
  for (const [id, cp] of [
    ["space", "a b"], ["pct2f", "a%2Fb"], ["slash", "a/b"], ["unicode", "充電器1"], ["plus-colon", "a+b:c"],
  ] as const) {
    add({
      id: `C-cpid-${id}`, group: "C", desc: `cp id ${JSON.stringify(cp)} through the simulator`,
      async run(c) {
        const a = c.sim("a", cp);
        c.rec("boot", await c.boot(a));
        c.rec("heartbeat", await c.raw(a, HB("h1"), 1500));
        c.rec("connections", await c.server.connections());
        const r: any = await c.server.call(cp, "ClearCache", {});
        c.rec("csmsCall", c.ctl(r));
      },
    });
  }
  add({
    id: "C-idle-silence-75s", group: "C", slow: true, timeoutMs: 120000,
    desc: "boot, then no traffic for 75 s (both servers use 60 s ping/read idle defaults)",
    async run(c) {
      const a = c.sim("a"); c.rec("boot", await c.boot(a));
      let closedAt: number | null = null;
      const t0 = Date.now();
      while (Date.now() - t0 < 75000) {
        if (closedAt === null && a.events.some((e) => e.event === "disconnected")) closedAt = Date.now() - t0;
        await sleep(250);
      }
      c.rec("closedDuringIdle", closedAt !== null);
      c.note("closedAtMs", closedAt);
      c.rec("connections", await c.server.connections());
      c.rec("probe", await c.raw(a, HB("probe1"), 1500));
    },
  });

  // ---------------------------------------------------------------- group D

  add({
    id: "D-normal-flow", group: "D", desc: "boot, status, authorize, start tx, meter values, stop tx, heartbeat",
    timeoutMs: 40000,
    async run(c) {
      const s = c.sim("a"); c.rec("boot", await c.boot(s));
      c.rec("status", await c.cmd(s, "update_connector_status", { connector: 1, status: "Available" }));
      c.rec("authorize", await c.cmd(s, "authorize", { tagId: "TAG1" }));
      c.rec("start", await c.cmd(s, "start_transaction", { connector: 1, tagId: "TAG1" }));
      await c.settle(500);
      c.rec("meter", await c.cmd(s, "send_meter_value", { connector: 1 }));
      c.rec("stop", await c.cmd(s, "stop_transaction", { connector: 1 }));
      c.rec("heartbeat", await c.cmd(s, "heartbeat"));
      await c.settle(800);
    },
  });
  add({
    id: "D-idtag-rules", group: "D", desc: "Authorize with INVALID/BLOCKED/EXPIRED prefixes and a transaction start/stop with a blocked tag",
    async run(c) {
      const s = c.sim("a"); c.rec("boot", await c.boot(s));
      if (is16) {
        const START = { connectorId: 1, idTag: "T", meterStart: 0 };
        for (const t of ["INVALID1", "BLOCKED1", "EXPIRED1"]) c.rec(`authorize-${t}`, await c.cmd(s, "send_ocpp_call", { action: "Authorize", payload: { idTag: t } }));
        c.rec("start-blocked", await c.cmd(s, "send_ocpp_call", { action: "StartTransaction", payload: { ...START, timestamp: ts, idTag: "BLOCKED1" } }));
        c.rec("stop-no-idtag", await c.cmd(s, "send_ocpp_call", { action: "StopTransaction", payload: { meterStop: 1, timestamp: ts, transactionId: 1 } }));
      } else {
        const tx = (over: object) => ({ eventType: "Started", timestamp: ts, triggerReason: "Authorized", seqNo: 0, transactionInfo: { transactionId: "tx1" }, ...over });
        for (const t of ["INVALID1", "BLOCKED1", "EXPIRED1", "VIP-INV", "UNKNOWN1"]) c.rec(`authorize-${t}`, await c.cmd(s, "send_ocpp_call", { action: "Authorize", payload: { idToken: tokObj(t) } }));
        c.rec("start-blocked", await c.cmd(s, "send_ocpp_call", { action: "TransactionEvent", payload: tx({ idToken: tokObj("BLOCKED1") }) }));
        c.rec("ended-no-token", await c.cmd(s, "send_ocpp_call", { action: "TransactionEvent", payload: tx({ eventType: "Ended", seqNo: 1, triggerReason: "StopAuthorized" }) }));
      }
    },
  });
  add({
    id: "D-datatransfer-echo", group: "D", desc: "DataTransfer with string data and with a messageId (echo)",
    async run(c) {
      const s = c.sim("a"); c.rec("boot", await c.boot(s));
      c.rec("string", await c.cmd(s, "data_transfer", { vendorId: "acme", messageId: "m1", data: "hello" }));
      c.rec("object", await c.cmd(s, "data_transfer", { vendorId: "acme", data: { a: 1 } }));
      c.rec("none", await c.cmd(s, "data_transfer", { vendorId: "acme" }));
    },
  });
  for (const [action, payload, needsTx] of V.csms) {
    add({
      id: `D-csms-${action}`, group: "D", desc: `CSMS-initiated ${action} answered by the simulator's engine`,
      timeoutMs: 40000,
      async run(c) {
        const s = c.sim("a"); c.rec("boot", await c.boot(s));
        if (needsTx) { c.rec("start", await c.cmd(s, "start_transaction", { connector: 1, tagId: "TAG1" })); await c.settle(500); }
        const r: any = await c.server.call(c.cpId, action, await resolvePayload(c, payload));
        c.rec("control", c.ctl(r));
        await c.settle(1500);
      },
    });
  }
}

for (const V of VERS) defineABCD(V);

// ------------------------------------------------------------------ group E

cases.push(...scenarioCases(Object.keys(scripts)));
cases.push(...scenarioCases201(Object.keys(scripts201)));

// ------------------------------------------------------------------ group G: OCPP 2.1 (gocpp only)

cases.push(...cases21);
