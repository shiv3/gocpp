# 0001. One resident goroutine per connection

Status: Accepted
Date: 2026-11-25

## Context

A CSMS holds tens of thousands of mostly idle WebSocket connections. Goroutines
per connection set the memory floor, and every extra goroutine is another place
for teardown races (send on a closed channel, a writer blocked on a dead socket).
The upstream library this project grew out of keeps several per connection; see
[ocpp-go-structural-issues.md](../ocpp-go-structural-issues.md), section 12.

## Decision

`dispatcher.Conn` has exactly one resident goroutine, the reader. It reads
frames, resolves CALLRESULT / CALLERROR frames directly in the pending-call
store, and for each inbound CALL starts a short-lived goroutine or hands it to a
`HandlerPool` worker, up to `MaxConcurrentHandlers`. When all slots are busy the
frame waits in a small bounded queue; only a full queue stops the reader.

There is no writer goroutine: a frame is written by the goroutine that produced it
(a handler or a `Call` caller) under a per-connection FIFO lock. The write timeout
and the read-idle watchdog are `time.AfterFunc` timers. Teardown is one
`context.AfterFunc` on a context with a cancel cause, so it runs once whichever
side ends the connection.

## Consequences

- An idle connection costs one goroutine; `benchmarks` asserts footprint bounds.
- No channel is closed under a sender; senders select on the connection context.
- Frame order on the wire equals lock order, so the frame observer sees wire order
  ([ordering.md](../ordering.md)).
- A handler that blocks holds its slot, not the connection: other frames of the
  same charge point keep being read until the queue is full. Handlers of one
  connection run concurrently, so a handler that needs an earlier request's effect
  must synchronise itself.
- Back-pressure is per connection; `WithGlobalConcurrencyLimit` adds a process cap.

## Alternatives considered

- Reader plus writer goroutine per connection with a send channel: simpler to
  reason about ordering, but doubles the idle footprint and needs the
  close-vs-send discipline that the lock-and-context design avoids.
- A fixed worker pool for all connections: bounds goroutines globally but lets one
  slow charge point starve others; available as an opt-in `HandlerPool`.
