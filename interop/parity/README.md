# Parity test CSMS pair

Two minimal central systems with identical observable behaviour, one on gocpp
(`cs-gocpp`: OCPP 1.6, 2.0.1 and 2.1) and one on ocpp-go (`cs-ocppgo`: OCPP 1.6
and 2.0.1; ocpp-go has no 2.1), so a driver can send the same traffic to both
and diff the results. `-version 1.6|2.0.1|2.1` (default 1.6) selects the
protocol; the subprotocol is `ocpp<version>`. The two Go modules keep the root
gocpp module free of an ocpp-go dependency.

Version 2.x canned answers (same model as 1.6): BootNotification Accepted /
interval 300, Heartbeat, Authorize and TransactionEvent (when an `idToken` is
present the answer carries `idTokenInfo`) by the idToken rule below,
StatusNotification, MeterValues, NotifyReport (empty answers) and DataTransfer
by vendorId. CSMS-initiated calls: every `SentByCSMS` message of the gocpp
profile (`cs-gocpp/calls_201.go`, `calls_21.go`, generated) and the 40 request
types of the ocpp-go 2.0.1 CSMS (`cs-ocppgo/requests201.go`, generated). Other
CP-to-CSMS messages are left to the library default (gocpp: NotSupported;
ocpp-go: NotImplemented). On 2.1 `cs-gocpp` also records the SEND
`NotifyPeriodicEventStream`.

| Module | Purpose |
| --- | --- |
| `common/` | stdlib only: event log, canned-response rules, control HTTP API |
| `cs-gocpp/` | gocpp `csms` server, typed v16 / v201 / v21 handlers (`replace` to this checkout) |
| `cs-ocppgo/` | `ocpp16.NewCentralSystem` with every 1.6 profile, `ocpp2.NewCSMS` with every 2.0.1 profile; ocpp-go is pinned to the `shiv3/ocpp-go v0.17.2-fix.3` fork via `replace` |
| `smoke/` | Go test that runs both binaries and compares their answers |

## Build and run

```sh
make            # builds bin/cs-gocpp and bin/cs-ocppgo
make test       # smoke test (also builds the binaries itself if run via go test)
bin/cs-gocpp  -listen 127.0.0.1:9000 -events /tmp/gocpp.jsonl
bin/cs-ocppgo -listen 127.0.0.1:9001 -events /tmp/ocppgo.jsonl
```

Flags (both): `-version 1.6|2.0.1|2.1` (ocpp-go: 1.6|2.0.1), `-listen HOST:PORT` (required), `-events PATH` (JSONL, appended;
default none), `-call-timeout 10s`. `cs-gocpp` also has
`-schema lenient|strict|tolerant|off` (default `lenient`).

Endpoint: `ws://HOST:PORT/ocpp/{cpId}`, subprotocol `ocpp1.6`, no auth, last
connection wins for a duplicate id. Read limit: library default (1 MiB in both).

## Canned responses

- BootNotification: Accepted, interval 300, currentTime `2026-01-01T00:00:00Z`.
- Heartbeat: currentTime `2026-01-01T00:00:00Z`.
- Authorize: idTag prefix `INVALID` or suffix `-INV` -> Invalid, prefix
  `BLOCKED` or suffix `-BLK` -> Blocked, prefix `EXPIRED` or suffix `-EXP` ->
  Expired, otherwise Accepted. (The suffix forms exist because the simulator's
  bundled scenarios hard-code tags such as `CERT023-INV`.)
- StartTransaction: transactionId from a per-server counter starting at 1;
  idTagInfo by the Authorize rule.
- StopTransaction: idTagInfo Accepted if idTag is present, else none.
- StatusNotification, MeterValues, DiagnosticsStatusNotification,
  FirmwareStatusNotification: empty confirmation.
- DataTransfer by vendorId: `parity.reject` -> Rejected; `parity.panic` ->
  records `handler_panic_pending`, then panics; `parity.sleep` -> sleeps `data`
  milliseconds then Accepted; `parity.error` -> application error (gocpp:
  returns an error; ocpp-go: nil confirmation plus error); anything else ->
  Accepted with `data` echoed.
- Everything else keeps the library default (gocpp: NotSupported for the
  security-extension messages; ocpp-go: NotSupported for unknown features).

## Control API (same listener, `/control/`, JSON)

- `POST /control/call {"cpId","action","payload"}` sends a CSMS-initiated 1.6
  request (RemoteStartTransaction, RemoteStopTransaction, Reset,
  ChangeConfiguration, GetConfiguration, ChangeAvailability, UnlockConnector,
  ClearCache, DataTransfer, TriggerMessage, ReserveNow, CancelReservation,
  SetChargingProfile, ClearChargingProfile, GetCompositeSchedule,
  GetLocalListVersion, SendLocalList, UpdateFirmware, GetDiagnostics). The
  payload is decoded into the library's own request type. Answers:
  `{"ok":true,"result":{...}}`,
  `{"ok":false,"callError":{"code","description"}}` or
  `{"ok":false,"error":"...","kind":"timeout|not_connected|invalid_request|other"}`.
  HTTP status is 200 for all of these.
- `POST /control/raw {"cpId","frame"}`: unsupported in both
  (`{"ok":false,"error":"unsupported","kind":"other"}`); neither library
  exposes a raw text-frame write.
- `POST /control/disconnect {"cpId"}`: ocpp-go closes with
  `ws.Server.StopConnection`, gocpp with `Server.Disconnect` (both close code 1000).
