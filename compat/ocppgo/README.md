# gocpp ocpp-go compatibility module

`github.com/shiv3/gocpp/compat/ocppgo` has the public API of
`github.com/lorenzodonini/ocpp-go` **v0.19.0** and runs on the
gocpp runtime. An application switches to it with one line in its `go.mod`:

```
replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.2
```

## Versions

Use `compat/ocppgo` **`v0.19.0-gocpp.x`** (tags `compat/ocppgo/v0.19.0-gocpp.N`,
for example `v0.19.0-gocpp.2` above). This line tracks upstream
`lorenzodonini/ocpp-go` v0.19.0 and is the only one that is maintained.

Two other sets of tags exist, and they stay on the Go module proxy only because
published module versions are permanent:

- `compat/ocppgo/v0.17.2-gocpp.1` (and its `-rc` tags) on the same module path
  as above. It is unmaintained and deprecated; the `go.mod` of this module
  retracts it, which takes effect from the next `compat/ocppgo` tag on
  (`go list -m -versions` then hides it, and `@latest` never selects it).
- `compat/ocppgo-v019/v0.19.0-gocpp.1`, from an earlier layout in which the
  v0.19.0 module lived in a different directory. That directory is gone; the
  tags are unmaintained and deprecated. Use `compat/ocppgo` instead.

See `MAINTAINING.md` for CI checks, the upstream pin and releasing, and `ROLLOUT.md` for a staged rollout.

