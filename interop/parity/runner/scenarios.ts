// Group E: the simulator's bundled OCPP 1.6 scenarios, run against both
// servers. A scenario is a charge-point-side script that waits for
// CSMS-initiated requests; the table below says what the CSMS (this runner,
// through /control/call) has to send, and when, for the scenario to progress.
//
// A step is one of
//   { wait: "<Action>", then: [calls] }   scenario_status shows state=waiting
//                                         with that expectation, then send calls
//   { node: "<nodeId>", then: [calls] }   scenario is at that node (running or waiting)
//   { sim: "<command>", params }          simulator JSON-mode command
//   { sleep: ms }
// followed by "wait until the scenario reaches a terminal state or the case
// budget runs out" and a scenario_report.
import type { Case } from "./cases";
import type { Ctx } from "./ctx";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { norm, SIM_DIR, sleep } from "./lib";

export interface Env { tx(): Promise<number>; url: string }
type Payload = unknown | ((e: Env) => unknown | Promise<unknown>);
export interface CallAct { action: string; payload?: Payload }
export type Step =
  | { wait: string; then?: CallAct[] }
  | { node: string; then?: CallAct[] }
  | { sim: string; params?: unknown }
  | { sleep: number };
export interface Script {
  steps?: Step[];
  /** evSettings override passed to run_scenario_template */
  evSettings?: Record<string, unknown>;
  /** simulator template id when it differs from the case key */
  template?: string;
  /** budget for the whole case in ms (default 55 s) */
  maxMs?: number;
  /** a local HTTP sink/source is needed (diagnostics upload, firmware location) */
  http?: boolean;
  /** Checks that the scenario took the path the test case intends; gets the
   *  full run transcript (in order) and returns problems (empty = fine). */
  expect?: (t: Tx) => string[];
  /** why this script looks like it does; goes into the report */
  note?: string;
}

// ------------------------------------------------------- transcript helpers

interface Entry { seq: number; direction: "sent" | "received"; kind: string; uniqueId: string; action?: string; payload?: any }
export class Tx {
  constructor(public e: Entry[]) {}
  /** payloads of the calls the CP sent for an action */
  sent(a: string): any[] { return this.e.filter((x) => x.direction === "sent" && x.kind === "call" && x.action === a).map((x) => x.payload); }
  /** the CSMS answers (call results) to the CP calls for an action */
  answers(a: string): any[] {
    return this.e.filter((x) => x.direction === "sent" && x.kind === "call" && x.action === a)
      .map((x) => this.e.find((y) => y.direction === "received" && y.uniqueId === x.uniqueId && y.kind !== "call")?.payload);
  }
  /** index of the first CSMS call for an action, or -1 */
  csmsCall(a: string): number { return this.e.findIndex((x) => x.direction === "received" && x.kind === "call" && x.action === a); }
  sentAfter(a: string, idx: number): any[] { return this.e.slice(idx + 1).filter((x) => x.direction === "sent" && x.kind === "call" && x.action === a).map((x) => x.payload); }
}
function entriesFromTrace(path: string): Entry[] {
  if (!existsSync(path)) return [];
  const out: Entry[] = [];
  for (const line of readFileSync(path, "utf8").split("\n")) {
    if (!line.trim()) continue;
    try {
      const r = JSON.parse(line);
      const f = JSON.parse(r.raw);
      if (!Array.isArray(f)) continue;
      const direction = r.direction === "cp-to-csms" ? "sent" : "received";
      if (f[0] === 2) out.push({ seq: out.length, direction, kind: "call", uniqueId: f[1], action: f[2], payload: f[3] });
      else if (f[0] === 3) out.push({ seq: out.length, direction, kind: "callresult", uniqueId: f[1], payload: f[2] });
      else if (f[0] === 4) out.push({ seq: out.length, direction, kind: "callerror", uniqueId: f[1], payload: f[4] });
    } catch { /* skip */ }
  }
  return out;
}
const uniq = (a: unknown[]) => [...new Set(a.filter((x) => x !== undefined))];
const same = (got: unknown, want: unknown) => JSON.stringify(got) === JSON.stringify(want);
const chk = (label: string, got: unknown, want: unknown): string[] => (same(got, want) ? [] : [`${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`]);
const authOutcome = (status: string) => (t: Tx) => [
  ...chk("Authorize answer", t.answers("Authorize").map((p) => p?.idTagInfo?.status), [status]),
  ...chk("StartTransaction sent", t.sent("StartTransaction").length, 0),
];
const fwSeq = (a: string, want: string[]) => (t: Tx) => chk(a, t.sent(a).map((p) => p.status), want);
const started = (idTag: string) => (t: Tx) => chk("StartTransaction idTags", t.sent("StartTransaction").map((p) => p.idTag), [idTag]);
const stopReason = (r: string) => (t: Tx) => chk("StopTransaction reasons", t.sent("StopTransaction").map((p) => p.reason), [r]);

