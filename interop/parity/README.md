# Parity test CSMS pair

Minimal OCPP 1.6 central systems with identical observable behaviour on gocpp
(`cs-gocpp`), on the gocpp ocpp-go drop-in (`cs-ocppgo-compat`) and on upstream
ocpp-go v0.19.0 (`cs-ocppgo-upstream`). They are the servers driven by the
load-test harness in `../loadtest`. The two Go modules keep the root
gocpp module free of an ocpp-go dependency.

| Module | Purpose |
| --- | --- |
| `common/` | stdlib only: event log, canned-response rules, control HTTP API |
| `cs-gocpp/` | gocpp `csms` server, typed v16 handlers (`replace` to this checkout) |
| `cs-ocppgo-upstream/` | `ocpp16.NewCentralSystem` with every 1.6 profile on upstream `lorenzodonini/ocpp-go` v0.19.0, no `replace`: **the ocpp-go baseline** |
| `cs-ocppgo-compat/` | the same server built against `compat/ocppgo` (the gocpp ocpp-go drop-in, `replace` to this checkout; `-first-wins` selects upstream's duplicate id policy); no dispatcher is injected, so it runs on the gocpp runtime |

## Build and run

```sh
make            # builds bin/cs-gocpp, bin/cs-ocppgo-upstream and bin/cs-ocppgo-compat
bin/cs-gocpp  -listen 127.0.0.1:9000 -events /tmp/gocpp.jsonl
bin/cs-ocppgo-upstream -listen 127.0.0.1:9001 -events /tmp/ocppgo.jsonl
```

Flags (both): `-listen HOST:PORT` (required), `-events PATH` (JSONL, appended;
default none), `-call-timeout 10s`. `cs-gocpp` also has
`-schema lenient|strict|tolerant|off` (default `lenient`). `-compat` turns on `csms.WithOCPPGoCompat()` (implies lenient); `-first-wins` and `-unlimited-size` add the two opt-in behaviours that preset leaves out.

Endpoint: `ws://HOST:PORT/ocpp/{cpId}`, subprotocol `ocpp1.6`, no auth, last
connection wins for a duplicate id in cs-gocpp (first wins in upstream ocpp-go; `cs-gocpp -first-wins` matches it). Read limit: 1 MiB in cs-gocpp, none in upstream (`-unlimited-size`).

## Canned responses

- BootNotification: Accepted, interval 300, currentTime `2026-01-01T00:00:00Z`.
- Heartbeat: currentTime `2026-01-01T00:00:00Z`.
- Authorize: idTag prefix `INVALID` or suffix `-INV` -> Invalid, prefix
  `BLOCKED` or suffix `-BLK` -> Blocked, prefix `EXPIRED` or suffix `-EXP` ->
  Expired, otherwise Accepted. 
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
  `ws.Server.StopConnection` (close code 1000). gocpp has no per-connection
  close in its public API, so it answers `unsupported`.
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

## `cs-ocppgo-upstream` notes

Same contract as the other servers (control API, canned responses, events, `-lite`,
`-pprof`, `-call-timeout`) on upstream `github.com/lorenzodonini/ocpp-go` at its
latest release (v0.19.0), with no `replace`.

- `-version 1.6|2.0.1` exists for the contract, but only `1.6` is implemented:
  the load generator does not speak 2.0.1 for any server.
- Duplicate connection id: upstream is **first-wins**. A second connection with
  a registered id is upgraded, then closed with 1008 ("a connection with this
  ID already exists") and the old one stays (cs-gocpp is last-wins by default and closes
  the old socket). So no `replaced` disconnect cause exists here; the rejected
  socket produces an `error` event ("client X already exists, closing
  duplicate client") and no `connect`.
- A dead peer keeps its slot until the 60 s read timeout, and reconnects in that
  window are refused (1008).
- The server read limit is unlimited (cs-gocpp caps at 1 MiB); there is no
  `ServerTimeoutConfig.MaxMessageSize`.
- `ws.Server.Write` blocks on a 1-slot outbound channel while holding the
  connection-map read lock 
  `StopConnection` can block or panic on a closed channel, and the ping handler
  can block the read loop. These are library behaviours, not changes here.
- Not changed: dispatcher timeout set to `-call-timeout`, all six profiles
  registered, ws/ocppj loggers captured, listener binds all interfaces.
