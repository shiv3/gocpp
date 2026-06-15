# Migrating from lorenzodonini/ocpp-go

gocpp is a deliberate rewrite, not a drop-in replacement: there is no automated
converter and no compatibility shim. The payoff is typed, generics-first handlers and
a single module that also speaks OCPP 2.1.

## API mapping

| lorenzodonini/ocpp-go | gocpp |
|---|---|
| `ocpp16.NewCentralSystem(nil, nil)` | `csms.NewServer(csms.WithSubProtocols("ocpp1.6"))` |
| Implement the `core.CentralSystemHandler` interface (all methods) | `csms.On(srv, v16p.<Msg>, handler)` per message you care about |
| `cs.SetNewChargePointHandler(...)` / `SetChargePointDisconnectedHandler(...)` | connection accept is automatic; look up live conns with `srv.Get(id)` |
| `req.(*core.BootNotificationRequest)` type assertion in a generic handler | typed `req v16msg.BootNotificationRequest` parameter — no assertion |
| `cs.SendRequestAsync(cpID, req, cb)` | `csms.Call(ctx, conn, v16p.<Msg>, req)` — synchronous, typed |
| `ocpp16.NewChargePoint(id, nil, nil)` | `cp.NewClient(id, url, cp.WithSubProtocols("ocpp1.6"))` |
| `cp.Start(csmsURL)` | `client.Connect(ctx)` (single) or `client.Run(ctx)` (auto-reconnect) |
| logrus logger | `slog.Logger` via `WithLogger` |
| Validation via go-playground struct tags | JSON-Schema validation (`WithSchemaRegistry` + `WithStrictSchema`) + generated tags |

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

## Notable behavioral differences

- **Direction is enforced at registration**: a CSMS can only `On` charge-point-originated
  messages and only `Call` CSMS-originated ones (the CP is the mirror); the wrong
  direction returns `ocppj.ErrInvalidDirection`.
- **Errors**: use `errors.As` for `*ocppj.CallError` and `errors.Is` for sentinels like
  `ocppj.ErrCallTimeout`, `ocppj.ErrNotConnected`.
- **One module, three versions**: import `v16`, `v201`, or `v21` message/profile packages
  as needed; the dispatcher and csms/cp packages are version-agnostic.