const iso = (ms = 0) => new Date(Date.now() + ms).toISOString();
const rs = (idTag: string, extra: object = {}): CallAct => ({ action: "RemoteStartTransaction", payload: { connectorId: 1, idTag, ...extra } });
const rstop = (): CallAct => ({ action: "RemoteStopTransaction", payload: async (e: Env) => ({ transactionId: await e.tx() }) });
const reserve = (id = 7, idTag = "CERT-RES01"): CallAct => ({
  action: "ReserveNow", payload: () => ({ connectorId: 1, expiryDate: iso(3600e3), idTag, reservationId: id }),
});
const unlock = (connectorId = 1): CallAct => ({ action: "UnlockConnector", payload: { connectorId } });
const profilePayload = (purpose: string, extra: Record<string, unknown> = {}, connectorId = 1) => ({
  connectorId,
  csChargingProfiles: {
    chargingProfileId: 11, stackLevel: 0, chargingProfilePurpose: purpose, chargingProfileKind: "Absolute",
    chargingSchedule: { chargingRateUnit: "W", chargingSchedulePeriod: [{ startPeriod: 0, limit: 11000 }], duration: 3600, startSchedule: iso() },
    ...extra,
  },
});
const profile = (purpose: string, extra: Record<string, unknown> = {}, connectorId = 1): CallAct => ({
  action: "SetChargingProfile", payload: () => profilePayload(purpose, extra, connectorId),
});
const sendList = (updateType: "Full" | "Differential", version: number, list: object[]): CallAct => ({
  action: "SendLocalList", payload: { listVersion: version, updateType, localAuthorizationList: list },
});
const entry = (idTag: string, status = "Accepted") => ({ idTag, idTagInfo: { status, expiryDate: iso(86400e3) } });