- `POST /control/shutdown`: gocpp calls `csms.Server.Shutdown` (graceful close
  of all connections, then the server context is cancelled, so afterwards new
  charge point sockets are accepted and closed again immediately); ocpp-go calls
  `StopConnection` for every connected id (`ws.Server.Stop` would also close
  the HTTP listener and this API). The HTTP listener stays up in both.
- `GET /control/connections`: sorted connected ids.
- `GET /control/events`, `POST /control/events/reset`: in-memory events
  (reset does not touch the JSONL file).
- `GET /control/health`: `{"ok":true,"lib":"gocpp|ocpp-go","version":"..."}`.

## Events

`{"seq","time","kind","cpId","action","payload","detail"}` plus `cause` on
disconnects and `level`/`msg`/`attrs` on log events. Kinds: `connect`,
`disconnect`, `request` (handler invoked; decoded request re-encoded),
`response_sent` (recorded when the handler returns, with the response or an
`error:` detail; not literally when the frame hits the wire),
`handler_panic_pending`, `error` (ocpp-go library error-level logs and
central-system errors), `log` (gocpp Warn/Error slog records via an installed
handler). `cause` is one of `replaced|server_closed|peer_closed|read_error|timeout|unknown`
and `detail` holds the raw library error. ocpp-go does not hand the disconnect
callback a cause, so it is inferred: our own `StopConnection` ->
`server_closed`; an id that reconnected while still registered -> `replaced`;
otherwise from the library's debug line about the read error, normalized by the
same function gocpp's error string goes through (`common.ClassifyReadError`).

## Non-default settings

- ocpp-go: `ocppj` dispatcher timeout set to `-call-timeout` (library default
  30 s), so an unanswered CSMS call is cancelled and does not block that charge
  point's request queue; the ws logger and ocppj logger are replaced by a
  capturing logger; all six 1.6 profiles are registered; `ocppj`'s global
  `FormationViolation` becomes the 1.6 spelling when `Start` runs (library
  behaviour). The ws server binds `:PORT` on all interfaces: ocpp-go's `Start`
  takes only a port, so the host part of `-listen` is ignored.
- gocpp: `WithCallTimeout(-call-timeout)`, `WithLogger` (capturing slog
  handler), `WithSubProtocols("ocpp1.6")`, lenient schema registry by default.
  Ping interval, pong wait, read idle timeout (60 s) and write timeout are the
  library defaults; ocpp-go's are its defaults too (ping wait 60 s,
  write wait 10 s).
- gocpp's CSMS-initiated calls use the typed `csms.Call`, not `CallRaw`, so the
  request goes through the Go struct like in ocpp-go.

## Runner (differential test against the simulator)

`runner/` drives the same simulator traffic (JSON Lines mode of the
charge-point simulator CLI, spawned as a child process) against both servers
and writes `results/<date>/report.md`. Raw artifacts (simulator stdout and
trace, server events and log, normalised observations) go to
`results/<date>/raw/` (git-ignored).

```sh
make                                    # build both servers
bun run interop/parity/runner/main.ts   # all cases, 2 runs (flaky detection)
bun run interop/parity/runner/main.ts --only 'A-*' --runs 1
bun run interop/parity/runner/main.ts --skip-slow   # skip the 75 s idle case
```

Options: `--only glob[,glob]`, `--runs N` (default 2), `--skip-slow`,
`--concurrency N` (default 6 server/simulator pairs), `--out DIR`. `SIM_DIR`
selects the simulator checkout (default `/work/wt/ocpp-cp-sim-parity`). Every
case gets a fresh server process per library (ports from 21000), so a server
that crashes does not affect the next case. Cases: `runner/cases.ts`.

### Group E: the simulator's bundled scenarios

`E-<scenario id>` cases run each bundled OCPP 1.6 scenario template
(`packages/core/src/utils/scenarios/*.json` in the simulator checkout) against
both servers. `runner/scenarios.ts` holds one script per scenario: what the
CSMS has to send (through `/control/call`) once the run is `waiting` for a given
request, or once it reaches a node, plus simulator commands where the scenario
is driven from the charge-point side (plug in, disconnect/connect, ...). Scripts
react to `scenario_status`, not to fixed sleeps. Observed per case: the
`scenario_report` (state, verdict, assertions, CSMS-initiated transcript), the
control-call results, the wire trace and the server events; scripts can add a
path check (`expect`) on the complete wire trace for scenarios that carry no
assertions. A scenario without a script or a skip reason in that file makes the
runner fail at start, so new bundled scenarios cannot be forgotten. The report
gets a group E section with a per-scenario table and the CSMS script; a file
`analysis.md` in the output directory is appended to the report if present.

### Versions 2.0.1 and 2.1 (groups A-E, G)

Cases carry a version (`runner/cases.ts`): the 1.6 ids are unchanged, 2.0.1 ids
carry `201-` after the group letter (`A-201-...`, `D-201-...`, `E-201-...`,
). Groups A (malformed frames and payloads), B, C (connection), D
(flows) run for 1.6 and 2.0.1 against both servers. Group E runs the two `-201`
bundled templates, the version-neutral templates and a few cert16 templates on a
2.0.1 station (`scripts201` in `runner/scenarios.ts`; templates written around
1.6 messages are skipped with a reason).

Group G is OCPP 2.1 against `cs-gocpp` only: the simulator with `--ocpp-version
OCPP-2.1` as the only charge point; SEND (type 6) and other raw frames go through its `send_raw_frame` command (`runner/cases21.ts`). Each flow
records pass/fail checks; the report has a table per flow.

`--gocpp-args "-schema strict"` passes extra flags to `cs-gocpp` only (the
default schema mode is `lenient`), for comparing the strict mode with ocpp-go.
