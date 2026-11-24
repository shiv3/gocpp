# Ordering guarantees on one connection

What a handler or caller can rely on for the frames of a single charge point
connection. Nothing here orders frames of different connections.

## Writes

- Every frame a connection sends (CALLRESULT, CALLERROR, CALL, SEND) is written
  under one per-connection write lock, FIFO. Frames therefore reach the socket in
  lock order, and the frame observer (`csms.WithFrameObserver`) runs while the lock
  is still held, so **observer order is wire order**.
- Outbound CALLs are serialized by default (one outstanding CALL per connection,
  OCPP-J §4.1; `csms.WithConcurrentCalls` turns it off). Queued calls are sent FIFO
  and `CallAsync` callbacks fire in submission order.

## Inbound requests

- CALLs and SENDs from the charge point **start** in arrival order, but handlers run
  concurrently (up to `MaxConcurrentHandlers`, 16 by default; further frames wait in
  a bounded queue). Two handlers can therefore finish, and their CALLRESULTs be
  written, in a different order than the requests arrived. A handler that must see
  the effects of an earlier request has to synchronize on its own state.
- A CALLRESULT/CALLERROR for a CALL the CSMS sent is resolved by the connection's
  reader as soon as it is read; it is not queued behind inbound handlers. A
  handler of a later request can therefore run before or after the caller of an
  earlier `Call` has been resumed. Do not assume one implies the other.

## Doing something after the reply

A request handler cannot send its own reply early: the CALLRESULT is written after
the handler returns. A `Call` made inside the handler is written **before** that
reply, which charge points reject or mishandle (for example GetConfiguration
before the BootNotification response). Use `csms.AfterReply`:

```go
csms.On(srv, profiles.BootNotification, func(ctx context.Context, c *csms.Conn, req messages.BootNotificationRequest) (messages.BootNotificationResponse, error) {
    csms.AfterReply(ctx, func(ctx context.Context) {
        _, _ = csms.Call(ctx, c, profiles.GetConfiguration, messages.GetConfigurationRequest{})
    })
    return messages.BootNotificationResponse{Status: "Accepted", Interval: 300, CurrentTime: time.Now()}, nil
})
```

Guarantees:

- The callback starts only after the CALLRESULT write has returned and the frame
  observer has seen it, so the CALLRESULT precedes anything the callback sends.
- Callbacks of one reply run in registration order on one goroutine, outside the
  handler slot (they may block on a `Call`; they do not hold up other inbound
  CALLs). The ctx is the connection context and is cancelled when the connection
  ends.
- Nothing runs if the handler returns an error (a CALLERROR is sent), if the write
  fails, or if the connection closes first. Panics are recovered and logged.
- It orders work after *this* reply only. A different inbound CALL may be handled,
  and answered, before or while the callback runs.
- `AfterReply` returns false, and the callback is dropped, outside a handler
  context and for handlers served through a gateway link.