export const scripts: Record<string, Script> = {
  "cert16-octt-strictness-probe": { note: "no CSMS action: the two `ocpp_absent` assertions (no GetConfiguration / GetDiagnostics from the CSMS) hold" },
  "cert16-octt-strictness-probe+csms-sends": {
    template: "cert16-octt-strictness-probe",
    steps: [{ node: "wait-for-config", then: [{ action: "GetDiagnostics", payload: (e: Env) => ({ location: `${e.url}/diag` }) }, { action: "GetConfiguration", payload: {} }] }],
    http: true,
    note: "the CSMS does what the probe warns about: GetDiagnostics (answered with the armed CallError NotImplemented) and GetConfiguration inside the 15 s window; both assertions are expected to fail",
  },
  "cert16-reservation-basic": { expect: started("CERT-RES01"), steps: [{ wait: "ReserveNow", then: [reserve(7, "CERT-RES01")] }] },
  "cert16-tc001-cold-boot": { note: "no CSMS action; 10 s idle" },
  "cert16-tc003-charging-plugin-first": { expect: (t) => [...started("CERT003")(t), ...chk("StopTransaction count", t.sent("StopTransaction").length, 1)], note: "no CSMS action; the idTag CERT003 is authorised by the default rule" },
  "cert16-tc004-charging-id-first": { expect: (t) => [...started("CERT004")(t), ...chk("StopTransaction count", t.sent("StopTransaction").length, 1)], note: "no CSMS action" },
  "cert16-tc005-ev-side-disconnect": { expect: stopReason("EVDisconnected"), note: "no CSMS action" },
  "cert16-tc010-remote-start": { expect: started("CERT010"), steps: [{ wait: "RemoteStartTransaction", then: [rs("CERT010")] }] },
  "cert16-tc011-remote-start-stop": { expect: (t) => [...started("CERT011")(t), ...chk("StopTransaction count", t.sent("StopTransaction").length, 1)], steps: [{ wait: "RemoteStartTransaction", then: [rs("CERT011")] }, { wait: "RemoteStopTransaction", then: [rstop()] }] },
  "cert16-tc012-remote-stop": { expect: (t) => [...started("CERT012")(t), ...chk("StopTransaction count", t.sent("StopTransaction").length, 1)], steps: [{ wait: "RemoteStopTransaction", then: [rstop()] }] },
  "cert16-tc013-hard-reset": { expect: stopReason("HardReset"), steps: [{ wait: "Reset", then: [{ action: "Reset", payload: { type: "Hard" } }] }] },
  "cert16-tc014-soft-reset": { expect: stopReason("SoftReset"), steps: [{ wait: "Reset", then: [{ action: "Reset", payload: { type: "Soft" } }] }] },
  "cert16-tc017-unlock-occupied": { steps: [{ node: "delay-unlock-window", then: [unlock(1)] }] },
  "cert16-tc018-unlock-failure": { steps: [{ node: "delay-unlock-window", then: [unlock(1)] }] },
  "cert16-tc019-get-configuration-all": { steps: [{ wait: "GetConfiguration", then: [{ action: "GetConfiguration", payload: {} }] }] },
  "cert16-tc019-get-configuration-key": { steps: [{ wait: "GetConfiguration", then: [{ action: "GetConfiguration", payload: { key: ["HeartbeatInterval"] } }] }] },
  "cert16-tc021-change-configuration": { steps: [{ wait: "ChangeConfiguration", then: [{ action: "ChangeConfiguration", payload: { key: "MeterValueSampleInterval", value: "10" } }] }] },
  "cert16-tc023-1-authorize-invalid": { expect: authOutcome("Invalid"), note: "no CSMS action; tag CERT023-INV needs the harness rule suffix -INV -> Invalid" },
  "cert16-tc023-2-authorize-expired": { expect: authOutcome("Expired"), note: "no CSMS action; tag CERT023-EXP needs the harness rule suffix -EXP -> Expired" },
  "cert16-tc023-3-authorize-blocked": { expect: authOutcome("Blocked"), note: "no CSMS action; tag CERT023-BLK needs the harness rule suffix -BLK -> Blocked" },
  "cert16-tc024-lock-failure": { expect: (t) => [...chk("faulted", t.sent("StatusNotification").some((p) => p.status === "Faulted" && p.errorCode === "ConnectorLockFailure"), true), ...chk("StartTransaction sent", t.sent("StartTransaction").length, 0)], note: "no CSMS action" },
  "cert16-tc026-remote-start-rejected": { expect: (t) => chk("StartTransaction sent", t.sent("StartTransaction").length, 0), steps: [{ wait: "RemoteStartTransaction", then: [rs("CERT026")] }] },
  "cert16-tc028-remote-stop-rejected": { steps: [{ wait: "RemoteStopTransaction", then: [rstop()] }] },
  "cert16-tc031-unlock-unknown-connector": { steps: [{ wait: "UnlockConnector", then: [unlock(99)] }] },
  "cert16-tc042-1-get-local-list-version-not-supported": { steps: [{ wait: "GetLocalListVersion", then: [{ action: "GetLocalListVersion", payload: {} }] }] },
  "cert16-tc042-2-get-local-list-version-empty": { steps: [{ wait: "GetLocalListVersion", then: [{ action: "GetLocalListVersion", payload: {} }] }] },
  "cert16-tc043-1-send-local-list-not-supported": { steps: [{ wait: "SendLocalList", then: [sendList("Full", 1, [entry("LIST-A")])] }] },
  "cert16-tc043-3-send-local-list-failed": { steps: [{ wait: "SendLocalList", then: [sendList("Full", 1, [entry("LIST-A")])] }] },
  "cert16-tc043-4-send-local-list-full": { steps: [{ wait: "SendLocalList", then: [sendList("Full", 1, [entry("LIST-A"), entry("LIST-B", "Blocked")])] }] },
  "cert16-tc043-5-send-local-list-differential": {
    steps: [
      { wait: "SendLocalList", then: [sendList("Full", 1, [entry("LIST-A"), entry("LIST-B")])] },
      { wait: "SendLocalList", then: [sendList("Differential", 2, [entry("LIST-C"), { idTag: "LIST-A" }])] },
    ],
  },
  "cert16-tc044-1-firmware-update": { expect: fwSeq("FirmwareStatusNotification", ["Downloading", "Downloaded", "Installing", "Installed"]), http: true, steps: [{ wait: "UpdateFirmware", then: [{ action: "UpdateFirmware", payload: (e: Env) => ({ location: `${e.url}/fw.bin`, retrieveDate: iso() }) }] }] },
  "cert16-tc044-2-firmware-download-failed": { expect: fwSeq("FirmwareStatusNotification", ["Downloading", "DownloadFailed"]), http: true, steps: [{ wait: "UpdateFirmware", then: [{ action: "UpdateFirmware", payload: (e: Env) => ({ location: `${e.url}/fw.bin`, retrieveDate: iso() }) }] }] },
  "cert16-tc044-3-firmware-install-failed": { expect: fwSeq("FirmwareStatusNotification", ["Downloading", "Downloaded", "Installing", "InstallationFailed"]), http: true, steps: [{ wait: "UpdateFirmware", then: [{ action: "UpdateFirmware", payload: (e: Env) => ({ location: `${e.url}/fw.bin`, retrieveDate: iso() }) }] }] },
  "cert16-tc045-1-get-diagnostics": { expect: fwSeq("DiagnosticsStatusNotification", ["Uploading", "Uploaded"]), http: true, steps: [{ wait: "GetDiagnostics", then: [{ action: "GetDiagnostics", payload: (e: Env) => ({ location: `${e.url}/diag` }) }] }] },
  "cert16-tc048-1-reserve-now-faulted": { steps: [{ wait: "ReserveNow", then: [reserve()] }] },
  "cert16-tc048-2-reserve-now-occupied": { steps: [{ wait: "ReserveNow", then: [reserve()] }] },
  "cert16-tc048-3-reserve-now-unavailable": { steps: [{ wait: "ReserveNow", then: [reserve()] }] },
  "cert16-tc048-4-reserve-now-rejected": { steps: [{ wait: "ReserveNow", then: [reserve()] }] },
  "cert16-tc051-cancel-reservation": { steps: [{ wait: "ReserveNow", then: [reserve(7)] }, { wait: "CancelReservation", then: [{ action: "CancelReservation", payload: { reservationId: 7 } }] }] },
  "cert16-tc052-cancel-reservation-rejected": { steps: [{ wait: "CancelReservation", then: [{ action: "CancelReservation", payload: { reservationId: 999 } }] }] },
  "cert16-tc054-trigger-message": { expect: (t) => chk("StatusNotification after TriggerMessage", t.sentAfter("StatusNotification", t.csmsCall("TriggerMessage")).length >= 1, true), steps: [{ wait: "TriggerMessage", then: [{ action: "TriggerMessage", payload: { requestedMessage: "StatusNotification", connectorId: 1 } }] }] },
  "cert16-tc055-trigger-message-rejected": { steps: [{ wait: "TriggerMessage", then: [{ action: "TriggerMessage", payload: { requestedMessage: "Heartbeat" } }] }] },
  "cert16-tc056-central-smart-charging-txdefault": { steps: [{ wait: "SetChargingProfile", then: [profile("TxDefaultProfile")] }] },
  "cert16-tc057-central-smart-charging-txprofile": {
    steps: [{ wait: "SetChargingProfile", then: [{ action: "SetChargingProfile", payload: async (e: Env) => profilePayload("TxProfile", { transactionId: await e.tx() }) }] }],
  },
  "cert16-tc059-remote-start-with-profile": { expect: started("CERT059"),
    steps: [{ wait: "RemoteStartTransaction", then: [rs("CERT059", {
      chargingProfile: { chargingProfileId: 12, stackLevel: 0, chargingProfilePurpose: "TxProfile", chargingProfileKind: "Relative", chargingSchedule: { chargingRateUnit: "W", chargingSchedulePeriod: [{ startPeriod: 0, limit: 7000 }] } },
    })] }],
  },
  "cert16-tc061-clear-cache": { steps: [{ wait: "ClearCache", then: [{ action: "ClearCache", payload: {} }] }] },
  "cert16-tc064-data-transfer": { expect: (t) => [...chk("DataTransfer vendor", t.sent("DataTransfer").map((p) => p.vendorId), ["com.example.cert16"]), ...chk("DataTransfer answer", t.answers("DataTransfer").map((p) => p?.status), ["Accepted"])], note: "no CSMS action; the CP sends DataTransfer vendor com.example.cert16, answered by the echo rule" },
  "cert16-tc066-get-composite-schedule": {
    steps: [
      { wait: "SetChargingProfile", then: [profile("TxDefaultProfile", {}, 0)] },
      { wait: "GetCompositeSchedule", then: [{ action: "GetCompositeSchedule", payload: { connectorId: 1, duration: 600, chargingRateUnit: "W" } }] },
    ],
  },
  "cert16-tc067-clear-charging-profile": {
    steps: [
      { wait: "SetChargingProfile", then: [profile("TxDefaultProfile", {}, 0)] },
      { wait: "ClearChargingProfile", then: [{ action: "ClearChargingProfile", payload: { id: 11 } }] },
    ],
  },
  "dataflow-authorize-branch-16": { expect: (t) => [...chk("Authorize answers", uniq(t.answers("Authorize").map((p) => p?.idTagInfo?.status)), ["Accepted"]), ...chk("StartTransaction sent", t.sent("StartTransaction").length, 1)], note: "no CSMS action; DEMO-TAG-1 is Accepted, so the Accepted branch (transaction) runs" },
  "dataflow-call-error-handling": { expect: (t) => chk("DataTransfer answers", uniq(t.answers("DataTransfer").map((p) => p?.status)), ["Accepted"]), note: "no CSMS action; vendor org.example.dataflow is answered Accepted by the echo rule, so the result/Accepted branch runs" },
  "dataflow-charge-to-soc": { note: "no CSMS action" },
  "dataflow-remote-start-reauthorize-16": { expect: (t) => [...chk("first Authorize idTag", t.sent("Authorize")[0]?.idTag, "DEMO-TAG-2"), ...chk("StartTransaction sent", t.sent("StartTransaction").length, 1)], steps: [{ wait: "RemoteStartTransaction", then: [rs("DEMO-TAG-2")] }], note: "the scenario answers the RemoteStart itself (behavior replace), then Authorizes the tag from the call" },
  "essential-cp-behavior": { evSettings: { batteryCapacityKwh: 1, initialSoc: 78, targetSoc: 80 }, note: "stopMode evSettings: with the bundled 75 kWh EV the meter node never ends within any sane budget, so the EV is overridden to 1 kWh, SoC 78 -> 80 %", steps: [{ wait: "RemoteStartTransaction", then: [rs("TAG001")] }, { wait: "RemoteStopTransaction", then: [rstop()] }] },
  "full-charging-cycle": { maxMs: 25000, note: "no CSMS action; the meter node runs up to 25 kWh at 500 Wh / 5 s (about 250 s), so the runner stops the scenario after 25 s and compares the state at that point" },
  "multi-status-monitor": {
    maxMs: 25000,
    steps: [{ sleep: 1500 }, { sim: "ev_plug_in", params: { connector: 1 } }, { sim: "start_transaction", params: { connector: 1, tagId: "MSM-TAG" } }, { sleep: 4000 }, { sim: "update_connector_status", params: { connector: 1, status: "Faulted" } }],
    note: "no CSMS action; the connector status triggers are driven by simulator commands (plug in, start transaction, Faulted)",
  },
  "parallel-heartbeat-session": { steps: [{ wait: "RemoteStartTransaction", then: [rs("PARALLEL-TAG")] }] },
  "remote-start-auto-meter": { steps: [{ wait: "RemoteStartTransaction", then: [rs("REMOTE001")] }], maxMs: 30000 },
  "remote-start-timeout-fallback+timeout": { expect: (t) => chk("StartTransaction sent", t.sent("StartTransaction").length, 0), template: "remote-start-timeout-fallback", maxMs: 50000, note: "no CSMS action: the 30 s wait times out and the timeout edge runs" },
  "remote-start-timeout-fallback": { steps: [{ wait: "RemoteStartTransaction", then: [rs("TIMEOUT-TAG")] }], note: "RemoteStart sent inside the 30 s window (the `ok` branch); the timeout branch is case E-remote-start-timeout-fallback+timeout" },
  "resilience-authorization-rejected": { expect: authOutcome("Blocked"), note: "no CSMS action; tag RESIL-BLK needs the harness rule suffix -BLK -> Blocked" },
  "resilience-communication-loss-recovery": {
    steps: [{ sleep: 1500 }, { sim: "disconnect" }, { sleep: 1500 }, { sim: "connect" }],
    note: "the link is cut and restored by the simulator's own disconnect/connect commands (the server cannot drop a connection in gocpp)",
  },
  "resilience-connector-fault-recovery": { note: "no CSMS action" },
  "resilience-ev-disconnect-recovery": { note: "no CSMS action" },
  "resilience-power-loss-recovery": {
    steps: [{ node: "wait-down" }, { sleep: 500 }, { sim: "disconnect" }, { sleep: 1500 }, { sim: "connect" }],
    note: "after the transaction started the simulator link is cut and restored with the simulator's own commands",
  },
  "resilience-temporary-unavailable-recovery": {
    steps: [
      { wait: "ChangeAvailability", then: [{ action: "ChangeAvailability", payload: { connectorId: 1, type: "Inoperative" } }] },
      { wait: "ChangeAvailability", then: [{ action: "ChangeAvailability", payload: { connectorId: 1, type: "Operative" } }] },
    ],
  },
  "smart-charging": {
    maxMs: 25000,
    steps: [{ node: "trigger-charging" }, { sim: "ev_plug_in", params: { connector: 1 } }, { sim: "start_transaction", params: { connector: 1, tagId: "SC-TAG" } }, { sleep: 4000 }, { sim: "stop_transaction", params: { connector: 1 } }, { node: "trigger-finish" }, { sim: "update_connector_status", params: { connector: 1, status: "Finishing" } }],
    note: "no CSMS action; Charging / Finishing status triggers are driven by simulator commands (plug in, start transaction, stop transaction; the Finishing status caused by the stop is emitted before the trigger node starts waiting, so Finishing is set once more by update_connector_status)",
  },
  "status-triggered-actions": { maxMs: 25000, note: "no CSMS action; by design the status trigger re-fires on every Available (a Heartbeat loop), so the runner stops it after 25 s" },
};

