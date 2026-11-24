# Structural issues in ocpp-go v0.19.0

This page lists design-level problems in
[`lorenzodonini/ocpp-go`](https://github.com/lorenzodonini/ocpp-go) **v0.19.0**
that can be reproduced with small programs, explains why each follows from the
design rather than from a single faulty line, and says how gocpp behaves in the
same situation, both through its native API and through the
[drop-in module](../compat/ocppgo/README.md).

It is written for people who run ocpp-go in a CSMS and want to know which
failure modes to expect, and for people evaluating gocpp who want to see what
it does and does not change. ocpp-go has carried OCPP in Go for years and a
large part of the ecosystem is built on it; this page is a technical analysis
of one tagged version, not a judgement of the project or of the people who
maintain it.

## Scope and method

- **Subject:** the `v0.19.0` tag (2025-02-02, the latest release). Where it
  matters, the page also says whether upstream `master` at commit `b22429c`
  (2025-08-17) differs.
- **Evidence:** every claim is backed by (a) a `file:line` reference into the
  v0.19.0 source, and (b) a repro program in
  [`interop/ocppgo-repro/`](../interop/ocppgo-repro/) with its stored output in
  [`interop/ocppgo-repro/out/`](../interop/ocppgo-repro/out/). The programs use
  only ocpp-go's public API on the server side, configured as its README
  shows; the charge points are plain WebSocket clients that follow a script.
- **Variants:** each repro was run against upstream v0.19.0 and, from the same
  source with one `replace` line, against the gocpp drop-in
  (`compat/ocppgo v0.19.0-gocpp.2` on gocpp v0.3.3). One repro also has a
  variant on the native gocpp API.
- **Words used below:** *reproduced* means the problem occurred; *not
  reproduced* means it did not. "Drop-in" is the compatibility module;
  "native" is gocpp's own API (`csms`, `cp`).
- Things that turned out **not** to be problems, and the limits of this
  analysis, are listed in [Limits](#limits-of-gocpp-and-of-this-analysis).
  Please read that section before drawing conclusions.

## Summary

| # | Issue | Repro | ocpp-go v0.19.0 | `master` b22429c | gocpp drop-in | gocpp native |
|---|---|---|---|---|---|---|
| 1 | One dispatcher goroutine and one lock for all charge points; blocking channel sends while the lock is held | r01, r02 | all charge points stop being served (r01: 10/10 runs; r02: 9/10 runs, the other run crashed) | blocking sends unchanged | not reproduced | no shared dispatcher |
| 2 | WebSocket write: blocking send while the connection-table lock is held | r03 | one stalled reader stops all writes and new connections | unchanged | not reproduced | no write queue |
| 3 | Responses are matched to callbacks by position, not by message id | r08 | misdelivered responses; process exit through the typed API | unchanged | race not reproduced; reconnect case fixed by default (next release after `v0.19.0-gocpp.2`; `v0.19.0-gocpp.2` and earlier: reproduced) | matched by message id |
| 4 | A panic in an application handler ends the process | r04 | process exits | unchanged | contained (`InternalError`) | contained (`InternalError`) |
| 5 | No inbound frame size limit | r05 | 64 MiB frame accepted, RSS +349 MiB | unchanged | same by default; limit is opt-in | 1 MiB by default |
| 6 | Duplicate id: the first connection wins | r06 | reconnecting charge point refused for 60 s | unchanged | same by default; last-wins is opt-in | last wins by default |
| 7 | JSON numbers pass through `float64` | r07 | 64-bit ids silently changed or rejected | unchanged | **reproduced** (same decoding code) | not reproduced |
| 8 | Each inbound frame is decoded twice | r09 | 1.3x to 2.2x the time of a single typed decode | unchanged | same code | single decode |
| 9 | Hand-written types and `validate` tags differ from the OCA schemas; no OCPP 2.1 | r13 | 3 of 3 schema-valid messages rejected | partly fixed, unreleased | **reproduced** (same tags; leniency is opt-in) | types generated from the schemas; 1.6, 2.0.1, 2.1 |
| 10 | Configuration is process-wide; `SetMessageValidation(false)` does not disable inbound validation | r10 | reproduced | unchanged | **reproduced** (same package variables) | per-server options |
| 11 | No `context.Context` in handlers or calls | (API) | - | unchanged | unchanged (API compatibility) | `ctx` everywhere |
| 12 | Two goroutines and four channels per connection | r12 | 2.00 goroutines per idle connection | unchanged | 1.00 | 1.00 |
| 13 | Client: calling `Stop` twice panics | r11 | process exits | partly changed | **reproduced** (client is ocpp-go's code) | separate implementation |

## 1. Server dispatcher: one goroutine, one lock, blocking sends

**Design.** Every request the CSMS sends, for every charge point, goes through
a single goroutine, `DefaultServerDispatcher.messagePump`
([`ocppj/dispatcher.go:476-571`](https://github.com/lorenzodonini/ocpp-go/blob/v0.19.0/ocppj/dispatcher.go#L476-L571),
started at `:410`). The parts involved:

| Part | Where | Property |
|---|---|---|
| `d.mutex` (`sync.RWMutex`) | `dispatcher.go:375` | shared by the dispatcher and the pending-request state (`NewServerState(&d.mutex)`, `:399`) |
| `requestChannel` | `:406` | capacity 20; `SendRequest` (`:468-470`) and `DeleteClient` (`:439-441`) send on it **while holding the read lock** |
| `timerC` | `:407` | capacity 10; each timeout goroutine sends on it **while holding the read lock** (`:614-618`) |
| `readyForDispatch` | `:396` | capacity 1; `CompleteRequest` sends on it (`:648`), and the pump, its only reader, **also calls `CompleteRequest`** (`:536`, `:589`) |
| pending state | `ocppj/state.go:145-181` | `AddPendingRequest`, `HasPendingRequest` and `GetClientState` take the **write** lock, also for lookups |
| network write | `dispatcher.go:585` | the pump calls `network.Write` synchronously |

A `sync.RWMutex` makes new readers wait once a writer is waiting. So whenever a
goroutine holds the read lock while it waits for channel space, and the only
goroutine that can make space (the pump) waits for the write lock, neither can
proceed. The inbound path takes the same lock for every frame
(`GetClientState`, `ocppj/server.go:241`), so from that moment **no charge
point gets an answer to anything**, including Heartbeat and BootNotification.
Connections stay open; only OCPP stops.

There are at least four ways into this state:

1. **Burst of calls.** `requestChannel` is full (20), `SendRequest` waits on it
   with the read lock, and the pump waits for the write lock in
   `dispatchNextRequest` -> `AddPendingRequest` (`:584`).
2. **Timeouts firing together.** More than 10 timeout goroutines wait on
   `timerC` with the read lock, and the pump waits for the write lock in
   `HasPendingRequest` (`:532`).
3. **The pump blocks on itself.** A regular answer has filled the one slot of
   `readyForDispatch`; the pump then handles a timeout and sends on the channel
   that only it reads (`:536` -> `:648`).
4. **Many disconnects.** `DeleteClient` waits on `requestChannel` with the read
   lock (`:439-441`).

A fifth outcome of the same race is a nil dereference: `dispatchNextRequest`
uses the result of `q.Peek()` without checking it (`:580-584`), and the queue
can be empty when an answer and a timeout for the same request cross.

**What goes wrong.**

- **[r01](../interop/ocppgo-repro/r01_dispatcher_freeze/main.go)** (path 1):
  200 healthy charge points; the CSMS sends one `TriggerMessage` to each from a
  plain `for` loop. No charge point misbehaves.

  ```
  send loop STILL BLOCKED inside SendRequestAsync after 5 s: 21 of 200 calls sent
  canary Heartbeat 0: NO ANSWER within 3s … (calls sent 21, answered 0 of 200)
       1  chan send | ocppj.(*DefaultServerDispatcher).SendRequest <- …
       1  sync.RWMutex.Lock | ocppj.(*serverState).AddPendingRequest <- …dispatchNextRequest <- …messagePump
  ```

  Reproduced in **10 of 10** consecutive runs
  ([tally](../interop/ocppgo-repro/out/r01_dispatcher_freeze.upstream.tally.txt)).
  The number of calls sent before the loop blocks depends on scheduling (21 in
  those runs; 79 in another run on a busier machine); the outcome is the same.
  Drop-in: 200/200 answered, canary 4/4
  ([output](../interop/ocppgo-repro/out/r01_dispatcher_freeze.compat.txt)).

- **[r02](../interop/ocppgo-repro/r02_timeout_selfdeadlock/main.go)** (paths 2
  and 3): 300 charge points that never answer CSMS calls plus 8 healthy ones.
  Calls are paced so that path 1 is excluded. The server stops when the
  timeouts begin.

  ```
  canary Heartbeat 1: NO ANSWER within 3s … (healthy calls answered 101645, mute timeouts delivered 0/300)
     289  chan send | ocppj.(*DefaultServerDispatcher).waitForTimeout
       1  chan send | ocppj.(*DefaultServerDispatcher).CompleteRequest <- ocppj.(*DefaultServerDispatcher).messagePump
  ```

  In 10 consecutive runs the server stopped serving in **9** and the **process
  exited** with a nil dereference at `dispatcher.go:584` in **1**
  ([tally](../interop/ocppgo-repro/out/r02_timeout_selfdeadlock.upstream.tally.txt),
  [crash](../interop/ocppgo-repro/out/r02_timeout_selfdeadlock.upstream.crash-run.txt)).
  The split between the two outcomes varies from run to run. The repro uses a
  2 s call timeout to keep the run short; the default is 30 s. Drop-in: 300/300
  timeouts delivered, healthy calls keep flowing, canary 5/5.

- **Load test** (supporting measurement, single runs, see
  [Limits](#limits-of-gocpp-and-of-this-analysis)): 4,000 simulated OCPP 1.6
  charge points on loopback, 40% of them never answering CSMS calls, 50 CSMS
  calls per second, 30 s call timeout, 25 minutes with 5 scheduled restarts.

  | | ocpp-go v0.19.0 | gocpp drop-in | gocpp native (v0.3.0) |
  |---|---|---|---|
  | Server stopped serving all charge points | **6 times in 6 starts**, 52 to 95 s after each start | never | never |
  | Time with no message processed | **1,035 of 1,500 s (69%)** | 0 | 0 |
  | Charge points connected at the end | **0** | 3,989 | 3,989 |
  | Max RSS / goroutines | 556 MB / 23,737 | 154 MB / 5,988 | 164 MB / 5,310 |

  With 5 calls per second the v0.19.0 server stopped twice in 12 minutes. With
  no CSMS calls it did not stop at 4,000 charge points, but it did stop during
  a connection ramp at about 31,700 connections (path 4). The goroutine dumps
  of the stopped servers show the same blocked sends as r01 and r02. The
  harness is on the branch `test/loadtest-4k`, not on `main`:
  [`interop/loadtest/`](https://github.com/shiv3/gocpp/tree/test/loadtest-4k/interop/loadtest)
  (its [README](https://github.com/shiv3/gocpp/blob/test/loadtest-4k/interop/loadtest/README.md)
  lists every flag). To run it, check out that branch, build a CSMS binary
  from `interop/parity/` (`cs-gocpp`, `cs-ocppgo-upstream` or
  `cs-ocppgo-compat`) and the harness, then for example
  `loadtest -server-bin ./cs-ocppgo-upstream -steps 4000:50:25m -restarts 5 -call-timeout 30s`
  (`-mute 0.4` is the default mix shown above; add `-server-args=-compat`
  for the drop-in preset of `cs-gocpp`). A freeze is detected by two canary
  charge points; the harness then saves a goroutine dump. Treat
  the figures as context; r01 and r02 reproduce the mechanism in seconds.

**Why it is structural.** Each path can be patched (make one send
non-blocking, take a read lock instead of a write lock), and the pull requests
that do so are listed under [Maintenance status](#maintenance-status). What
remains after such patches is that all charge points share one goroutine and
one lock, and that goroutine performs network writes. Anything the pump calls
that can block, blocks every charge point, so every later change to the pump's
path needs the same analysis again. Giving each connection its own state means
replacing the exported `ServerDispatcher`, `ServerState` and `ServerQueueMap`
interfaces. On `master`, the code after v0.19.0 adds nil checks in the timeout
branch (commit `d291e8c`); the blocking sends and the unchecked `Peek` in
`dispatchNextRequest` are still there.

**gocpp.** There is no dispatcher goroutine shared between connections.
Pending calls live in a per-connection store
(`core/dispatcher/pending.go`); its lock only guards the table, channel sends
happen outside it, and each result channel has capacity 1. Timeouts are
per-call timers. The goroutine that issues a call writes it, under a
per-connection lock ([architecture.md](architecture.md#connection-lifecycle)).
The drop-in uses this runtime for `ocppj.Server`, which is why r01 and r02 do
not reproduce on it. **Exception:** an application that injects its own
`ServerDispatcher` into the drop-in runs ocpp-go's original dispatcher; see
the [checklist](#if-you-use-the-drop-in).

## 2. WebSocket write path: blocking send under the connection-table lock

**Design.** `ws.Server.Write` holds `connMutex.RLock` (the lock of the table of
all connections) while it sends on the connection's `outQueue`, a channel of
capacity 1 (`ws/websocket.go:462-472`, `:534`). `cleanupConnection` takes the
write lock of the same mutex (`:674-680`), and so does `wsHandler` for every
new connection (`:551`).

**What goes wrong.** When a charge point stops reading its socket: the
`writePump` blocks in `WriteMessage` (10 s by default); the next message fills
`outQueue`; a third `Write` waits on the channel with the read lock; after the
write timeout the `writePump` enters `cleanupConnection` and waits for the
write lock. Only that goroutine could release the third writer, so both wait
forever, and because a writer is waiting, **every `Write` to every other charge
point and every new connection waits too**.

**[r03](../interop/ocppgo-repro/r03_stalled_reader_freeze/main.go):** one
client sends three `DataTransfer` requests and stops reading (the answers are
16 MiB each so that they exceed the socket buffers; the write timeout is 3 s to
keep the run short).

```
t= 3.0s canary Heartbeat: answered in 160µs
t= 6.0s canary Heartbeat: NO ANSWER within 2 s      <- and from then on
       3  sync.Mutex.Lock | ws.(*Server).wsHandler <- …
       1  chan send | ws.(*Server).Write <- ocppj.(*Server).SendResponse <- …
       1  sync.RWMutex.Lock | ws.(*Server).cleanupConnection <- ws.(*Server).writePump
```

Drop-in: canary answered 11/11.

The precondition is narrow: the unread data must exceed the socket buffers, so
small messages to a half-open connection do not trigger it quickly. Large
payloads (local lists, certificates, diagnostics), sustained traffic to a dead
peer, or a client that does this on purpose do. Short of the permanent stop,
the pump of section 1 calls `Write` synchronously, so one slow write delays
CSMS calls to all charge points for up to the write timeout.

**Why it is structural.** The fix changes the write contract (per-connection
lock, bounded queue, and a decision about what a full queue means). `master`
restructured the ws package after v0.19.0, and `server.Write` there still
holds `connMutex.RLock` across the queue send (`ws/server.go:345-354` at
`b22429c`). Pull requests #408 and #414 address it.

**gocpp.** No writer goroutine and no queue: the goroutine that produced a
frame writes it under a per-connection FIFO lock, and a connection whose write
exceeds `WriteTimeout` is aborted on its own
([performance.md](performance.md#goroutine-model)). No I/O happens under the
connection-table lock.

## 3. Responses are matched by position, not by message id

**Design.** The callback given to `SendRequestAsync` is appended to a FIFO per
charge point id (`internal/callbackqueue/callbackqueue.go:9-12`). When a
response, an error or a timeout arrives, the **first** callback of that charge
point is taken (`ocpp1.6/central_system.go:697-726`); the `requestId`
parameter is not used for the lookup. Correctness rests on an implicit
invariant: the dispatcher sends one request per charge point at a time and
completions arrive in submission order. The state for one request lives in
three places (request queue, pending state, callback queue).

**What goes wrong.** The invariant breaks in these cases:

- **Timeout racing a late answer.** The inbound path interprets the answer
  against the pending request, calls `dispatcher.CompleteRequest`, and then
  calls `responseHandler` whether or not the completion succeeded
  (`ocppj/server.go:274-280`). If the pump has already completed the same
  request as timed out (`dispatcher.go:536-541`), two callbacks are dequeued
  for one request, and the second belongs to the **next** request.
- **No disconnect handler registered.** The only code that clears a charge
  point's callbacks on disconnect is inside the wrapper installed by
  `SetChargePointDisconnectedHandler` (`central_system.go:501-509`). If the
  application never calls that method (it is optional), the callback of a
  request pending at disconnect stays queued and receives the first answer
  after the reconnect.
- **Typed API.** `cs.ChangeAvailability(...)` and the other typed senders
  assert the response type unconditionally (`central_system.go:71` and the
  equivalent line in every method). A misdelivered response of another type
  panics, and since callbacks run in a bare `go callback(...)` (`:700`), the
  **process exits**.
- **One lock for all charge points.** `TryQueue` holds a single mutex for all
  charge points while it calls `SendRequest` (`callbackqueue.go:20-26`), so
  when section 1 blocks `SendRequest`, response handling for everyone blocks
  behind it.

**[r08](../interop/ocppgo-repro/r08_callback_fifo/main.go)** (call timeout
2 s; [output](../interop/ocppgo-repro/out/r08_callback_fifo.upstream.txt)):

- **A. The charge point answers with a payload that fails validation.** No
  misdelivery. The application hears about it only when the call timeout
  expires, as `GenericError - Request timed out`, not as a validation error,
  and the next call to that charge point is held back until then. The charge
  point receives a CALLERROR in reply to its CALLRESULT.
- **B. The answer arrives within 1.5 ms of the timeout** (300 charge points):
  in the stored run **11 of 300** `ChangeAvailability` callbacks were given a
  `*core.GetConfigurationConfirmation`; another run gave 4. The count varies
  per run; it is a race.
- **C. No disconnect handler:** the callback of the `ChangeAvailability` sent
  before the drop was called with the `GetConfiguration` response sent after
  the reconnect. Every run.
- **C2. The same through the typed API:**

  ```
  panic: interface conversion: ocpp.Response is *core.GetConfigurationConfirmation, not *core.ChangeAvailabilityConfirmation
  …ocpp1%2e6.(*centralSystem).ChangeAvailability.func1 … central_system.go:71
  ```

When the misdelivered response has the same type as the expected one, there is
no panic and no log line: the application receives another request's result.

Related upstream reports: #57, #66, #67 (2021, closed), #294, #306 (2024, closed),
[#363](https://github.com/lorenzodonini/ocpp-go/issues/363) (2025, open),
PR #432 (2026, open).

**Why it is structural.** Individual races can be closed (for example by not
calling `responseHandler` when `CompleteRequest` did not complete anything).
Ruling misdelivery out requires looking the callback up by message id, which
changes the glue between `ocppj` and the 1.6 / 2.0.1 packages.

**gocpp.** Native: a response is matched to its pending call by message id; the
response type is fixed by the type parameters of `csms.Call[Req, Resp]`, so
there is no assertion; a late answer to a call that already timed out is
dropped. **Drop-in:** the 1.6 / 2.0.1 glue, including the callback FIFO, is
still ocpp-go's code, with one change by default: pending callbacks of a
charge point are failed (`client disconnected, no response received from
client`) when it disconnects, **whether or not the application registered a
disconnect handler**. B did not reproduce (0 of 300) and C2 does not end the
process. **C is fixed** in the release after `v0.19.0-gocpp.2`
([output](../interop/ocppgo-repro/out/r08_callback_fifo.compat.txt), made with
the module from this repository; on `v0.19.0-gocpp.2` and earlier C reproduces:
the stale callback receives the wrong response). The FIFO itself stays: the
gocpp runtime keeps the order of completions of one connection, so matching by
position is correct as long as a disconnect clears the queue. A (a response
that fails validation is reported as a timeout, and the next call waits) is also
unchanged by default, because applications may rely on the timing;
`compat.SetValidationErrorToCallback(endpoint, true)` makes the callback hear
the validation error at once.

## 4. A handler panic ends the process

**Design.** Each inbound request runs in `go func() { … }()` without `recover`
(`ocpp1.6/central_system.go:659-694`); callbacks likewise (`:700`, `:710`,
`:720`). The library code of v0.19.0 contains no `recover()`.

**What goes wrong.**
**[r04](../interop/ocppgo-repro/r04_handler_panic/main.go):** a nil
dereference in the Heartbeat handler for one charge point ends the server
process with exit status 2; an unrelated charge point sees close 1006. One
application bug that only one vendor's payload triggers disconnects the whole
fleet, and the reconnect wave that follows is itself a trigger for sections 1
and 6.

**Why it is structural.** This one is a missing safeguard rather than a design
that prevents the fix: adding `recover` at the handler and callback launch
sites (1.6 and 2.0.1, both roles) is a local change, and applications can wrap
their own handlers today. It is listed because the default outcome is
process-wide. Upstream request: #205 (2023, open).

**gocpp.** Handlers and callbacks run with panic recovery; the charge point
gets `[4,"<id>","InternalError","internal error",{}]` and both the connection
and the process continue. Same on the drop-in
([output](../interop/ocppgo-repro/out/r04_handler_panic.compat.txt)) and
native (end of
[this output](../interop/ocppgo-repro/out/r07_number_precision.native.txt)).

## 5. No inbound frame size limit

**Design.** The server calls `conn.ReadMessage()` with no limit
(`ws/websocket.go:593`; there is no `SetReadLimit` call in v0.19.0 or
`master`). The frame is then copied several more times by the decode path of
section 8.

**What goes wrong.**
**[r05](../interop/ocppgo-repro/r05_unbounded_frame/main.go):** one 64 MiB
`DataTransfer` raises peak RSS from 11 MiB to 360 MiB (5.5 times the frame) and
is answered with `Accepted`. A single client, hostile or just broken, can
exhaust the memory of the process with a few frames.

**Why it is structural.** It is not: a read limit is a local addition. It is
listed because the default is unbounded.

**gocpp.** Native: 1 MiB by default (`csms.WithMaxMessageSize`), larger frames
close the connection with 1009. **Drop-in: unbounded by default**, to match
upstream; with `compat.SetMaxMessageSize(srv, 1<<20)` the same frame is refused
with close 1009 and RSS grows by 3 MiB
([output](../interop/ocppgo-repro/out/r05_unbounded_frame.compat-safe.txt)).

## 6. Duplicate id: the first connection wins

**Design.** If the connection table already has the id, the new connection is
upgraded and then closed with 1008 "a connection with this ID already exists"
(`ws/websocket.go:553-561`). The old entry is only removed on a read error or
when the read deadline passes (60 s by default).

**What goes wrong.**
**[r06](../interop/ocppgo-repro/r06_duplicate_lockout/main.go):** a charge
point whose link died without a FIN reaching the server (power cut, NAT
timeout, cellular handover) reconnects every 5 s. With default settings it is
refused **11 times** and served again after **60 s**. Related upstream reports:
#314, #336, #358.

**Why it is structural.** The policy itself is a local decision (PR #376
proposes making it configurable). Doing last-wins correctly also requires
ordering the old connection's teardown against the new one's registration,
which runs through the code of section 2.

**gocpp.** Native: last wins by default (the old connection is closed with
1008, the new one is served); `DuplicatePolicyRejectNew` and others are
available. **Drop-in: first wins by default**, to match upstream; with
`compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)` the charge point is
served on its first retry
([output](../interop/ocppgo-repro/out/r06_duplicate_lockout.compat-safe.txt)).

## 7. JSON numbers pass through float64

**Design.** The first decoding step (section 8) unmarshals the whole frame into
`[]interface{}`, so every JSON number becomes a `float64` before it reaches the
typed struct.

**What goes wrong.**
**[r07](../interop/ocppgo-repro/r07_number_precision/main.go)**
(`transactionId` of a 1.6 `StopTransaction`):

```
wire transactionId 9007199254740993     -> handler saw 9007199254740992     CHANGED SILENTLY; reply [3,"m1",{}]
wire transactionId 1152921504606846977  -> handler saw 1152921504606847000  CHANGED SILENTLY; reply [3,"m2",{}]
wire transactionId 9223372036854775807  -> handler NOT called; reply [4,"m3","FormationViolation","json: cannot unmarshal number 9223372036854776000 into Go struct field .transactionId of type int",{}]
```

Integers above 2^53 are rounded without an error; values near the int64
maximum are rejected. Charge points that only use ids that fit in 32 bits are
not affected. Upstream report with a real charger sending
`9223372036854775807`: #188 (2023, open); PR #191 (use `json.Number`) is open.

**Why it is structural.** It is a consequence of section 8: the intermediate
`[]interface{}` is part of the exported API (`ParseMessage` takes it).

**gocpp.** Native decodes the payload straight into generated structs with
`int64` fields; all five values arrive unchanged
([output](../interop/ocppgo-repro/out/r07_number_precision.native.txt)).
**Drop-in: reproduced, identical output**, because payload decoding is
ocpp-go's code.

## 8. Each inbound frame is decoded twice

**Design.** An inbound frame is (1) unmarshalled into `[]interface{}` with the
payload as `map[string]interface{}` (`ocppj/ocppj.go:229-236`), (2) the payload
is marshalled back to bytes, (3) unmarshalled into a value created with
`reflect.New(requestType)` (`:363-378`, responses `:380-395`), and (4)
validated by reflection with go-playground/validator (`:441`).

**What it costs.**
**[r09](../interop/ocppgo-repro/r09_decode_bench/bench_test.go)** compares that
path with one typed decode of the same structs through the same validator
(`-cpu 1`, i5-13600K;
[output](../interop/ocppgo-repro/out/r09_decode_bench.upstream.txt)):

| Frame | ocpp-go path | single typed decode | ratio |
|---|---|---|---|
| Heartbeat | 2,187 ns, 504 B, 20 allocs | 1,726 ns, 344 B, 11 allocs | 1.3x |
| StatusNotification | 6,540 ns, 1,392 B, 49 allocs | 3,405 ns, 608 B, 15 allocs | 1.9x |
| MeterValues (8 sampled values) | 35,676 ns, 9,891 B, 238 allocs | 16,304 ns, 4,320 B, 86 allocs | 2.2x |

This is a micro-benchmark of the decode step only. In an end-to-end load test
it did not make ocpp-go slower than gocpp (see
[Limits](#limits-of-gocpp-and-of-this-analysis)). The more important
consequences are the side effects: numbers go through `float64` (section 7),
and a large frame is held several times over (section 5).

**Why it is structural.** `ParseMessage(arr []interface{}, …)` is exported, so
changing the path changes the public API of `ocppj`.

**gocpp.** Native splits the envelope and decodes the payload once into the
generated type. The drop-in runs ocpp-go's parsing and validation code, so
this item is unchanged there.

## 9. Hand-written types and validation tags; no OCPP 2.1

**Design.** The message types of 1.6 and 2.0.1 are written by hand, with 630
`validate:"…"` struct tags and custom validators registered in package `init`
functions. The official OCA JSON schemas are not used at build time or at run
time, so nothing checks one against the other.

**What goes wrong.** The differences fall into classes:

1. **`required` means two things.** In the tags it means "not the zero value";
   in JSON Schema it means "the property is present". So `0`, `0.0` and `""`
   cannot be sent or received in places where the schema allows them.
   **[r13](../interop/ocppgo-repro/r13_tag_drift/main.go):** 1.6
   `ChangeConfiguration` with `value: ""` (#246, open), 2.0.1
   `CostUpdatedRequest` with `totalCost: 0`, and 2.0.1
   `ClearVariableMonitoringResponse` with `id: 0` are valid against the
   official schemas, and v0.19.0 rejects **3 of 3**. Earlier reports of the
   same class: #151, #157, #244 (closed), #425 (open).
2. **Types that differ from the schema.** 1.6 `DataTransfer.data` is `string`
   in the schema and `interface{}` in the code (#257, open).
3. **Enums, structures, bounds.** #426, #385 / PR #386, #424 (all open). After
   v0.19.0, `master` fixed `GetCompositeScheduleResponse` (commit `9356607`);
   that fix is not in a release.
4. **Error code spelling.** The OCPP 1.6 specification spells the error code
   `OccurenceConstraintViolation`; v0.19.0 sends `OccurrenceConstraintViolation`
   on 1.6 connections (`ocppj/ocppj.go:194`; visible in the r10 and r08
   outputs). Fixed on `master` (commit `7bee131`, which renames exported
   constants), not released.
5. **OCPP 2.1.** v0.19.0 has the packages `ocpp1.6` and `ocpp2.0.1` only.
   Issue #337 and PR #371 are open.

**Why it is structural.** Each difference is easy to fix once found. Without a
mechanical link to the schemas, they are found one at a time, usually when a
new charger model is connected.

**gocpp.** Native: the types for 1.6, 2.0.1 and 2.1 are generated from the OCA
schemas (CI fails if `make codegen` changes anything), and messages can be
validated at run time against the embedded schemas (opt-in:
`csms.WithLenientSchema()` or `WithStrictSchema`; the library default is no
schema validation). gocpp has hand-written corrections too
(`schemas/overrides/`), and constraints that the schemas do not express are
not enforced. **Drop-in: r13 reproduces 3 of 3**, because validation is
ocpp-go's tags; per-server leniency is opt-in
([compat README](../compat/ocppgo/README.md#lenient-validation-for-real-chargers)).

## 10. Process-wide configuration

**Design.** Configuration lives in package variables: `ocppj.Validate` (the
validator instance, aliased by `ocpp1.6/types.Validate` and
`ocpp2.0.1/types.Validate`; `ocppj/ocppj.go:19`), `validationEnabled` (`:22`),
the loggers of `ocppj` and `ws`, `EscapeHTML` (`:27`), the message id
generator, and `types.DateTimeFormat` (one per version package). They are read
on the hot path without synchronisation.

**What goes wrong.**
**[r10](../interop/ocppgo-repro/r10_globals/main.go)** runs two central
systems, A and B, in one process
([output](../interop/ocppgo-repro/out/r10_globals.upstream.txt)):

- Calling `ocppj.SetMessageValidation(false)` for B's benefit lets **A** send
  an invalid `RemoteStartTransaction` (empty `idTag`) that it refused a moment
  before. Settings cannot differ per server, tenant, or OCPP version (#125,
  #407).
- `SetMessageValidation(false)` is documented as "outgoing and incoming OCPP
  messages will not be validated anymore", but the inbound path validates
  unconditionally (`ocppj/ocppj.go:441`, `:462`, `:496`); only the outbound
  functions check the flag (`:524`, `:547`, `:565`). After the call, B still
  rejects a `BootNotification` with an empty vendor. Tolerating a charger's
  quirk therefore means editing the tags.
- Under `go test -race`, calling `SetLogger` while another server is serving
  is reported as a data race.

**Why it is structural.** Moving a variable into the endpoint is possible one
at a time, but `Validate` is an exported variable and validator registration
is spread over the packages' `init` functions. PR #391 (thread-safe globals)
is open.

**gocpp.** Native: configuration is per server, through options
(`csms.WithLenientSchema()`, `WithTimeEncoding(...)`, ...). **Drop-in: r10
reproduces**; the package variables are part of ocpp-go's API and are kept.
The settings the drop-in adds (`compat.*`) are per server.

## 11. No context.Context

**Design.** Handlers have the shape
`OnHeartbeat(chargePointId string, request *core.HeartbeatRequest) (…, error)`
and sending is `SendRequestAsync(clientId, request, callback)`; no function in
the public API takes a `context.Context`.

**Consequences.**

- A queued request cannot be cancelled (PR #105, open since 2021).
- The call timeout is one value for the whole dispatcher (`SetTimeout`,
  `dispatcher.go:426-428`), not per request (#165); internally it is
  `context.WithTimeout(context.TODO(), …)` (`:598`).
- A handler that is running (a database query, a call to another service) is
  not told that its charge point has disconnected.
- `Stop` is `Shutdown(context.TODO())` (`ws/websocket.go:430`); it cannot be
  given a deadline.
- There is no carrier for trace ids or request-scoped values from the
  connection to the handler and on to downstream calls.

**Why it is structural.** Adding `ctx` changes the signature of every handler
interface and every send function, which is a new major API.

**gocpp.** Native:
`csms.On(srv, msg, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error))`
and `csms.Call(ctx, c, msg, req)`. The `ctx` is bound to the connection and is
cancelled when it ends; per-call deadlines come from `ctx`. The drop-in keeps
ocpp-go's API, so it has no `ctx` either.

## 12. Goroutines per connection

**Design.** Each server connection has a `readPump` and a `writePump`
goroutine (`ws/websocket.go:566-567`) and four channels (`:534-537`); each
pending call adds a timeout goroutine (`dispatcher.go:565`); each inbound
request starts a goroutine with no per-connection bound
(`central_system.go:659`).

**Measured.**
**[r12](../interop/ocppgo-repro/r12_per_connection/main.go):** 2,000 idle
connections cost **2.00** goroutines each on v0.19.0 and **1.00** on the
drop-in. In the load test of section 1 (server in its own process), RSS per
connection during the ramp was 35 to 38 KB on v0.19.0, 22 to 23 KB on the
drop-in and 17.5 to 23 KB on native gocpp.

**Bridge mode.** The 1.00 above holds when the drop-in owns the sockets,
that is, when the `ws.WsServer` passed to `ocppj.NewServer` /
`ocpp16.NewCentralSystem` is the module's own `*ws.Server`. An application
that wraps it in its own `ws.WsServer` (for example to add authentication or
routing in front of it) no longer matches that type, and the module then runs
in *bridge mode*
([`compat/ocppgo/ocppj/server_gocpp.go`](../compat/ocppgo/ocppj/server_gocpp.go),
`newGocppRuntime` and `attach`): the application's server keeps the sockets and
the runtime is fed through its message handler. Each connection then has the
wrapped server's own goroutines **plus** the runtime's reader. Measured with
`./run.sh r12 compat -- -n 1000 -bridge` (the program wraps the module's
`*ws.Server` in a type that only delegates):
**3.00 goroutines per connection** (reader of the runtime's bridge, the wrapped
server's `writeLoop`, and its socket reader), against 1.00 without the wrapper
([output](../interop/ocppgo-repro/out/r12_per_connection.compat-bridge.txt));
the same wrapper on upstream v0.19.0 stays at 2.00
([output](../interop/ocppgo-repro/out/r12_per_connection.upstream-bridge.txt)).
So in bridge mode the drop-in uses more goroutines per connection than
upstream, not fewer; the dispatcher fixes of sections 1 and 2 still apply. The
`compat.Set*` options that act on sockets (`SetDuplicatePolicy`,
`SetMaxMessageSize`, `SetIdleClose`, `SetAdmission`, ...) take a
`*ws.Server`: call them on the inner `*ws.Server` that your wrapper delegates
to, not on the wrapper.

At a few thousand connections this difference does not matter. It matters for
how many connections fit in one process and for headroom during a reconnect
wave. The two-pump layout is the usual way to use gorilla/websocket, so this is
a property of the transport design, not a defect.

**gocpp.** One resident goroutine per connection (the reader); writes happen on
the calling goroutine; timers instead of watchdog goroutines
([performance.md](performance.md#goroutine-model)).

## 13. Client: calling Stop twice panics

**Design.** `ws.Client.Stop` closes `client.reconnectC` without a guard
(`ws/websocket.go:1137-1139`).

**What goes wrong.**
**[r11](../interop/ocppgo-repro/r11_client_crash/main.go) part A:** calling
`Stop` twice on an `ocpp16.ChargePoint` (for example from a signal handler and
a deferred cleanup) ends the process with `panic: close of closed channel`.

Part B of the same program (200 clients sending while the server drops all
connections every 300 ms, for 40 s) looked for a `send on closed channel` in
`Write` (`:1054-1060` against `:999-1007`). **It did not crash.** What it did
show is a period of more than 10 s in which no Heartbeat was answered although
the server was up and the clients reported `IsConnected() == true`. This is
not a crash but a stall of the client's request queue, and it follows from how
the client dispatcher handles a request that is in flight when the connection
drops:

1. On a read or write error, `ws.Client.writePump` calls `closure`, which runs
   `cleanup()` and the disconnect handler, and then `handleReconnection()`
   (`ws/websocket.go:905-966`, `cleanup` at `:999-1007`,
   `handleReconnection` at `:1009-1042`); `ocppj.Client.onDisconnected` calls
   `dispatcher.Pause()` (`ocppj/client.go:347-353`).
2. `Pause` only stops the timer and sets `paused` (`ocppj/dispatcher.go:253-261`).
   The request that was in flight stays in the pending state and at the head
   of the queue; it is not re-sent and not failed.
3. After the reconnect, `onReconnected` calls `dispatcher.Resume()`
   (`ocppj/client.go:355-360`). `Resume` sees `HasPendingRequest()` and does
   nothing but re-arm the timer with the full call timeout, 30 s by default
   (`dispatcher.go:263-274`, `defaultMessageTimeout` at `:103`). The answer can
   never arrive on the new connection, and `messagePump` dispatches nothing
   else until that timer fires (`:173-231`, `rdy` stays false).
4. Every further disconnect repeats step 3 and restarts the 30 s. When
   connections drop more often than the timeout, a client whose request was in
   flight at a drop is stuck for as long as the drops continue. Meanwhile
   `IsConnected()` is true, and new requests are accepted into the queue.

To check this, [r11](../interop/ocppgo-repro/r11_client_crash/main.go) has two
more flags: `-flap-for D` makes the server stop dropping connections after
`D`, and `-call-timeout T` sets the client dispatcher's timeout. With drops
stopping at 12 s and 100 clients, throughput fell from about 190,000 answered
Heartbeats per 5 s to about 4,100 (a few clients get through) between 15 s
and 40 s and returned to full speed at 45 s, which is 12 s plus the 30 s
default timeout; with `-call-timeout 3s` it was back at full speed by 15 to
20 s
([output](../interop/ocppgo-repro/out/r11_client_crash.upstream-stall-recovery.txt)).
Recovery time follows the timeout, not the reconnect. A client that reconnects
cleanly with nothing in flight is not affected, so a real charge point that
sends one message at a time and drops rarely sees a stall of at most one
timeout, but a charge point behind a flapping connection can sit idle far
longer. Setting a shorter timeout bounds it
(`ocppj.NewDefaultClientDispatcher(...).SetTimeout`); the stall itself is not
removed by that.

**Why it matters / gocpp.** This affects tools built on ocpp-go's client
(simulators, load generators, tests), not a CSMS. It is a local bug, not a
design issue; it is listed because **the drop-in does not change it**: the
client side of the drop-in is ocpp-go's unchanged code, and r11 gives the same
result on both. gocpp's native client (`cp`) is a separate implementation.

## Upstream master after v0.19.0

Between `v0.19.0` and `b22429c` there are 20 commits (59 files), not part of
any release. They restructure the `ws` package (`ws/server.go`,
`ws/client.go`; `ws.Server` becomes an interface, TLS moves to options), which
changes the exported API; add nil checks in the dispatcher's timeout branch;
fix the 1.6 error code spelling and `GetCompositeScheduleResponse`; and make
the charge point id resolver replaceable. As far as the items above are
concerned: 9.3 and 9.4 are addressed in part; the blocking sends of sections 1
and 2, the callback FIFO, the missing `recover`, the read limit and the
duplicate policy are as in v0.19.0. Code that tracks `master` by
pseudo-version gets those fixes together with the `ws` API change.

## Maintenance status

Facts as of 2026-11-24, from the GitHub API. They are here because they affect
how soon a fix can be expected in a release, which is relevant when deciding
between patching locally and waiting.

| | |
|---|---|
| Latest release | v0.19.0, 2025-02-02 (before it: v0.18.0 on 2024-03-13, v0.17.0 on 2023-09-10) |
| Latest commit on `master` | `b22429c`, 2025-08-17 |
| Open issues / open pull requests | 68 / 54 |
| Pull requests for the stops described in sections 1 and 2 | [#408](https://github.com/lorenzodonini/ocpp-go/pull/408) (opened 2026-03-15), [#412](https://github.com/lorenzodonini/ocpp-go/pull/412) (2026-03-23), [#414](https://github.com/lorenzodonini/ocpp-go/pull/414) (2026-04-07): open, no review and no comment so far |
| Maintenance question | [#431](https://github.com/lorenzodonini/ocpp-go/issues/431), opened 2026-10-17 by a contributor, asks about the project's maintenance and whether co-maintainers would be accepted; 7 comments, none from the repository owner so far. The thread also surveys the active forks |

Several independent forks listed in #431 change the same places (dispatcher
blocking, callback queue, read limit, `context`). That is consistent with the
analysis above, and it also means that fixes exist outside this repository:
gocpp is one option among several.

## Limits of gocpp and of this analysis

**gocpp.**

- **Pre-v1.** gocpp is at v0.3.x; the README says the public API may still
  change before v1.0. The first commit is from 2026-06.
- **One maintainer.** All commits so far come from one person.
- **No production track record** that this page can point to. The evidence
  here is repro programs, test suites and load tests.
- **Not faster on latency or CPU.** With 4,000 charge points and no CSMS calls
  (a condition under which v0.19.0 does not stop), single runs gave:

  | | 30k msg/s: CPU / RSS / Heartbeat p99 | 60k msg/s: CPU / RSS / Heartbeat p99 | allocation per request |
  |---|---|---|---|
  | gocpp v0.3.0 (lenient schema) | 0.98 cores / 124 MB / 1.4 ms | 1.85 / 124 MB / 5.3 ms | 3.5 KB |
  | ocpp-go v0.19.0 | 1.01 cores / 167 MB / 1.4 ms | 1.88 / 168 MB / 5.3 ms | 2.9 KB |

  Throughput, CPU and p99 are the same. gocpp uses less memory per connection
  and, with schema validation on, allocates about 20% more per request. The
  difference this page is about is whether the server keeps serving, not
  speed.
- **The drop-in replaces the server runtime only.** Payload decoding,
  validation, the callback FIFO of the 1.6 / 2.0.1 glue, the package-level
  configuration and the whole client side remain ocpp-go's code. On the
  drop-in, r07, r10, r11 and r13 reproduce exactly as on upstream, and
  r09 is the same code.
- **Up to and including `v0.19.0-gocpp.2` the drop-in misdelivered a response
  after a reconnect when no disconnect handler was registered** (r08-C). Later
  releases clear the callbacks of a disconnected charge point by default.
- **The drop-in's defaults follow upstream**: first-wins duplicates, no frame
  size limit, idle connections dropped without a close frame. r05 and r06
  reproduce on it until the options are set.
- **An injected `ServerDispatcher` selects ocpp-go's original dispatcher**, and
  with it section 1.
- **The drop-in is young.** A response-ordering bug in its own handling of the
  callback FIFO was found and fixed on 2026-11-24 (gocpp pull requests
  [#102](https://github.com/shiv3/gocpp/pull/102) and
  [#103](https://github.com/shiv3/gocpp/pull/103), included in
  `v0.19.0-gocpp.2`).
- **Native gocpp behaves differently from ocpp-go on the wire** in a number of
  places (error codes, leniency, timestamps);
  [migration-from-ocpp-go.md](migration-from-ocpp-go.md) lists them.
- gocpp needs Go 1.26 or later; ocpp-go declares `go 1.16`.

**This analysis.**

- **Loopback, single runs.** The load-test figures come from one machine,
  loopback, plain `ws://` without TLS, one run per configuration, with other
  work on the machine. They show whether a server stops; they are not a
  benchmark.
- **OCPP 1.6 only for load tests.** No 2.0.1 load numbers exist for either
  library. The repro programs also use 1.6, except r13.
- **Shortened timeouts.** r02, r03 and r08 shorten the call or write timeout
  (stated in each program and its output) so that a run takes seconds. With
  the defaults (30 s and 10 s) the same thing takes correspondingly longer.
- **Races are probabilistic.** r02's split between stop and crash and r08-B's
  count vary per run. r01 was deterministic in the runs made.
- **r03 has a narrow precondition** (unread data beyond the socket buffers); it
  is less likely in normal operation than section 1.
- **Native gocpp was exercised by one repro only** (r07 native, numbers and
  handler panic). The other statements about native gocpp rest on its
  documentation, code and test suite.
- **Things checked and found not to be problems in v0.19.0:**
  - A CALLRESULT that fails validation is **not** misdelivered; it is reported
    to the application as a timeout (r08-A).
  - There is **no int32 overflow**: the id fields are Go `int` (64-bit on
    64-bit platforms). The actual number issue is the `float64` step
    (section 7).
  - The package variable `ocppj.FormationViolation`, which earlier versions
    rewrote per OCPP version, is gone; v0.19.0 selects the error code per
    endpoint (`Dialect`). The remaining globals are the ones in section 10.
  - An ordinary lifecycle (connect, requests both ways, disconnect, `Stop`)
    showed **no data race** under `-race` (r10). The race found needs a
    setter called while another server runs.
  - The client `send on closed channel` could not be reproduced (r11-B).

## If you use the drop-in

The [drop-in module](../compat/ocppgo/README.md) removes sections 1, 2, 4 and
the race of section 3 (and, from the release after `v0.19.0-gocpp.2`, its reconnect case) without code changes. Use `compat/ocppgo` at
`v0.19.0-gocpp.x`: that line tracks upstream v0.19.0
([versions](../compat/ocppgo/README.md#versions)). Older tags on the Go proxy,
`compat/ocppgo/v0.17.2-gocpp.1` and `compat/ocppgo-v019/*`, are deprecated and
unmaintained. To also cover what its
upstream-compatible defaults leave open, do the following before `Start`:

- [ ] On `v0.19.0-gocpp.2` and earlier: **always register the disconnect
      handler** (`SetChargePointDisconnectedHandler` on 1.6,
      `SetChargingStationDisconnectedHandler` on 2.0.1), even if it does
      nothing. Without it, callbacks of requests pending at a disconnect are
      not cleared and receive the first response after the reconnect (r08-C).
      Later releases do this by default.
- [ ] `compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)` so that a
      charge point reconnecting after a half-open drop is served at once (r06).
- [ ] `compat.SetMaxMessageSize(srv, 1<<20)`, or the largest frame you expect
      (r05).
- [ ] **Do not inject a `ServerDispatcher`.** Build the server with
      `ocpp16.NewCentralSystem(nil, srv)` or `ocppj.NewServer(srv, nil, nil, …)`
      and set the call timeout with `compat.SetCallTimeout`;
      `compat.Runtime(endpoint)` reports which runtime is in use.
- [ ] **Coming from a fork of ocpp-go?** Several forks changed duplicate
      handling to last-wins and added inbound size limits. The drop-in's
      defaults are upstream v0.19.0's (first-wins, no limit), so moving to
      them silently reverses both. To keep the behaviour you had, set
      `compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)` and
      `compat.SetMaxMessageSize(srv, n)` (above), and check whether you also
      relied on other changes your fork made; the drop-in has only upstream's
      API and the options listed in its README.
- [ ] **If you wrap `ws.WsServer` yourself** (your own type around the
      module's `*ws.Server`), the module runs in bridge mode and does not own
      the sockets: call every `compat.Set*` that takes a `*ws.Server` on the
      inner server, and expect about 3 goroutines per connection instead of 1
      (section 12).
- [ ] Review `PingWait` against your chargers' ping and Heartbeat behaviour;
      see "Idle timeout" under
      [Known differences](../compat/ocppgo/README.md#known-differences).
- [ ] Remember what stays as it is: numbers above 2^53 (section 7), the
      `validate` tags (section 9; per-server leniency is available), the
      package-level settings (section 10), and ocpp-go's client (section 13).

```go
srv := ws.NewServer()
compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins)
compat.SetMaxMessageSize(srv, 1<<20)

endpoint := ocppj.NewServer(srv, nil, nil, core.Profile /* , ... */)
compat.SetCallTimeout(endpoint, 30*time.Second)

cs := ocpp16.NewCentralSystem(endpoint, srv)
cs.SetChargePointDisconnectedHandler(func(cp ocpp16.ChargePointConnection) {
	// required even when empty: this is what clears pending callbacks
})
```

## Reproducing

```sh
cd interop/ocppgo-repro
./run.sh r01              # upstream ocpp-go v0.19.0
./run.sh r01 compat       # the same source on the gocpp drop-in (go.compat.mod: one replace line)
./run.sh r05 compat-safe  # drop-in with last-wins, 1 MiB limit, close frame on idle
./run.sh r07 native       # gocpp native API
./run.sh all              # everything against upstream; "all compat" for the drop-in
```

[`interop/ocppgo-repro/README.md`](../interop/ocppgo-repro/README.md) has the
list of programs, their run times and what each output means. The directory is
a separate Go module and is not built or tested by this repository's CI.
