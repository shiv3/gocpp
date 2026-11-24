# gocpp-csms: a standalone test CSMS

`gocpp-csms` is a small central system (CSMS) built on gocpp for testing charge
points: real chargers, simulators and charge-point code under test dial into it, it
answers with sensible defaults, records what they send, and lets a test send
CSMS-initiated commands and change its answers over an HTTP control API.

```sh
go install github.com/shiv3/gocpp/cmd/gocpp-csms@latest
```

The binary is also in the release archives next to `gocpp-validate` and `gocpp-sim`.
It lives in the root module, so `go install ...@<version>` works with any tag or
pseudo-version.

It is the CSMS used by the e2e suite of
[ocpp-cp-simulator](https://github.com/shiv3/ocpp-cp-simulator), which used to carry
its own copy under `e2e/csms/`. The mode that suite uses is described below as the
**legacy e2e mode**; its interface is frozen and pinned by
`cmd/gocpp-csms/legacy_contract_test.go`.

The **serve mode** (`gocpp-csms serve`) is the configurable CSMS for everything else:
one listener for OCPP 1.6, 2.0.1 and 2.1, TLS, mTLS and Basic Auth, health, readiness
and Prometheus endpoints, a generic command API for every CSMS-initiated action, and
schema-valid answers to every charge-point-initiated action. New features are opt-in
flags and endpoints of the serve mode; the legacy e2e mode does not change.

| Started as | Mode |
|---|---|
| `gocpp-csms --version 1.6\|2.0.1\|2.1` | legacy e2e mode (frozen) |
| `gocpp-csms serve [flags]` | serve mode |
| `gocpp-csms` (nothing) | usage error, exit status 2 (kept from the legacy contract) |

## Legacy e2e mode

```sh
gocpp-csms --version 1.6      # or 2.0.1, 2.1; env GOCPP_CSMS_VERSION
```

One process serves one OCPP version (WebSocket subprotocol `ocpp1.6`, `ocpp2.0.1`
or `ocpp2.1`). It opens two listeners on ephemeral loopback ports:

- the OCPP listener: a charge point dials `ws://127.0.0.1:<ocpp>/ocpp/<cpId>`
  (no TLS, no authentication);
- the control listener: `http://127.0.0.1:<http>`.

A missing or unknown `--version` prints a message on stderr and exits with status 2
(without `--version` the message points at `gocpp-csms serve`).
SIGINT or SIGTERM shuts both listeners down, flushes stdout and exits 0. All logs go
to stderr.

### stdout

The first stdout line announces the ports and is flushed at once:

```
E2E_CSMS_PORTS {"ocpp":40123,"http":40124}
```

Every following line is one NDJSON record of a CALL the charge point sent and the
CSMS handled, written before the CSMS answers it:

```json
{"seq":1,"cpId":"CP1","action":"BootNotification","payload":{"chargePointModel":"M","chargePointVendor":"V"}}
```

`seq` starts at 1 and counts across all connections. Inbound handlers run one at a
time over the whole process, so `seq` order is the order the frames arrived in.
`payload` is the decoded request encoded again (not the raw frame). Actions without a
handler are answered with a `NotSupported` CALLERROR and are not recorded.

Handled actions:

| Version | Actions |
|---|---|
| 1.6 | BootNotification, Heartbeat, StatusNotification, Authorize, StartTransaction, StopTransaction, MeterValues, DataTransfer |
| 2.0.1, 2.1 | BootNotification, Heartbeat, StatusNotification, Authorize, TransactionEvent, MeterValues, NotifyReport, DataTransfer |

### Default answers

- BootNotification: `Accepted`, `interval` 300, `currentTime` now (UTC).
- Heartbeat: `currentTime` now.
- Authorize: status `Accepted` (`idTagInfo` in 1.6, `idTokenInfo` in 2.x).
- 1.6 StartTransaction: `idTagInfo` `Accepted` and a `transactionId` from a
  process-wide counter starting at 1. StopTransaction: `{}`.
- 2.x TransactionEvent: `{}` (an `idTokenInfo` is added only while an authorization
  policy other than `accept-all` is active).
- DataTransfer: `Accepted`. StatusNotification, MeterValues, NotifyReport: `{}`.

Decimals (for example a charging schedule `limit`) are written as JSON numbers.

### Control API

Every answer is `{"ok":bool,"result":...,"error":"..."}` with `Content-Type:
application/json`; `result` and `error` are left out when empty. A wrong method
answers 405.

**`GET /healthz`** answers `{"ok":true}`.

**`POST /command`** sends one CSMS-initiated message. The body names the charge point
and the action, with the request fields at the top level as in OCPP:

```json
{"cpId":"CP1","action":"RemoteStartTransaction","connectorId":1,"idTag":"TAG1"}
```

- 400: invalid JSON, missing `cpId` or `action`; 404: the charge point is not
  connected.
- 200 `{"ok":true,"result":<the charge point's response>}` on success.
- 200 `{"ok":false,"error":...}` when the call fails: CALLERROR from the charge
  point, no answer within 5 s, or an action this version does not support here.

Supported actions:

| Version | Actions |
|---|---|
| 1.6 | RemoteStartTransaction, RemoteStopTransaction, Reset, ReserveNow, CancelReservation, SetChargingProfile, ClearChargingProfile, GetCompositeSchedule |
| 2.0.1 | RequestStartTransaction, RequestStopTransaction, Reset, ChangeAvailability, TriggerMessage, UnlockConnector, GetVariables, SetVariables, GetBaseReport, ReserveNow |
| 2.1 | RequestStartTransaction, RequestStopTransaction, UsePriorityCharging, ReserveNow |

`remoteStartId` of RequestStartTransaction is filled from a counter when omitted.

**`/behavior`** switches how the CSMS answers. `GET` returns the state, `POST`
applies a change (unknown fields or invalid values: 400, nothing changed), `DELETE`
restores the defaults. All fields are optional; a field left out keeps its value.

```json
{
  "boot": ["Pending", "Accepted"],
  "bootIntervalSeconds": 300,
  "mode": "accept-all",
  "idTags": [],
  "rateMax": 1,
  "rateWindowMs": 60000
}
```

- `boot`: BootNotification statuses (`Accepted`, `Pending`, `Rejected`) answered in
  order per charge point; the last one repeats. Setting it restarts every charge
  point's position.
- `mode`:
  - `accept-all` (default);
  - `whitelist`: idTags not in `idTags` are `Invalid` (1.6) / `Unknown` (2.x);
  - `blacklist`: idTags in `idTags` are `Blocked`;
  - `rate-limit`: more than `rateMax` authorizations of one idTag within
    `rateWindowMs` are `Blocked` (Authorize and the idTag of
    StartTransaction / TransactionEvent each count);
  - `offline`: open OCPP connections are dropped and new WebSocket upgrades answer
    503 until another mode is set. The control API stays up.

**`/sequence`** runs CSMS-initiated commands in the background:

```json
{"steps":[{"delayMs":500,"command":{"cpId":"CP1","action":"ReserveNow", "...": "..."}},
          {"delayMs":1000,"command":{"cpId":"CP1","action":"CancelReservation","reservationId":1}}],
 "periodMs":10000, "repeat":3}
```

`POST` answers `{"ok":true,"result":{"id":N}}`. Each `command` is a `/command` body;
`delayMs` counts from the previous step. With `periodMs` the whole sequence starts
again that long after the previous round began, `repeat` rounds in total (0 = until
stopped); `repeat` above 1 needs `periodMs`. `GET` lists the runs with the result of
every step so far (`round`, `step`, `ok`, `result`, `error`); `DELETE` stops all
runs.

## Using it from a test harness

1. Start `gocpp-csms --version <v>` with stdout piped.
2. Read lines until one starts with `E2E_CSMS_PORTS `; parse the JSON after it.
3. Poll `GET /healthz` until it answers `{"ok":true}`.
4. Point the charge point at `ws://127.0.0.1:<ocpp>/ocpp/<cpId>`.
5. Treat every later stdout line as a frame record; use `seq` to wait for frames
   sent after a given point.
6. Stop with SIGTERM.

## Serve mode

```sh
gocpp-csms serve                                   # ws://:8080/ocpp/<cpId>, control on 127.0.0.1:8081
gocpp-csms serve --ocpp-addr :443 --tls-cert cert.pem --tls-key key.pem \
  --basic-auth-file chargers.txt --log-level debug
```

One OCPP listener serves every configured version; the WebSocket subprotocol a charge
point offers (`ocpp1.6`, `ocpp2.0.1`, `ocpp2.1`) picks the version of its connection.
The charge point id is the last path segment under the path prefix, so
`/ocpp/CP1` and `/ocpp/site-a/CP1` both connect `CP1` (a prefix without a trailing
slash is treated as if it had one).

### Flags

Every flag has an environment default: `--foo-bar` reads `GOCPP_CSMS_FOO_BAR`; a flag
on the command line wins. An environment value that does not parse is a usage error
(exit status 2).

| Flag | Default | Meaning |
|---|---|---|
| `--ocpp-addr` | `:8080` | OCPP WebSocket listen address |
| `--control-addr` | `127.0.0.1:8081` | control API, `/healthz`, `/readyz`, `/metrics` |
| `--path-prefix` | `/ocpp/` | path charge points dial under; id = last segment |
| `--versions` | `2.1,2.0.1,1.6` | accepted OCPP versions, in order of preference |
| `--tls-cert`, `--tls-key` | | serve `wss://` with this PEM certificate and key |
| `--tls-client-ca` | | require a client certificate signed by this PEM CA bundle (mTLS) |
| `--mtls-match-cpid` | `false` | also require the client certificate's common name to equal the charge point id |
| `--basic-auth-file` | | require HTTP Basic Auth (see below) |
| `--log-format` | `json` | `json` or `text`, on stderr |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `--max-connections` | `0` | simultaneous charge points (0 = unlimited); beyond it upgrades get 503 |
| `--inbound-concurrency` | `0` | inbound handlers running at once over all connections (0 = unlimited) |
| `--drain-timeout` | `30s` | on SIGTERM, time to close charge point connections |
| `--command-timeout` | `30s` | timeout of a CSMS-initiated command |
| `--boot-interval` | `300` | `interval` of BootNotification answers |
| `--schema` | `strict` | validation of charge point messages: `strict`, `tolerant`, `lenient`, `off` |
| `--record` | `false` | NDJSON frame lines on stdout (format of the legacy mode) |
| `--announce` | `false` | `E2E_CSMS_PORTS {"ocpp":N,"http":N}` as the first stdout line |

Without `--record` and `--announce` stdout stays empty; logs always go to stderr.
`--record` writes the line before the CALL is answered; with
`--inbound-concurrency 1` its `seq` is the receive order over all connections, as in
the legacy mode (otherwise only per-connection order is likely, not guaranteed).

### Authentication

`--basic-auth-file` lists one charge point per line, `cpId:secret`, where the secret
is the password in clear or its bcrypt hash (`$2a$`, `$2b$`, `$2y$`); blank lines and
`#` comments are ignored; the id ends at the first colon. A charge point not in the
file, without credentials or with a wrong password is refused with 401 before the
WebSocket upgrade (OCPP security profiles 1 and 2).

```
# chargers.txt
CP1:correct horse battery staple
CP2:$2b$10$Vb6...
```

`--tls-client-ca` makes the TLS handshake require a client certificate from that CA
(profile 3); add `--mtls-match-cpid` to bind the certificate to the charge point id.
Basic Auth and the certificate check can be combined; both must pass.

### Answers to the charge point

Every action a charge point may send in its version is answered with a schema-valid
answer that accepts it; nothing is answered NotSupported:

- BootNotification `Accepted` with `--boot-interval` and `currentTime`; Heartbeat
  `currentTime`.
- Authorize: `Accepted` (`idTagInfo` in 1.6, `idTokenInfo` in 2.x).
- DataTransfer, SignCertificate, GetCertificateStatus, NotifyEVChargingNeeds,
  NotifyEVChargingSchedule, RequestBatterySwap, PullDynamicScheduleUpdate:
  `{"status":"Accepted"}`. Get15118EVCertificate: `Failed` (an EXI installation
  response cannot be produced). VatNumberValidation (2.1): `Accepted`, echoing `vatNumber`.
  GetCertificateChainStatus (2.1): every requested certificate `Good`, next update in
  24 h.
- Everything else (StatusNotification, MeterValues, firmware, diagnostics and log
  status, SecurityEventNotification, the 2.x Notify* family, ReportChargingProfiles,
  ClearedChargingLimit, ReservationStatusUpdate, ...): `{}`. The 2.1 SEND
  NotifyPeriodicEventStream is accepted (and recorded).

Transactions are tracked per charge point and connector:

- 1.6 StartTransaction gets a `transactionId` from a process-wide counter (1, 2, ...)
  and `idTagInfo` `Accepted`; a transaction still open on the same connector is ended
  (`Replaced`). StopTransaction ends it (`idTagInfo` `Accepted` when the request
  carries an `idTag`, otherwise `{}`). MeterValues with a `transactionId` mark it
  updated.
- 2.x TransactionEvent `Started`/`Updated`/`Ended` creates, updates and ends the
  transaction with the charge point's `transactionInfo` (id, charging state, stopped
  reason, `seqNo`, EVSE and connector, energy register); the answer carries
  `idTokenInfo` `Accepted` when the event has an `idToken`, otherwise `{}`.