/** Scenarios not run, with the reason. */
export const skipped: Record<string, string> = {
  "dataflow-authorize-branch-201": "OCPP 2.0.1 only (idToken payload)",
  "dataflow-remote-start-reauthorize-201": "OCPP 2.0.1 only (RequestStartTransaction)",
};

/** One-line description of what the CSMS side does for a scenario (for the report). */
export async function describeScript(id: string): Promise<string> {
  const sc = scripts[id];
  if (!sc) return skipped[id] ? `skipped: ${skipped[id]}` : "?";
  const env: Env = { url: "http://127.0.0.1:<port>", tx: async () => 1 };
  const parts: string[] = [];
  for (const st of sc.steps ?? []) {
    if ("sleep" in st) { parts.push(`sleep ${st.sleep} ms`); continue; }
    if ("sim" in st) { parts.push(`sim ${st.sim}${st.params ? " " + JSON.stringify(st.params) : ""}`); continue; }
    const when = "wait" in st ? `on waiting for ${st.wait}` : `at node ${st.node}`;
    const calls: string[] = [];
    for (const a of st.then ?? []) {
      const p = typeof a.payload === "function" ? await (a.payload as (e: Env) => unknown)(env) : a.payload ?? {};
      calls.push(`${a.action} ${JSON.stringify(p).replace(/"20\d\d-[^"]*"/g, '"<ts>"')}`);
    }
    parts.push(calls.length ? `${when}: send ${calls.join(", ")}` : `${when}: (no call)`);
  }
  return (parts.length ? parts.join("; then ") : "no CSMS action") + (sc.maxMs && sc.maxMs < 55000 ? ` [budget ${sc.maxMs / 1000} s]` : "");
}

