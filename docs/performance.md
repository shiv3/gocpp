# gocpp performance & scaling

This page describes the runtime cost model of a gocpp CSMS, the concurrency
bounds you can tune, and what to watch as connection counts grow.

## Goroutine model

gocpp uses the standard Go *goroutine-per-connection* model. For each accepted
charge-point connection, `dispatcher.Conn.Start` launches a fixed set of
long-lived goroutines (`core/dispatcher/conn.go`):

- **reader** — reads frames off the socket, routes CallResult/CallError to the
  pending store, queues inbound Calls for dispatch.
- **writer** — sole owner of the socket write side; serializes outbound frames.
- **dispatch** — pulls inbound Calls and spawns handler goroutines under the
  concurrency bounds below.
- a small **teardown watcher** that waits on the worker `WaitGroup` and closes
  `done`.

So the baseline cost is roughly **4 goroutines per connection**, regardless of
traffic. On top of that, each inbound Call is handled in its own short-lived
goroutine (`go c.runHandler(frame)`), bounded as described next.

For `N` connections the fixed baseline is `~4·N` goroutines. Goroutines start
with a ~2 KB stack, so tens of thousands of mostly-idle connections cost on the
order of tens to low-hundreds of MB — well within range for a single process.
Typical CSMS traffic is bursty-but-light (Heartbeat / StatusNotification), so in
practice the baseline dominates and handler goroutines are few.

## Concurrency bounds

Inbound handler concurrency is bounded at two levels. Both must be acquired
before a handler goroutine is spawned; when a bound is saturated the `dispatch`
loop blocks, which back-pressures through the inbound channel
(`OutboundQueueSize`, default 64) and ultimately slows reads on that socket. No
inbound Call is dropped — it waits for a slot.

### Per-connection: `MaxConcurrentHandlers`

Each connection has its own `semaphore.Weighted` sized by
`dispatcher.Config.MaxConcurrentHandlers` (default **16**). This caps how many
handlers a *single* charge point can run at once, so one chatty peer cannot
monopolize the connection's processing.

With per-connection limits only, the worst-case total handler concurrency is
`N · MaxConcurrentHandlers` — it grows linearly with the number of connections
and has no server-wide ceiling.

### Server-wide: `WithGlobalConcurrencyLimit`

To bound total handler concurrency across *all* connections, set a server-wide
cap:

```go
srv := csms.NewServer(
    csms.WithSubProtocols("ocpp1.6"),
    csms.WithGlobalConcurrencyLimit(512), // at most 512 handlers in flight, process-wide
)
```

This installs a single `semaphore.Weighted` shared by every connection
(`csms/server.go`). A handler goroutine is spawned only after acquiring **both**
the per-connection slot and the global slot, so total concurrent handlers is
bounded by `min(N · MaxConcurrentHandlers, GlobalConcurrencyLimit)`.

- The cap is **opt-in**: a value `<= 0` (the default) disables it, leaving only
  per-connection limits — behavior is unchanged from before the option existed.
- When the global cap is reached, further inbound calls **wait** (back-pressure)
  rather than being rejected. Charge points see slower responses, not errors.
- Pick a value tied to how many handlers your backend (DB, downstream services)
  can absorb in parallel, independent of how many charge points are connected.

## What is *not* bounded

- **Baseline goroutines** (`~4·N`) and **socket/file descriptors** scale with the
  number of connections, not with `WithGlobalConcurrencyLimit`. To bound those,
  limit the number of accepted connections at your load balancer or in front of
  `Server.Handler()` (e.g. reject/503 above a threshold). gocpp does not cap the
  connection count itself.
- **Outbound** CSMS→CP calls are governed by `CallTimeout` (default 30s) and
  `WriteTimeout` (default 10s), not by the handler semaphores.

## Tuning summary

| Knob | Default | Effect |
|------|---------|--------|
| `WithGlobalConcurrencyLimit(n)` | disabled | Server-wide cap on concurrent inbound handlers; back-pressures when full. |
| `MaxConcurrentHandlers` (`dispatcher.Config`) | 16 | Per-connection cap on concurrent inbound handlers. |
| `OutboundQueueSize` (`dispatcher.Config`) | 64 | Inbound/outbound channel depth; absorbs bursts before back-pressure reaches the socket. |
| `WithCallTimeout` | 30s | Deadline for an outbound CSMS→CP call awaiting its result. |
| `WithWriteTimeout` | 10s | Deadline for a single socket write. |

## Rules of thumb

- Mostly-idle fleets (Heartbeat-dominated): the `~4·N` baseline is the main cost;
  default handler limits are fine. Watch process RSS and FD count as `N` grows.
- High-throughput or heavy handlers (DB writes, downstream calls): set
  `WithGlobalConcurrencyLimit` to protect shared backends from `N`-proportional
  fan-out, and size it to the backend's parallel capacity.
- Always run load tests with `-race` off but pprof on; goroutine and heap
  profiles will show whether the baseline or handler fan-out dominates for your
  workload.
