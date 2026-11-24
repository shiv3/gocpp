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
`cmd/gocpp-csms/legacy_contract_test.go`. Further modes and features will be opt-in
flags and endpoints that leave this mode unchanged.

## Legacy e2e mode

```sh
gocpp-csms --version 1.6      # or 2.0.1, 2.1; env GOCPP_CSMS_VERSION
```

One process serves one OCPP version (WebSocket subprotocol `ocpp1.6`, `ocpp2.0.1`
or `ocpp2.1`). It opens two listeners on ephemeral loopback ports:

- the OCPP listener: a charge point dials `ws://127.0.0.1:<ocpp>/ocpp/<cpId>`
  (no TLS, no authentication);
- the control listener: `http://127.0.0.1:<http>`.

A missing or unknown `--version` prints a message on stderr and exits with status 2.
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