const TERMINAL = new Set(["completed", "error"]);

async function status(c: Ctx, sim: import("./lib").Sim, id: string): Promise<any | null> {
  const r = await sim.cmd("scenario_status", { connector: 1, scenarioId: id }, 5000);
  return r.ok && r.data ? r.data : null;
}

/** Normalise a scenario_report into what is compared. */
function normReport(c: Ctx, rep: any, st: any): unknown {
  if (!rep) return null;
  const errs: any[] = rep.errors ?? [];
  for (const e of errs) c.noteText(`[scenario error] ${JSON.stringify(e)}`);
  return norm({
    executionState: rep.executionState,
    stopped: rep.stopped,
    verdict: rep.verdict,
    conformanceVerdict: rep.conformanceVerdict,
    compatibilityVerdict: rep.compatibilityVerdict,
    assertions: (rep.assertions ?? []).map((a: any) => ({ id: a.id ?? a.name, status: a.status ?? a.verdict })),
    errorCount: errs.length,
    timeout: rep.timeout,
    executedNodes: st?.executedNodes,
    // Only the CSMS-initiated exchange (received calls, the CP's answers): the
    // CP-initiated entries are in the wire layer, and the last of them is cut
    // off at the moment the run ends (a simulator timing artefact).
    transcript: (rep.transcript ?? []).filter((t: any) => (t.direction === "received" && t.kind === "call") || (t.direction === "sent" && t.kind !== "call")).map((t: any) => {
      const o: any = { dir: t.direction, kind: t.kind };
      if (t.action) o.action = t.action;
      if (t.payload !== undefined) o.payload = t.payload;
      if (t.errorCode) o.errorCode = t.errorCode;
      return o;
    }),
    initialState: rep.initialState,
    finalState: rep.finalState,
  });
}