### Control API

The control API has no authentication of its own: keep `--control-addr` on loopback
(the default) or behind something that authenticates. Same envelope as the legacy
mode, plus `errorCode` (the OCPP error code when the
charge point answered with a CALLERROR).

- `GET /healthz` - `{"ok":true}` while the process runs.
- `GET /readyz` - `ok` (200), or 503 while draining or shutting down.
- `GET /metrics` - Prometheus: gocpp's connection, call, schema and session metrics
  plus Go and process metrics.
- `POST /command` - any CALL the CSMS may send in the charge point's negotiated
  version, with the request as `payload`:

  ```json
  {"cpId":"CP1","action":"SetChargingProfile","payload":{"connectorId":1,"csChargingProfiles":{...}}}
  ```

  The legacy flat shape (request fields next to `cpId` and `action`) is accepted too.
  The payload is validated against the version's request schema before it is sent.
  400: bad JSON, missing `cpId`/`action`, `payload` mixed with other fields, an action
  the CSMS cannot send in that version (`unsupported 1.6 action "X"`), a payload that
  does not match the schema; 404: not connected; 200 `{"ok":true,"result":<answer>}`;
  200 `{"ok":false,"error":..,"errorCode":..}` for a CALLERROR, or `ok:false` without
  `errorCode` for a timeout or a dropped connection.
