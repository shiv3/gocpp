
# Analysis (hand-written)

Charge-point side: only the TypeScript simulator (`/work/wt/ocpp-cp-sim-parity`, 50e666f5). Libraries: gocpp (this checkout, `main` 57b30d5) and ocpp-go (shiv3 fork v0.17.2-fix.3, 1.6 and 2.0.1 only). Two runs, 0 flaky. (The pre-existing 1.6 differences are analysed in `../2026-11-20*/`.)

## OCPP 2.0.1 differences (gocpp vs ocpp-go)

Acceptable (documented behaviour, spec-defensible, or harness artefact):
- gocpp runs the default `lenient` schema mode: it coerces/accepts string numbers (`evseId "1"`, `seqNo "0"`), enum case (`available`), timestamps without zone / with a space, `idToken` and `transactionId` longer than 36, an empty `meterValue` array, a string meter value. ocpp-go answers CALLERROR for these. Re-run with `--gocpp-args "-schema strict"` to compare strict mode.
- gocpp rejects `customData` without `vendorId` (schema-correct); ocpp-go accepts it.
- Unknown action: gocpp `NotImplemented` (spec wording for an unknown action), ocpp-go `NotSupported`. Error codes for schema violations differ (gocpp: PropertyConstraintViolation / TypeConstraintViolation / OccurrenceConstraintViolation; ocpp-go: FormatViolation / GenericError / PropertyConstraintViolation).
- Frames with a 40-char or empty message id: gocpp answers, ocpp-go drops them. `[2,"id"]` and `[2,"id",5,{}]`: gocpp answers FormatViolation, ocpp-go stays silent. `payload: null`: gocpp FormatViolation, ocpp-go treats it as `{}`.
- Control-API re-encoding artefacts (not wire): ocpp-go prints `"evseId":0` defaults in the decoded CSMS result; the harness drops `null`s.
- Panic in a DataTransfer handler: gocpp answers CALLERROR InternalError and keeps serving; ocpp-go's process dies (connection drops 1006). Same as 1.6.
- After `/control/shutdown` gocpp refuses new sockets (HTTP 503), ocpp-go accepts. Same as 1.6.
- Subprotocol in upper case: gocpp accepts and echoes `OCPP2.0.1`; ocpp-go answers 1002.

Risky (ocpp-go side, not gocpp):
- `NotifyReport` without `seqNo` is accepted by ocpp-go.
- int64 values above 2^53 are corrupted by ocpp-go (decoded through float64: `evseId 9223372036854775807` -> overflow error). gocpp decodes exactly (the harness printout of the gocpp event is rounded by the JS runner, not by gocpp).

Risky / worth a look (gocpp):
- Frames with message type 5 or 6 on a 1.6 / 2.0.1 connection are dropped silently by gocpp; ocpp-go answers `MessageTypeNotSupported`. gocpp's reader already answers MessageTypeNotSupported for unknown types on non-1.6 connections (`core/dispatcher/conn.go` reader), but `ocppj.Parse` is version-agnostic and accepts types 5/6, so on a 2.0.1 connection a SEND/CALLRESULTERROR is routed instead of rejected. Minor (2.1-only types on a 2.0.1 link); suspected location `core/ocppj/parse.go` + `core/dispatcher/conn.go` `routeInbound`.

No gocpp library bug that breaks 2.0.1 or 2.1 interoperability was found.

## Group E on 2.0.1

27 scenario cases (2 bundled `-201` templates, 13 version-neutral templates, 8 cert16 templates mapped by action alias, 2 timeout variants), identical on both libraries. Both `-201` templates pass their expected path. 6 templates end `FAIL` identically on both libraries (resilience-authorization-rejected, -communication-loss-recovery, -connector-fault-recovery, -ev-disconnect-recovery, -temporary-unavailable-recovery; cert16-tc017 never reaches UnlockConnector): their assertions are written for 1.6 messages and do not hold on a 2.0.1 station (simulator template issue, see gaps). 4 are stopped by the runner at budget (unbounded by design).

## OCPP 2.1 (gocpp only, simulator as charge point)

41 flows, 135 checks, all pass: simulator flow (boot, status, authorize, transaction start/stop, meter values, heartbeat; no gocpp warn/error log), Authorize rules, 19 CSMS-initiated actions incl. 2.1-only UsePriorityCharging, GetTariffs, ClearTariffs, SetDefaultTariff, NotifyAllowedEnergyTransfer (the simulator answered SetDefaultTariff with CALLERROR NotSupported "Local cost calculation is not supported"; relayed correctly), 9 raw-frame cases and 8 SEND (type 6) cases (valid / customData SEND reach the handler with no reply; invalid, unknown-action, CALL-action-as-SEND, malformed SENDs produce no reply and keep the connection), server disconnect, last-wins replacement. Note gocpp's lenient mode coerces `"id":"1"` in a SEND and `seqNo "0"` in a CALL.

## Simulator gaps (to file against ocpp-cp-simulator)

1. OCPP 2.1 CALLRESULTERROR (type 5): the simulator cannot send one, nor produce a CALLRESULT that the CSMS must reject, nor suppress its automatic answer to a CSMS CALL (`send_raw_frame` cannot race the engine). Blocks: CSMS-side CALLRESULTERROR emission test (gocpp sends `[5,id,FormatViolation,...]` for a schema-invalid CALLRESULT), CP-initiated CALLRESULTERROR handling by the CSMS, and 2.0.1/1.6 `A-callerror-answering-csms-request`.
2. OCPP 2.1 SEND (type 6): no command or scenario node sends `NotifyPeriodicEventStream` (or any SEND); only `send_raw_frame` with a hand-written frame (used here), so no scenario can drive it and the simulator's own SEND frame encoding is untested. No handling of CSMS-originated SEND observable either.
3. WebSocket handshake control: no option to offer a custom subprotocol list (none, wrong version, several, upper case) or to connect with a bad path. Blocks the 2.1 subprotocol negotiation cases (G) and the simulator-driven variant of group C subprotocol/path cases (those use a tiny raw client in the existing harness, 1.6/2.0.1 only).
4. OCPP 2.1 is the 2.0.1 engine: no 2.1-specific CP behaviour could be driven (no 2.1 scenario templates; no commands for 2.1-only CP messages such as NotifyPeriodicEventStream, NotifyDERAlarm, NotifySettlement, BatterySwap results, PullDynamicScheduleUpdate; 2.1 `TransactionEvent` extras not producible). Blocks: coverage of those CP-to-CSMS messages against gocpp's 2.1 handlers.
5. Scenario templates: only two bundled templates target 2.0.1 (`dataflow-*-201`), none 2.1; all `cert16-*` templates are written around 1.6 message names/payloads. On a 2.0.1 station the resilience-* templates' assertions fail (they assert 1.6 StatusNotification fields/behaviour), cert16-tc017 never receives UnlockConnector (alias mapping missing), and `cert16-tc019/021/043/044/045/048/051/052/056/057/059/066/067` cannot be run (1.6-only CSMS payloads: GetConfiguration, ChangeConfiguration, SendLocalList 1.6, firmware/diagnostics, ReserveNow 1.6, charging profile 1.6). Need 2.0.1 (and 2.1) variants.
6. Malformed output cannot be provoked from the simulator in 2.0.1/2.1: no way to make its engine answer a CSMS call with a schema-invalid payload or a custom CALLERROR code (only `armed CallError` for some actions in 1.6 templates), so CSMS handling of bad CP answers is not covered for 2.x.
