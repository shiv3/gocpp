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

## Options

CSMS (`csms.With*`): `WithSubProtocols`, `WithPath`, `WithCPIDExtractor`, `WithCallTimeout`,
`WithSchemaRegistry` + `WithStrictSchema` / `WithTolerantSchema`, `WithAuthenticator`,
`WithMetrics`, `WithTracerProvider`, `WithConnectionRegistry`, `WithDuplicatePolicy`,
`WithTransactionStore`, `WithConfigStore`, `WithMessageRouter`.

CP (`cp.With*`): `WithSubProtocols`, `WithCallTimeout`, `WithLogger`,
`WithSchemaRegistry` + `WithStrictSchema` / `WithTolerantSchema`.

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
