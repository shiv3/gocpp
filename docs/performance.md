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
- a **closer** that waits for teardown and closes the socket with the right
  close frame.

The read idle watchdog and the WebSocket ping are `time.AfterFunc` timers, not
goroutines: an inbound frame only stores a timestamp, the watchdog timer
re-arms itself for the remaining time when it fires early, and a goroutine
exists for the ping only while it waits for the pong. Enabling
`WithWebSocketPingInterval` therefore adds no resident goroutine; on the CP side
`WithHeartbeatInterval` adds one (the keepalive ticker). Each inbound Call is
handled in its own short-lived goroutine (`go c.runHandler(frame)`), bounded as
described next.

So the baseline cost is **5 goroutines per connection plus the net/http serve
goroutine, 6 in total**, regardless of traffic (see "Measuring footprint").

For `N` connections the fixed baseline is `~6·N` goroutines. Goroutines start
with a ~2 KB stack, so tens of thousands of mostly-idle connections cost on the
order of tens to low-hundreds of MB — well within range for a single process.
Typical CSMS traffic is bursty-but-light (Heartbeat / StatusNotification), so in
practice the baseline dominates and handler goroutines are few.

## Measuring footprint

Benchmarks live in `benchmarks/footprint_test.go`:

```sh
# Per-connection cost of 2,000 idle connections through the real csms server
# (idle, and after one Heartbeat each). One iteration is enough.
go test -run '^$' -bench IdleFootprint -benchtime=1x ./benchmarks/

# Per-message cost of an inbound Heartbeat CALL -> CALLRESULT round trip,
# on one connection and spread across 256 connections.
go test -run '^$' -bench HeartbeatMessage -benchmem ./benchmarks/
```

`BenchmarkIdleFootprint` reports `idle-` and `hb-` prefixed `heap-B/conn`
(`HeapAlloc` after a forced GC), `stack-B/conn` (`StackInuse`) and
`goroutines/conn`. The client side is a raw TCP socket with a hand-written
handshake, so it adds no goroutines and almost no heap: the numbers are the
server's. RSS is typically 1.5-2x heap+stack because of GC headroom and
runtime overhead. The per-message benchmarks use a `coder/websocket` client in
the same process, so their allocations include the client side.

`TestIdleFootprintBounds` fails if the per-connection heap, stack or goroutine
count exceeds fixed upper bounds. The bounds are ratcheted down as the footprint
shrinks. The race detector inflates heap and stack several-fold, so the test
only logs under `-race`; CI runs it a second time without `-race`. Run it with
`-short` for a 500-connection variant.

## Concurrency bounds

Inbound handler concurrency is bounded at two levels. Both must be acquired
before a handler goroutine is spawned; when a bound is saturated the `dispatch`
loop blocks, which back-pressures through the inbound channel
(`InboundQueueSize`, default 4) and ultimately slows reads on that socket (including
CALLRESULTs, so raise it if handlers block for long while awaiting a reply). No
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
| `InboundQueueSize` (`dispatcher.Config`, `WithInboundQueueSize`) | 4 | Reader-to-dispatcher channel depth; only fills while all handler slots are busy, then back-pressure reaches the socket. |
| `OutboundQueueSize` (`dispatcher.Config`, `WithOutboundQueueSize`) | 4 | Channel feeding the writer; each send waits for its own write, so a large buffer buys nothing. |
| `WithCallTimeout` | 30s | Deadline for an outbound CSMS→CP call awaiting its result. |
| `WithWriteTimeout` | 10s | Deadline for a single socket write. |
| `WithConcurrentCalls()` | serialized | Outbound CALLs are limited to one outstanding per connection by default (OCPP-mandated); later calls wait in a bounded per-connection FIFO (`WithAsyncQueueSize`, 64). This option lifts the limit (non-compliant). Measured overhead: see `core/dispatcher/call_bench_test.go`; in-process, serialized and concurrent are within noise (about 7.1 µs and 55 allocs per call on one connection; about 0.65 M calls/s at 1,000 connections and 0.58 M calls/s at 10,000, 56 allocs per call, in both modes). |
| `WithHandlerTimeout(d)` | 60s | Per-handler deadline (0 = none). |
| `WithMaxMessageSize(n)` | 1 MiB | Largest inbound WebSocket message. |
| `csms.WithMaxConnections(n)` | unlimited | Connection cap; over it, upgrades get HTTP 503 + Retry-After. |
| `WithWebSocketPingInterval(d)` | disabled | Transport keepalive; adds one goroutine per connection and detects dead peers via pong timeout (`WithWebSocketPongWait`, default 60s). |

## Rules of thumb

- Mostly-idle fleets (Heartbeat-dominated): the `~4·N` baseline is the main cost;
  default handler limits are fine. Watch process RSS and FD count as `N` grows.
- High-throughput or heavy handlers (DB writes, downstream calls): set
  `WithGlobalConcurrencyLimit` to protect shared backends from `N`-proportional
  fan-out, and size it to the backend's parallel capacity.
- Always run load tests with `-race` off but pprof on; goroutine and heap
  profiles will show whether the baseline or handler fan-out dominates for your
  workload.

## Per-message baselines

Allocation per message is what drives GC-mark share and therefore tail latency at
high request rates, so the per-message paths have their own benchmarks (no sockets,
no client in the numbers):

```sh
# inbound CALL -> typed handler -> CALLRESULT on the in-memory transport
go test -run '^$' -bench DispatchRoundTrip -benchmem ./benchmarks/
# schema validation alone, lenient and strict
go test -run '^$' -bench 'Validate(Lenient|Strict)' -benchmem ./core/schema/
```

Baseline before the per-message allocation work (Go 1.26, i5-13600K, one run;
`schema=off` has no validation, `lenient` and `strict` validate both the request and
the response):

| benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| DispatchRoundTrip/Heartbeat/schema=off | 14,138 | 1,922 | 42 |
| DispatchRoundTrip/Heartbeat/schema=lenient | 18,468 | 4,869 | 87 |
| DispatchRoundTrip/Heartbeat/schema=strict | 17,566 | 4,402 | 78 |
| DispatchRoundTrip/StatusNotification/schema=off | 15,293 | 2,040 | 42 |
| DispatchRoundTrip/StatusNotification/schema=lenient | 25,414 | 6,229 | 122 |
| DispatchRoundTrip/StatusNotification/schema=strict | 24,292 | 5,645 | 110 |
| ValidateLenient/HeartbeatReq | 747 | 1,145 | 18 |
| ValidateLenient/HeartbeatConf | 1,568 | 1,783 | 27 |
| ValidateLenient/StatusNotificationReq | 3,492 | 3,015 | 62 |
| ValidateLenient/MeterValuesReq | 10,614 | 9,407 | 179 |
| ValidateStrict/HeartbeatReq | 936 | 929 | 14 |
| ValidateStrict/HeartbeatConf | 1,296 | 1,533 | 22 |
| ValidateStrict/StatusNotificationReq | 3,038 | 2,653 | 54 |
| ValidateStrict/MeterValuesReq | 9,186 | 8,507 | 156 |