Before going live, go through the checklist in
[docs/ocpp-go-structural-issues.md](../../docs/ocpp-go-structural-issues.md#if-you-use-the-drop-in):
it lists what this module changes compared with ocpp-go v0.19.0, what it leaves as it
is, and the settings to turn on.

Status: server side on the gocpp runtime; the client side is upstream's. See `NOTICE` for what is upstream code and what is
new, and `UPSTREAM` for the pinned source.

## What runs on gocpp

- `ocppj.Server` serves every connection on gocpp's per-connection dispatcher:
  frame parsing, handler goroutines with panic recovery (an application handler
  that panics is answered with `InternalError` and the process keeps running),
  one FIFO slot per charge point for outbound calls, call timeouts, teardown.
  Payload validation, error codes and descriptions are still ocpp-go's own code.
- `ws.Server` is implemented on `coder/websocket` with the exact API of ocpp-go
  (gorilla types stay in the signatures, for example
  `StopConnection(id, websocket.CloseError)`). Close codes are sent as close
  frames; `Stop` closes with 1000.
- The client side (`ws.Client`, `ocppj.Client`, `ocpp1.6.ChargePoint`, ...) is
  ocpp-go's unchanged code.

## Wrapping `ws.WsServer` (bridge mode)

The table below applies when the server passed to `ocppj.NewServer` /
`ocpp16.NewCentralSystem` is this module's own `*ws.Server`: then the gocpp
dispatcher owns the sockets (about 1 goroutine per idle connection). If your
application wraps it in its own `ws.WsServer` implementation (or passes any
other `ws.WsServer`), the module runs in *bridge mode*: your server keeps the
sockets and the runtime is fed through its message handler. In that case:

- call every `compat.Set*` that takes a `*ws.Server` (`SetDuplicatePolicy`,
  `SetMaxMessageSize`, `SetIdleClose`, `SetAdmission`, ...) on the inner
  `*ws.Server` that your wrapper delegates to;
- expect about **3 goroutines per idle connection** (measured with 1,000
  connections and a delegating wrapper: 3.00; upstream v0.19.0 with the same
  wrapper: 2.00), not 1. See
  [section 12](../../docs/ocpp-go-structural-issues.md#12-goroutines-per-connection)
  and `interop/ocppgo-repro/r12_per_connection` (`-bridge`).

## Choosing the runtime

| How the server is built | Runtime |
|---|---|
| `ocpp16.NewCentralSystem(nil, ws)`, `ocppj.NewServer(ws, nil, nil)` | gocpp |
| the application passes its own `ServerDispatcher` | ocpp-go's original code |

`OCPPGO_COMPAT_RUNTIME=legacy` forces ocpp-go's original code path everywhere
(operational fallback); `=gocpp` also routes an injected
`*ocppj.DefaultServerDispatcher` to gocpp (used by CI for ocpp-go's own suites).
The package `compat` reports the runtime of a server and sets the call timeout
without injecting a dispatcher.

## Operating a server: the `compat` package

ocpp-go's API has no place for these, so they live in the extra package
`github.com/lorenzodonini/ocpp-go/compat` (the only package that is not in
ocpp-go). All of them need the gocpp runtime and the module's `ws.Server`.

| Need | API |
|---|---|
| Max connections, accept rate (token bucket, 503 + jittered Retry-After), soft cap | `compat.SetAdmission(srv, compat.Admission{...})` before `Start`; `compat.SetSoftMaxConnections` at runtime; `compat.Stats` counts refusals |
| Readiness for a load balancer (503 while draining, stopping or at the soft cap) | `srv.AddHttpHandler("/ready", compat.ReadyHandler(srv).ServeHTTP)` |
| Paced drain for deploys (close code 1012, idle connections first, upgrades refused meanwhile) | `compat.Drain(ctx, srv, compat.DrainOptions{Window, Rate, IdleGrace, RetryAfter, OnProgress})`, then `srv.Stop()`; `compat.Undrain`, `compat.Draining`, `compat.Connections` |
| Metrics (connections, calls per action and direction, pending calls; with `observability.SessionMetrics` also upgrade results and close code/cause), OpenTelemetry tracing, raw frame observer | `compat.SetObservability(endpoint, metrics, tracerProvider, frameObserver)` before `Start`; the Prometheus and OpenTelemetry metrics of gocpp (`core/observability/metrics/...`) fit |
| Call timeout without injecting a dispatcher | `compat.SetCallTimeout` |
| Answer that fails validation: callback gets the validation error at once (default as upstream: CALLERROR to the charger, callback hears "Request timed out" at the call timeout) | `compat.SetValidationErrorToCallback(endpoint, true)` before `Start` |
| Accept the validation quirks of real chargers (empty vendor, long configuration values, empty sampledValue list or value, empty idTag, empty ChangeConfiguration value, vendor measurands and units, numeric sampledValue value, int64-range ids, lenient timestamps, 1.6 error spelling) | `compat.SetValidationLeniency`, `compat.SetValidationErrorFilter`, see below |
| Duplicate charge point id: first wins as upstream (default) or last wins | `compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)` |
| Idle connection: dropped without a close frame as upstream (default) or closed with a frame (1000) | `compat.SetIdleClose(srv, compat.IdleCloseFrame)` before `Start` |
| Inbound message size: no limit as upstream (default) or a limit (close code 1009) | `compat.SetMaxMessageSize(srv, n)` |
| List the connected charge point ids | `compat.ConnectionIDs(srv)` |
| Serve the websocket on your own mux / router (gin, chi, `http.ServeMux`) instead of `Start` listening | `compat.StartWithoutListener(endpoint, srv)`, then `mux.Handle("/ocpp/", compat.Handler(srv))`; stop with the central system's `Stop` |
| Basic auth that knows the charge point id | `compat.SetBasicAuthIDHandler(srv, func(id, user, pass string) bool)` |
| Charge point id from a nested path (`/{org}/{cp}`) or the request | `compat.SetIDResolver(srv, func(r *http.Request, pathID string) (string, bool))` |
| permessage-deflate compression (off as upstream by default) | `compat.SetCompression(srv, compat.CompressionContextTakeover)` |
| Call timeout / idle timeout per connection (server-wide: `SetCallTimeout`, `SetTimeoutConfig`) | `compat.SetConnTimeouts(srv, func(r, id) compat.ConnTimeouts)` |

`SetObservability` needs the `*ocppj.Server`: build it yourself
(`ocppj.NewServer(srv, nil, nil, profiles...)`, then `ocpp16.NewCentralSystem(endpoint, srv)`).

## Lenient validation for real chargers

Upstream ocpp-go v0.19.0 rejects some messages that real chargers send. The default
of this module is upstream's behaviour; leniency is opt-in, per `*ocppj.Server`, through the
`compat` package (no package global, ocpp-go's `ocppj.Validate` is untouched):

```go
endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
compat.SetValidationLeniency(endpoint, compat.Leniency{
	AllowEmptyChargePointVendor:  true, // BootNotification with chargePointVendor "" (upstream: OccurrenceConstraintViolation)
	AllowLongConfigurationValues: true, // GetConfiguration answer with a value over 500 characters (PropertyConstraintViolation)
	AllowEmptySampledValue:       true, // MeterValues with "sampledValue": [] (PropertyConstraintViolation)
	AllowEmptySampledValueValue:  true, // sampledValue {"value": ""} in MeterValues / StopTransaction
	AllowEmptyIdTag:              true, // "idTag": "" in Authorize, StartTransaction (and the RemoteStartTransaction you send)
	AllowEmptyConfigurationValue: true, // ChangeConfiguration with value "" (the request you send; upstream refuses to send it)
	AllowVendorMeasurands:        true, // measurand / unit outside the 1.6 enumerations, e.g. "Energy.Active.lmport.Register"
	AllowNumericSampledValue:     true, // "value": 12.5 instead of "12.5" (reaches the handler as "12.5")
	PreserveLargeIntegers:        true, // numbers decoded as json.Number: transactionId 9223372036854775807 is not rounded
	LenientDateTime:              true, // "2024-01-02 03:04:05", "+0100", comma fraction, missing zone (UTC), date only
	SpecSpellingOccurenceV16:     true, // send "OccurenceConstraintViolation" (the 1.6 spec spelling) on 1.6 connections
})
cs := ocpp16.NewCentralSystem(endpoint, srv)
```

The accepted message reaches your handler (or callback) unchanged; any other
violation in the same message still rejects it, with upstream's error. For other
quirks install a filter, which is called with the action, the field path below the
payload (Go names, e.g. `ConfigurationKey[0].Value`) and the failed validator tag;
a message is accepted only if the filter tolerates every failure it has:

```go
compat.SetValidationErrorFilter(endpoint, func(action, field, tag string) bool {
	return action == "StatusNotification" && field == "VendorErrorCode" && tag == "max"
})
```

Both return false, and change nothing, when the server runs on the legacy runtime
(`OCPPGO_COMPAT_RUNTIME=legacy` or an injected dispatcher): leniency is a feature of
the gocpp runtime only. The preset also applies to OCPP 2.0.1 MeterValues (same
constraint). Leniency covers requests received and answers received by the server.

## Breaking changes vs upstream v0.19.0 (schema conformance)

The module keeps upstream's exported API except where a struct of upstream v0.19.0
contradicts the OCA JSON schemas (`schemas/` in this repository) so that a
schema-valid message cannot be received or sent. Those types follow the schema and
are therefore **breaking for callers of these types** (code that compiles against
upstream's shape needs the change listed). `scripts/apidiff-allow.txt` lists every
such difference, with the schema or upstream issue as the reason; any other API
difference fails CI. Every change is a patch record in `patches/`. Round-trip tests
against the schemas are in `ocpp2.0.1_test/schema_conformance_test.go` and
`ocpp1.6_test/schema_conformance_test.go`.

### Smart charging (OCPP 2.0.1)

| Type | Change | Migrating |
|---|---|---|
| `smartcharging.CompositeSchedule` (upstream issues #355, #385, PR #386) | now the flat schema shape: `ChargingSchedulePeriod []types.ChargingSchedulePeriod`, `EvseID int`, `Duration int`, `ScheduleStart *types.DateTime`, `ChargingRateUnit types.ChargingRateUnitType`; `StartDateTime` and `ChargingSchedule` are removed | read the periods from `Schedule.ChargingSchedulePeriod`; the unit, the start and the EVSE are fields of the schedule |
| `smartcharging.GetCompositeScheduleResponse` | `EvseID` removed (the schema has no top-level `evseId`, it is `Schedule.EvseID`); `StatusInfo *types.StatusInfo` added | use `Schedule.EvseID` |
| `smartcharging.NewGetCompositeScheduleResponse` | `(status)` instead of `(status, evseId)` | drop the second argument, set `Schedule.EvseID` |
| `types.ChargingSchedulePeriod` (upstream PR #418) | `PhaseToUse *int` added (`phaseToUse`) | additive; unkeyed struct literals need the new field |

### Security, ISO 15118, diagnostics, provisioning (OCPP 2.0.1)

| Type | Change | Migrating |
|---|---|---|
| `iso15118.GetCertificateStatusResponse.Status` (upstream issue #429, PR #381) | `types.Certificate15118EVStatus` (`Accepted`, `Failed`) instead of `types.GenericStatus` (`Accepted`, `Rejected`); `NewGetCertificateStatusResponse` takes the same type; `Rejected` is not a schema value and is now refused | use `types.Certificate15188EVStatusAccepted` / `types.Certificate15118EVStatusFailed` |
| `types.IdTokenInfo` | `EvseID []int` (`evseId`) added; the struct is no longer comparable with `==` (so is `authorization.AuthorizeResponse`) | compare fields, not structs |
| `provisioning.GetVariableResult` | `AttributeStatusInfo *types.StatusInfo` added; `attributeValue` is accepted up to 2500 characters (was 1000; `SetVariableData` stays at 1000, as in the schema) | additive |
| `types.OCSPRequestDataType.ResponderURL` | always sent and required (the schema requires `responderURL`); was optional | set it; an empty string is refused |
| `diagnostics.UploadLogStatus` (issue #426, PR #428) | `UploadLogStatusAcceptedCanceled` added and accepted in `LogStatusNotificationRequest` | additive |
| `diagnostics.NotifyCustomerInformationRequest.Data` (issue #425, PR #428) | an empty string is valid (no `required`; the schema has no `minLength`) | none |
| `diagnostics.GetLogResponse.Filename` (issue #424) | at most 255 characters (was 256) | none |
| `iso15118.GetInstalledCertificateIdsRequest.CertificateTypes` (issue #427) | `certificateType` is omitted when the list is empty or nil (it was sent as `null`); omitted means all types | none |

## Known differences

- Outbound requests are ordered per charge point, not globally.
- A panic in an application handler or response callback is contained.
- Requests pending at a disconnect are reported once (by the 1.6 / 2.0.1 glue's
  disconnect handler), as in ocpp-go.
- **Always register the disconnect handler**
  (`SetChargePointDisconnectedHandler` on 1.6,
  `SetChargingStationDisconnectedHandler` on 2.0.1), even with an empty function.
  As in ocpp-go, the code that clears the response callbacks of requests pending at
  a disconnect lives in the wrapper that this method installs. Without it a stale
  callback stays queued and is given the first response after the charge point
  reconnects, which may belong to a different request (the callback queue is
  ocpp-go's own code and matches by position, not by message id).
- `SendResponse` / `SendError` return a hand-over error; write errors surface in
  the logs.
- `InvalidMessageHook` is called for every frame ocpp-go calls it for, including
  envelope errors (wrong array length, unknown message type), and the envelope
  error texts are ocpp-go's own.
- Defaults follow ocpp-go v0.19.0: no inbound size limit, first-wins duplicate ids and
  a dropped (1006) idle connection. gocpp's safer variants are options
  (`compat.SetMaxMessageSize`, `compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)`,
  `compat.SetIdleClose(srv, compat.IdleCloseFrame)`); see "Operating a server".
  If you come from a fork of ocpp-go that switched to last-wins duplicates and
  added message size limits, set `compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)`
  and `compat.SetMaxMessageSize(srv, n)` to keep that behaviour; the defaults here
  are upstream's.
- Idle timeout: like ocpp-go's server, this one never pings. A connection that has
  received no frame (message, ping or pong) for `ws.ServerTimeoutConfig.PingWait`
  (60 s by default) is dropped. A charger that neither pings nor sends anything for
  that long, for example one with a Heartbeat interval above 60 s and no WebSocket
  ping, reconnects every minute; at 80,000 such chargers that is about 1,250
  reconnects per second, several times the CPU and the allocation rate of the same
  fleet that pings (ocpp-go's own client pings every 54 s). Raise `PingWait`
  (`srv.SetTimeoutConfig`) to match the longest silence your chargers keep.
- Upgrades arriving while `Stop` runs get HTTP 503; ocpp-go closes its listener, so they are refused at TCP level (a request already accepted is the only difference).
- ocpp-go's package level state (`ocppj.Validate`, `ocppj.SetMessageValidation`,
  loggers, ...) is unchanged, including that it is not synchronised: changing it
  while another endpoint serves is a data race under `-race`
  ([ocpp-go-structural-issues.md](../../docs/ocpp-go-structural-issues.md), section 10).

## Maintenance

- `scripts/sync-upstream.sh [--check]` re-creates the upstream files from the
  pinned tag and `patches/`; `scripts/refresh-patches.sh` regenerates the patches.
- `scripts/apidiff.sh` fails on any difference in the exported API of any
  package against the pinned tag, except the reviewed schema-conformance
  differences in `scripts/apidiff-allow.txt` (a stale entry fails too).
- The nested module is developed against the repository root through
  `replace github.com/shiv3/gocpp => ../..`; consumers get the version in `require`.