- `GET /connections` - `[{"cpId","version","subprotocol","remoteAddr"}]`.
- `GET /transactions[?cpId=X][&active=true]` - the transaction store (active ones and
  the last 1000 ended ones), ordered by creation.

### Shutdown

SIGTERM or SIGINT starts a drain: new upgrades are refused with 503 and `Retry-After`,
`/readyz` fails, and the open connections are closed (WebSocket close code 1012)
spread over `--drain-timeout`; then both listeners stop, stdout is flushed and the
process exits 0.

## Controller profile

The controller profile lets serve mode drive a charge point the way a home or fleet
energy-management controller does, over OCPP 1.6 and 2.0.1/2.1: it keeps a state model
of every charge point and connector, configures the charge point after boot, sets
charging limits and can start transactions. It is specified from the OCPP behaviour
only and is opt-in: without any of the flags below, nothing is registered and the
answers on the wire are those of plain serve mode. Any of `--controller`,
`--controller-setup`, `--controller-watchdog`, `--controller-remote-start-idtag`,
`--controller-boot-policy`, `--controller-faults` or `--controller-trigger-boot-after`
enables the state model and the REST endpoints.

### State model

Fed by every handled CALL (the inbound observer), per charge point:

- Boot data (vendor, model, serial number, firmware, reason, the answer status). A
  BootNotification is a reboot: the running transactions end (`Reboot`) and the
  connectors' status clocks restart.
