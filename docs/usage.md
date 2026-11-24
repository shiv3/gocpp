# gocpp usage guide

## Concepts

- **Message values** are generated per version as `ocppj.Message[Req,Resp]` values in
  `v16/profiles`, `v201/profiles`, `v21/profiles` (e.g. `v16profiles.BootNotification`).
  They carry the action name and direction, so `On`/`Call` infer `Req`/`Resp`.
- **Direction** is enforced: a CSMS may only `On` messages the charge point sends
  (`SentByCP`) and only `Call` messages it sends to the CP (`SentByCSMS`); the CP is the
  mirror. Wrong-direction registration returns `ocppj.ErrInvalidDirection`.

## CSMS

```go
srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1", "ocpp2.0.1", "ocpp1.6"))

// Handle an inbound (charge-point-originated) message.
csms.On(srv, v201p.Authorize, func(ctx context.Context, c *csms.Conn, req v201msg.AuthorizeRequest) (v201msg.AuthorizeResponse, error) {
    return v201msg.AuthorizeResponse{IDTokenInfo: v201msg.IdTokenInfoType{Status: "Accepted"}}, nil
})

go srv.ListenAndServe(":8080")

// Send a CSMS-originated message to a connected charge point.
if conn, ok := srv.Get("CP_1"); ok {
    _, err := v201client.NewCSMS(conn).Reset(ctx, v201msg.ResetRequest{Type: "OnIdle"})
    _ = err
}
```

`v201client.NewCSMS(conn)` (from `github.com/shiv3/gocpp/v201/client`) wraps the
`*csms.Conn` with a typed send method per CSMS-originated message. The low-level
`csms.Call(ctx, conn, v201p.Reset, req)` is still available if you prefer it.

`csms.NewServer().Handler()` returns an `http.Handler` if you want to mount the
WebSocket endpoint on your own mux / TLS server.

## Charge point

```go
client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1"))

// Handle a CSMS-originated message.
cp.On(client, v201p.Reset, func(ctx context.Context, req v201msg.ResetRequest) (v201msg.ResetResponse, error) {
    return v201msg.ResetResponse{Status: "Accepted"}, nil
})

client.Connect(ctx)              // single connection
// or client.Run(ctx)           // auto-reconnect with exponential backoff

// Typed send method per CP-originated message (from github.com/shiv3/gocpp/v201/client).
cpc := v201client.NewCP(client)
resp, err := cpc.BootNotification(ctx, req)
// Low-level equivalent: cp.Call(ctx, client, v201p.BootNotification, req)
```

The wrapper embeds `*cp.Client`, so `cpc.Connect`, `cpc.Close`, `cpc.IsConnected`,
etc. remain available — it can serve as your primary client handle.

## Bulk handler registration

Instead of one `cp.On` / `csms.On` per message, each version ships a `handlers`
package with typed interfaces, embeddable `Unimplemented` defaults (returning a
`NotSupported` CallError), and one-call registrars. Embed the Unimplemented type
and override only the messages you handle:

```go
import v16h "github.com/shiv3/gocpp/v16/handlers"

type myCP struct{ v16h.UnimplementedCPHandler }

func (myCP) OnReset(ctx context.Context, req v16msg.ResetRequest) (v16msg.ResetResponse, error) {
    return v16msg.ResetResponse{Status: v16msg.ResetResponseStatusAccepted}, nil
}

// Registers every CSMS->CP handler the type implements in one call.
err := v16h.RegisterCP(client, myCP{})
// CSMS side mirror: v16h.RegisterCSMS(srv, myCSMSHandler{})
```

### One server for several protocol versions

`BootNotification` exists in 1.6, 2.0.1 and 2.1 with different payloads, so one
`csms.Server` that offers several subprotocols
(`csms.WithSubProtocols("ocpp2.0.1", "ocpp1.6")`) needs a handler per
(version, action). Each version's `RegisterCSMS` registers its handlers for that
version only, so both can be registered on the same server:

```go
_ = v16h.RegisterCSMS(srv, my16Handler{})
_ = v201h.RegisterCSMS(srv, my201Handler{})
```

For single messages use `csms.OnVersion(srv, ocppj.V201, profiles.BootNotification, h)`
(and `csms.OnSendVersion` for 2.1 SEND). Dispatch order for a connection of
version v: the `OnVersion` handler for (v, action), then the version-agnostic
`csms.On` handler, then `csms.OnAny`. `csms.On` keeps serving every version.
`Conn.Version()` tells a shared handler which version it is serving.

## Sending typed messages

The recommended way to send is the per-version `client` package, which wraps the
client/conn with one method per direction-appropriate message (plus an `…Async`
variant — see [Async](#async-callback-calls)). This is usually preferred over
calling `cp.Call` / `csms.Call` directly:

```go
import v16client "github.com/shiv3/gocpp/v16/client"

// Charge point -> CSMS. CP embeds *cp.Client.
cpc := v16client.NewCP(client)
resp, err := cpc.BootNotification(ctx, v16msg.BootNotificationRequest{
    ChargePointVendor: "Acme", ChargePointModel: "M1",
})

// CSMS -> charge point. CSMS embeds *csms.Conn.
conn, _ := srv.Get("CP_1")
_, err = v16client.NewCSMS(conn).Reset(ctx, v16msg.ResetRequest{Type: v16msg.ResetRequestTypeSoft})
```

Two lower-level alternatives exist if you don't want the wrapper:

- `cp.Call(ctx, client, v16p.BootNotification, req)` / `csms.Call(ctx, conn, v16p.Reset, req)`
  — the generic typed primitives.
- The `calls` package free functions, which take the client/conn as an argument:
  `v16c.CPBootNotification(ctx, client, req)` / `v16c.CSMSReset(ctx, conn, req)`
  (`import v16c "github.com/shiv3/gocpp/v16/calls"`).

## Options

CSMS (`csms.With*`): `WithSubProtocols`, `WithPath`, `WithCPIDExtractor`, `WithCallTimeout`,
`WithWriteTimeout`, `WithSchemaRegistry` + `WithStrictSchema` / `WithTolerantSchema` / `WithLenientSchema`
(+ `WithLenientTimeLocation`), `WithAuthenticator`, `WithMetrics`, `WithTracerProvider`, `WithConnectionRegistry`,
`WithDuplicatePolicy`, `WithTransactionStore` and `WithConfigStore` (application-facing storage slots, see below), `WithMessageRouter` (deprecated, see docs/clustering.md),
`WithGlobalConcurrencyLimit`, `WithWebSocketPingInterval` + `WithWebSocketPongWait`,
`WithSerializedCalls` / `WithConcurrentCalls`, `WithHandlerTimeout`, `WithMaxMessageSize`, `WithMaxBadFrames`,
`WithMaxConnections`, `WithOnConnect`, `WithOnDisconnect`, `WithOriginPatterns`,
`WithInsecureSkipVerifyOrigin`, `WithCheckOrigin`, `WithInstanceID` (see [clustering](clustering.md)), `WithAcceptRateLimit` and `Server.Drain` (see [draining](draining.md)).

CP (`cp.With*`): `WithSubProtocols`, `WithCallTimeout`, `WithLogger`,
`WithSchemaRegistry` + `WithStrictSchema` / `WithTolerantSchema` / `WithLenientSchema`
(+ `WithLenientTimeLocation`), `WithBasicAuth`,
`WithHTTPHeader`, `WithTLSConfig`, `WithHeartbeatInterval`,
`WithWebSocketPingInterval` + `WithWebSocketPongWait`, `WithSerializedCalls` /
`WithConcurrentCalls`, `WithHandlerTimeout`, `WithMaxMessageSize`, `WithMaxBadFrames`,
`WithOfflineQueue` + `WithRetryInFlightCalls`, `WithOnConnect`, `WithOnDisconnect`,
`WithOnReconnect`.

Defaults that are easy to get wrong (library defaults, both CSMS and CP unless noted):

| Option | Default |
|--------|---------|
| Schema mode (`WithStrictSchema` / `WithTolerantSchema` / `WithLenientSchema`) | **off** (no validation, even with a registry); lenient is opt-in |
| `WithWebSocketPingInterval` / `WithWebSocketPongWait` / `WithWebSocketReadTimeout` | 54s / 60s / 60s (0 disables) |
| `WithCallTimeout` | 30s |
| `WithMaxMessageSize` | 1 MiB (larger closes with 1009) |
| `WithMaxBadFrames` | off (bad frames are answered or dropped, never a reason to close) |
| `csms.WithMaxConnections` | 0 = unlimited |

`csms.WithTransactionStore` and `csms.WithConfigStore` are **application-facing extension
points**: the server keeps the stores (in-memory by default) so your handlers can share
them, but csms itself never reads or writes them. Persisting transactions or configuration
is up to your handlers.

> Note: `csms.WithHeartbeatInterval` was removed — in OCPP the Charge Point emits
> Heartbeat, never the CSMS. Use `cp.WithHeartbeatInterval` on the client.

### Routing, duplicates, metadata, auth

- `WithCPIDExtractor(func(*http.Request) (cpID string, ok bool))` — map arbitrary paths
  (e.g. `/ocpp/{org}/{cpId}`) to a charge point id. Default is the `WithPath` prefix +
  trailing segment.
- `WithDuplicatePolicy(csms.DuplicatePolicyCloseExisting | csms.DuplicatePolicyRejectNew)`
  — on a second connection for the same cpID, close the old one (default) or reject the new
  one with HTTP 409.
- Handlers read connection metadata from `*csms.Conn`: `RemoteAddr()`, `RequestHeader()`,
  `TLS()`, `Subprotocol()`.
- Authenticators receive the parsed cpID: `Authenticate(r *http.Request, cpID string)`.
  For `auth.BasicAuth(verify)`, `verify(cpID, password)` is called with the path-parsed cpID.
- `csms.CallRaw(ctx, conn, action, payloadJSON)` sends an untyped CSMS→CP operation
  (symmetric with `cp.CallRaw`); prefer the typed `csms.Call` for application code.

### Async (callback) calls

`csms.Call` / `cp.Call` are synchronous (block for the response); spawn a goroutine if you
need concurrency. For an ocpp-go `SendRequestAsync`-style callback API, use `CallAsync`:

```go
csms.CallAsync(ctx, conn, v16p.GetConfiguration, req, func(resp v16msg.GetConfigurationResponse, err error) {
    // delivered when the response (or error) arrives; do not block here
})

// Equivalent via the client package: every send method has an …Async variant.
v16client.NewCSMS(conn).GetConfigurationAsync(ctx, req, func(resp v16msg.GetConfigurationResponse, err error) {
    // ...
})
```

- By default, calls are queued on a per-connection FIFO (shared with synchronous `Call`)
  and sent one outstanding at a time; callbacks fire in submission order. Bound the queue
  with `WithAsyncQueueSize(n)` (default 64) — waiting beyond it returns `ocppj.ErrQueueFull`.
  A call whose ctx is cancelled while queued is dropped without being sent.
- With `WithConcurrentCalls()` (non-compliant, see below) each `CallAsync` runs
  concurrently and the callback fires as responses arrive.
- `CallAsync` returns an error synchronously only if the call can't be accepted (not
  connected, nil callback, or queue full); per-call failures go to the callback.

### Handler context and limits

- **Handler context.** An inbound handler's `ctx` keeps the connection's values (charge
  point id, message id via `ocppj.MessageIDFromContext`, tracing) but is *not* cancelled
  when the connection drops, so work in flight (e.g. persisting a StopTransaction) can
  finish; the CALLRESULT of a handler that finishes after the disconnect is dropped
  silently. Instead each handler is bounded by `WithHandlerTimeout(d)` (default 60s,
  0 = none): the ctx carries that deadline and a handler that returns the deadline error
  is answered with an `InternalError` CALLERROR. The deadline is cooperative — honor `ctx`.
  A handler that wants to stop on disconnect selects on `csms.ConnFromContext(ctx)` →
  `conn.Done()` (CSMS) or `cp.ConnDone(ctx)` (charge point).
- `WithMaxMessageSize(n)` (CSMS and CP) sets the largest inbound WebSocket message, default
  1 MiB; a larger message closes the connection with 1009.
- `WithMaxBadFrames(n, window)` (CSMS and CP, opt-in) closes a connection with 1008 once the
  peer sent `n` bad frames within `window` (0 = whole connection life): not valid OCPP-J, an
  unsupported MessageTypeId, a CALL with a bad message id, or a CALL rejected by schema
  validation or whose payload cannot be decoded. An unknown action does not count. Every bad
  frame is exported as `gocpp_bad_frames_total{version,reason}` whether or not the limit is set.
  A useful internet-facing value is `WithMaxBadFrames(20, time.Minute)`.
- `csms.WithMaxConnections(n)` caps simultaneous charge points (0 = unlimited). Over the
  cap the upgrade is refused with HTTP 503 + `Retry-After` before the handshake. A charge
  point reconnecting under an id that is already connected replaces its old connection and
  is not refused.
- `Conn.Stats()` returns `ConnectedAt`, `LastReceivedAt` (any inbound frame, ping and pong
  included), `LastSentAt` (last write), `PendingCalls` (awaiting a response) and
  `QueuedCalls` (waiting for the call slot); `Conn.LastActivity()` is `LastReceivedAt`.
  `srv.Connections()` lists the connected ids; `srv.Get(id)` returns the `*Conn`.

### Client auth & TLS

- `cp.WithBasicAuth(user, pass)` sends HTTP Basic credentials on the WebSocket upgrade
  (OCPP Security Profile 1/2); pair with `csms.WithAuthenticator(auth.BasicAuth(...))`.
- `cp.WithHTTPHeader(key, value)` appends arbitrary upgrade headers (call repeatedly).
- `cp.WithTLSConfig(*tls.Config)` controls the `wss://` dial: custom roots, client
  certificates for mutual TLS (Security Profile 2/3), etc. (CSMS-side TLS is configured on
  the `http.Server` that serves `srv.Handler()`.)

### Keepalive, Heartbeat, serialization, offline queue

- Transport keepalive is **on by default** on both sides: ping every 54s
  (`WithWebSocketPingInterval(d)`), pong wait 60s (`WithWebSocketPongWait(d)`), and a read
  idle timeout of 60s (`WithWebSocketReadTimeout(d)`: close if no frame, ping or pong
  arrives). A missed pong tears the connection down so dead peers are detected. A value of
  0 disables the corresponding check.
- `cp.WithHeartbeatInterval(d)` makes the charge point auto-send OCPP `Heartbeat` every `d`.
- Outbound CALLs are serialized per connection by default: OCPP 1.6J §4.1.1 and 2.0.1
  Part 4 forbid sending a new CALL while one is outstanding. Calls wait in one bounded
  FIFO per connection (sync `Call` and `CallAsync` share it; replies to inbound requests
  and 2.1 SEND bypass it; connections never block each other). `CallTimeout` starts once the
  call owns the slot, immediately before the CALL is written (not when `Call` is invoked),
  so an unresponsive charger frees the slot after one timeout; the wait for the slot is
  bounded by the caller's ctx. `WithSerializedCalls()` is kept as an
  explicit no-op; `WithConcurrentCalls()` opts out (non-compliant, many chargers drop
  or mis-handle concurrent requests).
- `cp.WithOfflineQueue(capacity)` queues CP-originated calls while disconnected and flushes
  them FIFO on reconnect; a full queue returns `ocppj.ErrQueueFull`. Disabled (fail-fast with
  `ocppj.ErrNotConnected`) when capacity ≤ 0. By default a CALL that was already in flight
  when the link dropped fails with `ocppj.ErrConnClosed` (OCPP has no idempotency guarantee);
  `cp.WithRetryInFlightCalls()` opts into re-sending it after reconnect.
- Lifecycle callbacks: `cp.WithOnConnect/WithOnDisconnect/WithOnReconnect` and
  `csms.WithOnConnect(func(*Conn))` / `csms.WithOnDisconnect(func(*Conn, error))`.
- Handlers registered with `cp.On` / `handlers.RegisterCP` live on the client and persist
  across `client.Run(ctx)` reconnects — no need to re-register after a drop.
- `csms.WithOriginPatterns` / `WithInsecureSkipVerifyOrigin` / `WithCheckOrigin` control
  WebSocket origin verification (default: same-origin; no-Origin requests, e.g. charge
  points, are always allowed).
- `srv.Shutdown(ctx)` gracefully drains live connections; `srv.Close()` tears down now.

## Validation

Two layers (spec §4.4):

1. **JSON Schema** (source of truth) — embedded official schemas, validated on the wire
   when a registry is set. Mode is three-state: `WithStrictSchema(true)` rejects invalid
   messages, `WithTolerantSchema()` logs a warning and passes (vendor quirks), and
   `WithStrictSchema(false)` turns validation off. **Off is the library default**: with no
   schema option, nothing is validated even when a registry is set (lenient is opt-in). `WithLenientSchema()` is
   described below. **Recommendation:** use `WithLenientSchema()` for a production CSMS facing real
   chargers (it protects handlers from malformed payloads and repairs common firmware mistakes at about
   +0.35 core per 30k msg/s, see [performance.md](performance.md#schema-validation-cost)); use
   `WithTolerantSchema()` first if you want to see what your fleet sends without rejecting anything; leave
   it off only for trusted fleets or behind a validating proxy.
   **Lenient mode** (`csms.WithLenientSchema()` / `cp.WithLenientSchema()`) rejects
   structurally broken messages (missing required fields, wrong shapes) but repairs
   benign drift in inbound requests *and* responses before the typed decode, guided by
   the schema. Each repair is logged and counted as a soft violation:
   - enum values that differ only by case are rewritten to the canonical value
     (keyword `enum`);
   - `date-time` strings are rewritten to RFC3339 (keyword `format`): space instead of
     `T`, lowercase `t`/`z`, `,` as the fraction separator, offsets such as `+0900` or
     `+09`, and values with no zone at all. Zone-less values are read as UTC unless
     `csms.WithLenientTimeLocation(loc)` / `cp.WithLenientTimeLocation(loc)` is set;
   - primitives of the wrong JSON type are converted when lossless (keyword `type`):
     `"1"` for an integer, `"32.5"` for a number, `"true"`/`"false"` for a boolean, and
     a number where a string is expected (its JSON text). Values such as `"1.5"` for an
     integer, `"abc"` or `"1e999"` are left as sent and rejected.

   Further repairs are lossy or vendor specific, so each is **off by default** and
   switched on separately on the server (`csms` options, lenient mode only, ignored
   under `WithOCPPGoValidation()`):
   - `WithLenientEmptyNumbers()`: `""` where an integer or number is expected is read
     as `0` (keyword `type`);
   - `WithLenientMissingChargingRateUnit()` / `WithLenientDefaultMissing(prop, value)`:
     a required string property that is absent (for example `chargingRateUnit`) is
     filled with a default, `"W"` for the charging rate unit (keyword `required`);
   - `WithLenientVendorMeasurands()` / `WithLenientUnknownEnumValues(props...)`: a value
     outside the enum of the named properties (vendor measurands such as
     `Energy.Request.Target`) is passed unchanged (keyword `enum`).

   Strict, tolerant and off modes never rewrite payloads, and outbound encoding always
   stays RFC3339.
2. **Struct tags** — generated `validate:"required,max=…,oneof=…"` tags for app-side
   checks of structs you build yourself.

Validate a message manually:

```go
reg := schema.NewRegistry()
v201.RegisterSchemas(reg)
v, _ := reg.Lookup("2.0.1", "BootNotification", "request")
err := v.Validate(rawJSON)
```
