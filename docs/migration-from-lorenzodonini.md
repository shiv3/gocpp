# Migrating from lorenzodonini/ocpp-go

gocpp is a deliberate rewrite, not a drop-in replacement: there is no automated
converter and no compatibility shim. The payoff is typed, generics-first handlers and
a single module that also speaks OCPP 2.1.

## API mapping

| lorenzodonini/ocpp-go | gocpp |
|---|---|
| `ocpp16.NewCentralSystem(nil, nil)` | `csms.NewServer(csms.WithSubProtocols("ocpp1.6"))` |
| Implement the `core.CentralSystemHandler` interface (all methods) | `csms.On(srv, v16p.<Msg>, handler)` per message, or implement `v16/handlers.CSMSHandler` (embed `UnimplementedCSMSHandler`) and call `handlers.RegisterCSMS(srv, h)` once |
| Implement the `core.ChargePointHandler` interface (CP side) | `cp.On(client, v16p.<Msg>, handler)` per message, or `v16/handlers.CPHandler` + `handlers.RegisterCP(client, h)` |
| `cs.SetNewChargePointHandler(...)` / `SetChargePointDisconnectedHandler(...)` | `csms.WithOnConnect` / `csms.WithOnDisconnect`; look up live conns with `srv.Get(id)` |
| `req.(*core.BootNotificationRequest)` type assertion in a generic handler | typed `req v16msg.BootNotificationRequest` parameter — no assertion |
| `cs.SendRequestAsync(cpID, req, cb)` | `csms.CallAsync(ctx, conn, v16p.<Msg>, req, cb)` (callback, like ocpp-go) or `csms.Call(...)` (synchronous); typed wrappers `v16/calls.CSMS<Msg>(ctx, conn, req)` |
| `cp.SendRequestAsync(...)` | `cp.CallAsync(...)` / `cp.Call(...)`; typed wrappers `v16/calls.CP<Msg>(ctx, client, req)` |
| `ocpp16.NewChargePoint(id, nil, nil)` | `cp.NewClient(id, url, cp.WithSubProtocols("ocpp1.6"))` |
| `cp.Start(csmsURL)` | `client.Connect(ctx)` (single) or `client.Run(ctx)` (auto-reconnect; handlers persist across reconnects) |
| disable WebSocket origin check | `csms.WithInsecureSkipVerifyOrigin()` (or `WithOriginPatterns` / `WithCheckOrigin`) |
| logrus logger | `slog.Logger` (stdlib `log/slog`) via `WithLogger` |
| Validation via go-playground struct tags | JSON-Schema validation (`WithSchemaRegistry` + `WithStrictSchema` / `WithLenientSchema` / `WithTolerantSchema`) + generated tags |

## Before / after

A CSMS handling BootNotification + Heartbeat.

**Before (lorenzodonini):** implement `core.CentralSystemHandler`, type-assert each
request, return a confirmation, register the handler, and `Start`.

**After (gocpp):** see [`examples/migration-after`](../examples/migration-after) — typed
per-message handlers, no interface to satisfy, no type assertions:

```go
srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
    return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
})
csms.On(srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
    return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
})
srv.ListenAndServe(":8080")
```

## Less boilerplate

Instead of one `On` / `Call` per message, use the generated `handlers` and `calls`
packages (see [usage](usage.md#bulk-handler-registration)):

```go
import (
    v16h "github.com/shiv3/gocpp/v16/handlers"
    v16c "github.com/shiv3/gocpp/v16/calls"
)

type myCP struct{ v16h.UnimplementedCPHandler } // override only what you handle
func (myCP) OnReset(ctx context.Context, r v16msg.ResetRequest) (v16msg.ResetResponse, error) { /* ... */ }

v16h.RegisterCP(client, myCP{})                  // register all CSMS->CP handlers in one call
resp, _ := v16c.CPBootNotification(ctx, client, req) // typed, direction-safe send
```

## Notable behavioral differences

- **Direction is enforced at registration**: a CSMS can only `On` charge-point-originated
  messages and only `Call` CSMS-originated ones (the CP is the mirror); the wrong
  direction returns `ocppj.ErrInvalidDirection`. The exception is `DataTransfer`, which
  OCPP allows in both directions — it is marked `SentByBoth`, so either peer may `On` and
  `Call` it (no custom descriptor needed).
- **Errors**: use `errors.As` for `*ocppj.CallError` and `errors.Is` for sentinels like
  `ocppj.ErrCallTimeout`, `ocppj.ErrNotConnected`.
- **One module, three versions**: import `v16`, `v201`, or `v21` message/profile packages
  as needed; the dispatcher and csms/cp packages are version-agnostic.
- **Graceful shutdown**: `srv.Shutdown(ctx)` drains live connections; `srv.Close()` tears
  down immediately.