- Per connector (1.6 connector id, 2.x EVSE id; 0 = the charge point): status and
  error code with their timestamp. A status older than the stored one is ignored; a
  report without a timestamp counts as received now. `Available` clears a stale
  transaction.
- Last measurements per measurand and phase, scaled to the base unit (`kWh` -> `Wh`,
  `kW` -> `W`, `mA` -> `A`; 2.x `unitOfMeasure.multiplier` applied). Values with an
  older timestamp than the stored one are dropped.
- The running transaction: from StartTransaction (1.6) or TransactionEvent (2.x);
  recovered from the `transactionId` of 1.6 MeterValues when unknown; cleared by
  StopTransaction, `Ended` and a reboot.
- Last seen, capabilities learned by the setup (rate unit, maximum stack level, phase
  switching, accepted measurands), the configuration read, the limits applied through
  the helper and the setup report.

| Endpoint | |
| --- | --- |
| `GET /chargepoints` | every charge point seen or connected |
| `GET /chargepoints/{id}` | one charge point; 404 when unknown |

### Setup after boot (`--controller-setup`)

After an Accepted BootNotification (and once its answer was sent), in a background
task; a failed step is recorded in the setup report (`setup` of the station) and does
not stop the sequence.

1.6: ChangeAvailability(0, Operative) (skip with `--controller-setup-skip-availability`);
GetConfiguration for all keys, falling back to an explicit key list when that fails;
capabilities from `ChargeProfileMaxStackLevel`,
`ChargingScheduleAllowedChargingRateUnit` (`Power`/`W` only = watts, otherwise amps),
`ConnectorSwitch3to1PhaseSupported` / `ACPhaseSwitchingSupported`,
`SupportedFeatureProfiles`; measurand detection by ChangeConfiguration of
`MeterValuesSampledData` with one candidate at a time (`--controller-measurands`, a
comma separated list or `-name` to drop one from the default
`Power.Active.Import,Energy.Active.Import.Register,Current.Import,Voltage,Current.Offered,Power.Offered,SoC`),
the accepted ones cut to `MeterValuesSampledDataMaxLength` and set as one list (not
when the key is read-only); `MeterValueSampleInterval` (`--controller-meter-interval`),
`WebSocketPingInterval` (`--controller-ping-interval`, 0 = leave); TriggerMessage
StatusNotification and MeterValues for every connector (`NumberOfConnectors`) unless
the feature profiles lack RemoteTrigger.

