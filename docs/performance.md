# gocpp performance & scaling

This page describes the runtime cost model of a gocpp CSMS, the concurrency
bounds you can tune, and what to watch as connection counts grow.

## Goroutine model

A server-side connection costs **one resident goroutine** (the reader,
`core/dispatcher/conn.go`) and about 13 KB of RSS when idle. There is no writer,
dispatcher, closer or watchdog goroutine:

- the **reader** reads frames off the socket, resolves CallResult/CallError in
  the pending store and hands inbound Calls to handlers. It never runs a handler
  itself: it starts a handler goroutine when a slot is free and otherwise parks
  the frame in a small bounded queue (`InboundQueueSize`) that finishing
  handlers drain. It only stops reading when that queue is full.
- **writes** happen on the goroutine that produced the frame (a handler, a
  `DoCall` caller) under a per-connection FIFO write lock, so there is no writer
  goroutine or queue; `WriteTimeout` is enforced by one lazily armed timer.
- **teardown** (close frame, waiting for the reader, cleanup) is a
  `context.AfterFunc` callback that runs once when the connection ends.
- the read idle watchdog and the WebSocket ping are `time.AfterFunc` timers; a
  goroutine exists for the ping only while it waits for the pong. On the CP side
  `WithHeartbeatInterval` adds one goroutine (the keepalive ticker).
- each inbound Call runs on its own short-lived goroutine, bounded as described
  below, or on a resident worker when a `HandlerPool` is configured.

Socket buffers are 1 KB + 1 KB per connection (`csms.WithIOBufferSizes`).

Per-connection cost at 2,000 idle connections (`BenchmarkIdleFootprint`): about
8 KB heap + 4 KB stack, 1 goroutine; RSS about 13 KB (15 KB after one
Heartbeat). 10,000 idle connections therefore take well under 200 MB. Typical
CSMS traffic is bursty-but-light (Heartbeat / StatusNotification), so the
baseline dominates and handler goroutines are few.

### Handler worker pool (optional)

Every inbound Call starts a goroutine whose stack grows from 2 KB as the
handler (JSON decode, schema validation, your code) goes deeper; at tens of
thousands of requests per second that growth (`runtime.copystack`) can be a
double-digit share of server CPU. `dispatcher.NewHandlerPool(n)` with
`csms.WithHandlerPool` (or `cp.WithHandlerPool`) keeps `n` resident workers with
warm stacks. It is a hand-off, not a queue: a Call goes to a worker only if one
is idle at that moment and otherwise gets a new goroutine as before, so a slow
handler never delays another connection and the pool adds no concurrency limit.
Size it around the number of handlers typically in flight (tens, not thousands:
a pool far larger than that rotates through cold workers and is slower). Measure
with your own handlers before enabling it; it is off by default.

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

- **Baseline goroutines** (`~N`) and **socket/file descriptors** scale with the
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
| `InboundQueueSize` (`dispatcher.Config`, `WithInboundQueueSize`) | 4 | Bounded queue of inbound CALLs waiting for a handler slot; only fills while all slots are busy, then back-pressure reaches the socket. |
| `OutboundQueueSize` (`dispatcher.Config`, `WithOutboundQueueSize`) | ignored | Kept for compatibility; there is no writer queue any more (senders write under a per-connection FIFO lock). |
| `WithCallTimeout` | 30s | Deadline for an outbound CSMS→CP call awaiting its result. |
| `WithWriteTimeout` | 10s | Deadline for a single socket write. |
| `WithConcurrentCalls()` | serialized | Outbound CALLs are limited to one outstanding per connection by default (OCPP-mandated); later calls wait in a bounded per-connection FIFO (`WithAsyncQueueSize`, 64). This option lifts the limit (non-compliant). Measured overhead: see `core/dispatcher/call_bench_test.go`; in-process, serialized and concurrent are within noise (about 7.1 µs and 55 allocs per call on one connection; about 0.65 M calls/s at 1,000 connections and 0.58 M calls/s at 10,000, 56 allocs per call, in both modes). |
| `WithHandlerTimeout(d)` | 60s | Per-handler deadline (0 = none). |
| `WithOutboundValidation(bool)` | true | With false, payloads gocpp itself produces (handler replies, outbound Calls and Sends) skip schema validation and lenient repair. Inbound validation is unchanged. At high request rates it removes the response-side validation pass (about half of the lenient schema cost per request); keep it on while developing handlers that build raw payloads. |
| `WithMaxMessageSize(n)` | 1 MiB | Largest inbound WebSocket message. |
| `csms.WithMaxConnections(n)` | unlimited | Connection cap; over it, upgrades get HTTP 503 + Retry-After. |
| `WithWebSocketPingInterval(d)` | disabled | Transport keepalive (a timer, plus a goroutine only while a pong is awaited); detects dead peers via pong timeout (`WithWebSocketPongWait`, default 60s). |
| `WithIOBufferSizes(read, write)` | 1024, 1024 | Per-connection socket buffers (net/http would use 4096 each). |
| `WithHandlerPool(pool)` | off | Resident handler workers with warm stacks; see "Handler worker pool". |

## Rules of thumb

- Mostly-idle fleets (Heartbeat-dominated): the per-connection baseline (about 13 KB) is the main cost;
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