export async function runScenarioCase(c: Ctx, sid: string, script: Script, budgetMs: number): Promise<void> {
  const t0 = Date.now();
  const left = () => budgetMs - (Date.now() - t0);
  let httpHits: string[] = [];
  let srv: ReturnType<typeof Bun.serve> | undefined;
  if (script.http) {
    srv = Bun.serve({ port: 0, hostname: "127.0.0.1", fetch(req) { httpHits.push(`${req.method} ${new URL(req.url).pathname}`); return new Response("ok"); } });
  }
  try {
    const env: Env = {
      url: srv ? `http://127.0.0.1:${srv.port}` : "",
      async tx() {
        for (let i = 0; i < 20; i++) {
          const ev = await c.server.events();
          const r = ev.filter((e: any) => e.kind === "response_sent" && e.action === "StartTransaction").pop();
          const id = r?.payload?.transactionId;
          if (typeof id === "number") return id;
          await sleep(100);
        }
        return 0;
      },
    };
    const s = c.sim("a");
    c.rec("boot", await c.boot(s));
    const started = await s.cmd("run_scenario_template", { connector: 1, templateId: sid, ...(script.evSettings ? { evSettings: script.evSettings } : {}) }, 10000);
    c.rec("start", c.simResp(started));
    const runId: string | undefined = (started as any).data?.scenarioId;
    if (!runId) return;
    let n = 0;
    let missed = false;
    for (const step of script.steps ?? []) {
      if (missed) break;
      if ("sleep" in step) { await sleep(step.sleep); continue; }
      if ("sim" in step) { c.rec(`sim:${step.sim}`, await c.cmd(s, step.sim, step.params ?? {}, 8000)); continue; }
      const want = "wait" in step ? { kind: "wait" as const, v: step.wait } : { kind: "node" as const, v: step.node };
      let ok = false;
      while (left() > 0) {
        const st = await status(c, s, runId);
        if (st && TERMINAL.has(st.state)) break;
        if (st && want.kind === "wait" && st.state === "waiting" && st.expectation?.action === want.v) { ok = true; break; }
        if (st && want.kind === "node" && st.currentNodeId === want.v) { ok = true; break; }
        await sleep(100);
      }
      if (!ok) { c.rec(`missed:${want.v}`, true); missed = true; break; }
      for (const act of step.then ?? []) {
        const payload = typeof act.payload === "function" ? await (act.payload as (e: Env) => unknown)(env) : act.payload ?? {};
        c.rec(`call${++n}:${act.action}`, c.ctl(await c.server.call(c.cpId, act.action, payload)));
      }
    }
    // Wait for the scenario to finish.
    let st: any = null;
    while (left() > 0) {
      st = await status(c, s, runId);
      if (st && TERMINAL.has(st.state)) break;
      await sleep(200);
    }
    const finished = !!st && TERMINAL.has(st.state);
    if (!finished) {
      c.rec("unfinished", { state: st?.state, node: st?.currentNodeId, expecting: st?.expectation?.action ?? null });
      await s.cmd("stop_all_scenarios", { connector: 1 }, 5000);
      await sleep(300);
    }
    await sleep(1500); // let in-flight traffic after the last node (e.g. the reboot after Reset) settle
    const rep = await s.cmd("scenario_report", { connector: 1, scenarioId: runId }, 8000);
    c.rec("report", rep.ok ? normReport(c, rep.data, st) : c.simResp(rep));
    if (script.expect) {
      // Checked against the complete CP-side trace (the run transcript can miss CP calls made while the run ends).
      await s.end();
      c.rec("expect", script.expect(new Tx(entriesFromTrace(s.tracePath))));
    }
    if (srv) c.rec("http", httpHits.length ? [...httpHits].sort() : "none");
    c.note("elapsedMs", Date.now() - t0);
  } finally {
    srv?.stop(true);
  }
}

export function scenarioCases(ids: string[]): Case[] {
  const out: Case[] = [];
  // Every bundled scenario must have a script or a skip reason.
  const dir = join(SIM_DIR, "packages/core/src/utils/scenarios");
  if (existsSync(dir)) {
    for (const f of readdirSync(dir)) {
      const id = f.replace(/\.json$/, "");
      if (f.endsWith(".json") && !(id in scripts) && !(id in skipped) && !Object.values(scripts).some((x) => x.template === id)) {
        throw new Error(`bundled scenario ${id} has neither a script nor a skip reason in runner/scenarios.ts`);
      }
    }
  }
  for (const [id, why] of Object.entries(skipped)) out.push({ id: `E-${id}`, group: "E", desc: `scenario ${id}`, skip: why, async run() {} });
  for (const id of ids) {
    const sc = scripts[id];
    if (!sc) continue;
    out.push({
      id: `E-${id}`, group: "E", desc: `scenario ${id}${sc.note ? ": " + sc.note : ""}`,
      timeoutMs: (sc.maxMs ?? 55000) + 12000,
      run: (c) => runScenarioCase(c, sc.template ?? id, sc, sc.maxMs ?? 55000),
    });
  }
  return out;
}