2.x: ChangeAvailability (whole station); GetVariables of the SampledDataCtrlr,
OCPPCommCtrlr and SmartChargingCtrlr variables the helper needs (`RateUnit`,
`ProfileStackLevel`, `ACPhaseSwitchingSupported`); measurand detection with
SetVariables of `SampledDataCtrlr.TxUpdatedMeasurands`; `TxUpdatedInterval`,
`OCPPCommCtrlr.WebSocketPingInterval`; TriggerMessage StatusNotification and
MeterValues.

`--controller-trigger-boot-after D` sends TriggerMessage(BootNotification) to a charge
point that has not booted D after connecting. The meter watchdog
(`--controller-watchdog N`) sends TriggerMessage(MeterValues) for a connector with a
running transaction that sent no meter values for N meter intervals, then waits another
N intervals.

### Charging limits

`POST /chargepoints/{id}/limit`

```json
{"connectorId": 1, "amps": 16, "phases": 3, "purpose": "default", "stackLevel": 2, "relative": false, "profileId": 1}
```

Exactly one of `amps` and `watts` (0 pauses charging). It becomes a single-period
SetChargingProfile (period start 0), in the charge point's unit: a limit given in the
other unit is converted with `--controller-nominal-voltage` (default 230) and `phases`
(default 3; `phases` is also sent as `numberPhases`, which is how 1p/3p switching is
done). Values are truncated to 0.1. `purpose`: `default` (TxDefaultProfile, the
default), `tx` (TxProfile with the connector's running transaction; 409 without one)
or `max` (ChargePointMaxProfile in 1.6, ChargingStationMaxProfile in 2.x). The
profile is Absolute with a schedule starting one minute ago, or Relative with
`relative`. `stackLevel` defaults to the charge point's maximum when known, else 0;
the profile id to `--controller-profile-id` (default 1), so a new limit replaces the
previous one. 1.6 sends `connectorId` + `csChargingProfiles`, 2.x `evseId` +
`chargingProfile` (with `chargingSchedule` as an array). The answer is
`{"ok":true,"result":{"limit":...,"request":...,"response":...}}`; a charge point
answer other than `Accepted` gives `ok:false`.

`DELETE /chargepoints/{id}/limit[?connectorId=&purpose=&stackLevel=]` sends
ClearChargingProfile for those criteria (without any: the helper's profile id).
`GET /chargepoints/{id}/schedule[?connectorId=&duration=60&unit=A|W]` sends
GetCompositeSchedule and returns the charge point's answer.

### Policies

- Remote start (`--controller-remote-start-idtag TAG`): when a connector turns
  `Preparing` (1.6) or `Occupied` (2.x) and has no transaction, send
  RemoteStartTransaction / RequestStartTransaction with that id tag. Never stops a
  transaction.
- Boot policy (`--controller-boot-policy`): answer BootNotification `Pending` (interval
  `--controller-boot-pending-interval`, default 30 s) for every charge point not in
  `--controller-known-cps` (comma separated). Known ones get the normal answer.
- Lenient parsing: `--lenient-empty-numbers`, `--lenient-missing-charging-rate-unit`,
  `--lenient-vendor-measurands` and `--lenient-time-location ZONE` map to the library's
  lenient options and switch the schema mode to lenient (they win over `--schema`);
  `--compat-preset` applies the library's drop-in compatibility preset first.

### Fault injection (`--controller-faults`)

Faults are changes to the answers the CSMS gives to charge point CALLs, set at runtime:

```sh
curl -X PUT localhost:8081/faults -d '{"rules":[
  {"action":"Heartbeat","kind":"latency","delay":"2s"},
  {"action":"Authorize","cpId":"CP1","kind":"error","errorCode":"InternalError","description":"boom","count":3},
  {"action":"DataTransfer","kind":"ignore"}]}'
```

`PUT` replaces all rules, `GET /faults` lists them, `DELETE /faults` removes them. A rule
matches an `action` (empty or `*` = any) and optionally a `cpId`; the first match
applies; `count` limits it to that many CALLs. Kinds: `latency` (answer after `delay`),
`error` (CALLERROR with `errorCode`), `ignore` (never answered: the CALL is held until
the connection ends, at most ten minutes; the state model does not see it). Duplicate
responses are not supported: the library has no way to write a frame twice, and a
`duplicate` rule is refused with 400.

## Architecture

```
cmd/gocpp-csms/
  main.go                  mode switch: "serve" -> internal/server, otherwise internal/legacy
  ext_<name>.go            one file per serve-mode extension (init appends to serveExtensions)
  legacy_contract_test.go  the frozen legacy contract (runs the real binary)
  serve_test.go            serve mode as a process
  internal/
    legacy/    legacy e2e mode (frozen): typed handlers, /behavior, /sequence, offline gate
    recorder/  NDJSON frame lines and the ports sentinel (shared, format frozen)
    config/    serve flags, GOCPP_CSMS_* env defaults, Binder for extension flags
    server/    serve wiring: listeners, TLS, auth, metrics, health, drain; Extension, Hooks, Runtime
    actions/   per-version action sets by direction (from the generated handler interfaces), schema registry
    inbound/   answers to charge point CALLs (accept-all defaults), Override / Observe
    dispatch/  CSMS->CP commands: schema validation + CallRaw, POST /command
    txn/       transaction store (1.6 ids, 2.x transactionInfo)
    authn/     Basic Auth file, client certificate check, authenticator composition
    httpapi/   control API envelope
```

The legacy package is closed: nothing in serve mode imports it, and changes to it are
changes to the frozen contract. Serve-mode features build on the packages above
without editing them, through these extension points:

- **`server.Extension`** (`Name`, `Flags(*config.Binder)`, `Setup(*server.Hooks)`):
  an optional feature lives in its own `internal/<feature>` package and is compiled in
  by a file `cmd/gocpp-csms/ext_<feature>.go` whose `init` appends it to
  `serveExtensions`. Its flags are declared through the Binder (env defaults follow
  the same rule) and should default to "off".
- **`server.Hooks`** (during `Setup`):
  - `Inbound.Use(middleware)` wraps every answer (fault injection);
  - `Inbound.Override(version, action, responder)` replaces one answer
    (`Inbound.Default` gives the built-in one to wrap); `Inbound.Observe(fn)` sees every
    handled CALL with its answer.
  - `FrameObserver(fn)` - raw frames in both directions, fan-out (gocpp takes a single
    observer; the server multiplexes).
  - `OnConnect(fn)` / `OnDisconnect(fn)` - fan-out as well.
  - `Handle(pattern, handler)` - control API routes (`http.ServeMux` patterns, e.g.
    `GET /chargepoints/{id}`).
  - `SetAuthenticator(a)` / `Authenticator()` - replace (or wrap) the static
    authenticator, `SetCPIDExtractor(fn)` - change how the id is read from the URL.
  - `CSMSOptions(...)` - any other `csms.Option`; `Metrics` - register Prometheus
    collectors; `Txns`, `Recorder` - shared state.
  - `OnStart(fn(ctx, *Runtime))` - runs once listening; `Runtime` has the
    `csms.Server`, the `dispatch.Dispatcher` (send validated commands with
    `Dispatcher.Send`) and the stores; `ctx` ends at shutdown.

Planned extensions and where they attach:

- *Hosted test CSMS* (message log, registry): `FrameObserver` for a log of every frame
  including CALLRESULT/CALLERROR and outbound calls (the stdout recorder format stays as
  it is), `Handle` for `/messages` and the registry API, `SetAuthenticator` for
  per-charge-point passwords and open/private modes, `SetCPIDExtractor` for an
  instance segment in the URL, `OnConnect` and `CSMSOptions` for connection limits.
- *Smart-charging controller profile* (per-connector state model): `Inbound.Observe`
  for StatusNotification, MeterValues, BootNotification and transactions,
  `Inbound.Override` for boot policies, `OnStart` + `Runtime.Dispatcher` for setup
  sequences and limits, `Handle` for `/chargepoints` and `Txns.EndAll` for reboots.
