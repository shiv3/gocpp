# gocpp ocpp-go compatibility module

`github.com/shiv3/gocpp/compat/ocppgo` has the public API of
`github.com/lorenzodonini/ocpp-go` **v0.19.0** and runs on the
gocpp runtime. An application switches to it with one line in its `go.mod`:

```
replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.2
```

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
| Accept the validation quirks of real chargers (empty vendor, long configuration values, empty sampledValue, 1.6 error spelling) | `compat.SetValidationLeniency`, `compat.SetValidationErrorFilter`, see below |
| Duplicate charge point id: first wins as upstream (default) or last wins | `compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)` |
| Idle connection: dropped without a close frame as upstream (default) or closed with a frame (1000) | `compat.SetIdleClose(srv, compat.IdleCloseFrame)` before `Start` |
| Inbound message size: no limit as upstream (default) or a limit (close code 1009) | `compat.SetMaxMessageSize(srv, n)` |

`SetObservability` needs the `*ocppj.Server`: build it yourself
(`ocppj.NewServer(srv, nil, nil, profiles...)`, then `ocpp16.NewCentralSystem(endpoint, srv)`).

## Lenient validation for real chargers

Upstream ocpp-go v0.19.0 rejects some messages that real chargers send, and
forks of ocpp-go commonly relax exactly these. The default of this module is
upstream's behaviour; leniency is opt-in, per `*ocppj.Server`, through the
`compat` package (no package global, ocpp-go's `ocppj.Validate` is untouched):

```go
endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
compat.SetValidationLeniency(endpoint, compat.Leniency{
	AllowEmptyChargePointVendor:  true, // BootNotification with chargePointVendor "" (upstream: OccurrenceConstraintViolation)
	AllowLongConfigurationValues: true, // GetConfiguration answer with a value over 500 characters (PropertyConstraintViolation)
	AllowEmptySampledValue:       true, // MeterValues with "sampledValue": [] (PropertyConstraintViolation)
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
- Idle timeout: like ocpp-go's server, this one never pings. A connection that has
  received no frame (message, ping or pong) for `ws.ServerTimeoutConfig.PingWait`
  (60 s by default) is dropped. A charger that neither pings nor sends anything for
  that long, for example one with a Heartbeat interval above 60 s and no WebSocket
  ping, reconnects every minute; at 80,000 such chargers that is about 1,250
  reconnects per second, several times the CPU and the allocation rate of the same
  fleet that pings (ocpp-go's own client pings every 54 s). Raise `PingWait`
  (`srv.SetTimeoutConfig`) to match the longest silence your chargers keep.
- Upgrades arriving while `Stop` runs get HTTP 503; ocpp-go closes its listener, so they are refused at TCP level (a request already accepted is the only difference).
- ocpp-go's package level state (`ocppj.FormationViolation`, loggers, ...) is
  unchanged, including its data races when several endpoints start at once.

## Maintenance

- `scripts/sync-upstream.sh [--check]` re-creates the upstream files from the
  pinned tag and `patches/`; `scripts/refresh-patches.sh` regenerates the patches.
- `scripts/apidiff.sh` fails on any difference in the exported API of any
  package against the pinned tag.
- The nested module is developed against the repository root through
  `replace github.com/shiv3/gocpp => ../..`; consumers get the version in `require`.
