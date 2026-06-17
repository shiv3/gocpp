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
    _, err := csms.Call(ctx, conn, v201p.Reset, v201msg.ResetRequest{Type: "OnIdle"})
    _ = err
}
```

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

resp, err := cp.Call(ctx, client, v201p.BootNotification, req)
```

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

For sending, each version also ships a `calls` package with typed, direction-safe
helpers over `cp.Call` / `csms.Call`:

```go
import v16c "github.com/shiv3/gocpp/v16/calls"

// Charge point -> CSMS
resp, err := v16c.CPBootNotification(ctx, client, v16msg.BootNotificationRequest{
    ChargePointVendor: "Acme", ChargePointModel: "M1",
})

// CSMS -> charge point
conn, _ := srv.Get("CP_1")
_, err = v16c.CSMSReset(ctx, conn, v16msg.ResetRequest{Type: v16msg.ResetRequestTypeSoft})
```

## Options

CSMS (`csms.With*`): `WithSubProtocols`, `WithPath`, `WithCPIDExtractor`, `WithCallTimeout`,
`WithWriteTimeout`, `WithSchemaRegistry` + `WithStrictSchema` / `WithTolerantSchema`,
`WithAuthenticator`, `WithMetrics`, `WithTracerProvider`, `WithConnectionRegistry`,
`WithDuplicatePolicy`, `WithTransactionStore`, `WithConfigStore`, `WithMessageRouter`,
`WithGlobalConcurrencyLimit`, `WithWebSocketPingInterval` + `WithWebSocketPongWait`,
`WithSerializedCalls`, `WithOnConnect`, `WithOnDisconnect`, `WithOriginPatterns`,
`WithInsecureSkipVerifyOrigin`, `WithCheckOrigin`.

CP (`cp.With*`): `WithSubProtocols`, `WithCallTimeout`, `WithLogger`,
`WithSchemaRegistry` + `WithStrictSchema` / `WithTolerantSchema`, `WithBasicAuth`,
`WithHTTPHeader`, `WithTLSConfig`, `WithHeartbeatInterval`,
`WithWebSocketPingInterval` + `WithWebSocketPongWait`, `WithSerializedCalls`,
`WithOfflineQueue` + `WithRetryInFlightCalls`, `WithOnConnect`, `WithOnDisconnect`,
`WithOnReconnect`.

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
```

- Without `WithSerializedCalls`, each `CallAsync` runs concurrently and the callback fires
  as responses arrive.
- With `WithSerializedCalls`, calls are queued on a per-connection FIFO and sent one
  outstanding at a time; callbacks fire in submission order. Bound the queue with
  `WithAsyncQueueSize(n)` (default 64) — enqueuing beyond it returns `ocppj.ErrQueueFull`.
- `CallAsync` returns an error synchronously only if the call can't be accepted (not
  connected, nil callback, or queue full); per-call failures go to the callback.

### Client auth & TLS

- `cp.WithBasicAuth(user, pass)` sends HTTP Basic credentials on the WebSocket upgrade
  (OCPP Security Profile 1/2); pair with `csms.WithAuthenticator(auth.BasicAuth(...))`.
- `cp.WithHTTPHeader(key, value)` appends arbitrary upgrade headers (call repeatedly).
- `cp.WithTLSConfig(*tls.Config)` controls the `wss://` dial: custom roots, client
  certificates for mutual TLS (Security Profile 2/3), etc. (CSMS-side TLS is configured on
  the `http.Server` that serves `srv.Handler()`.)

### Keepalive, Heartbeat, serialization, offline queue

- `WithWebSocketPingInterval(d)` (+ `WithWebSocketPongWait(d)`, default 60s) enables
  transport-level ping/pong on either side; a missed pong tears the connection down so dead
  peers are detected. Disabled when interval is 0.
- `cp.WithHeartbeatInterval(d)` makes the charge point auto-send OCPP `Heartbeat` every `d`.
- `WithSerializedCalls()` enforces at most one outstanding outbound CALL per connection
  (some charge points cannot handle concurrent requests). Off by default.
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
   `WithStrictSchema(false)` (default) turns validation off.
2. **Struct tags** — generated `validate:"required,max=…,oneof=…"` tags for app-side
   checks of structs you build yourself.

Validate a message manually:

```go
reg := schema.NewRegistry()
v201.RegisterSchemas(reg)
v, _ := reg.Lookup("2.0.1", "BootNotification", "request")
err := v.Validate(rawJSON)
```
