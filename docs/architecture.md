# gocpp architecture

## Layering

```
csms / cp                 generics On/Call API, options, handshake
   │
core/dispatcher           version-agnostic Conn lifecycle, pending calls, direction
   │
core/ocppj                OCPP-J framing (Call/CallResult/CallError), msg IDs, errors
   │
core/transport            WS interface (coder/websocket impl, FakeWS for tests)
```

Per-version generated code (`v16`, `v201`, `v21`) sits beside the API layer and depends
only on `core/ocppj` (message values) and `core/schema` (embedded schemas). The
dispatcher is **version-agnostic**: adding a version added zero dispatcher code — the
key §1 boundary, validated across 1.6/2.0.1/2.1.

## Connection lifecycle

`dispatcher.Conn` is bound to a `context.WithCancelCause` and has one resident
goroutine, the **reader**:

- the **reader** reads frames, routes CallResult/CallError to the pending store and
  starts a short-lived goroutine (or hands the frame to a resident `HandlerPool`
  worker) for each inbound Call. When every handler slot is busy it parks the frame in
  a small bounded queue (`InboundQueueSize`) that finishing handlers drain, and only
  stops reading when that queue is full.
- **writes** happen on the goroutine that produced the frame (a handler or a `Call`
  caller) under a per-connection FIFO lock; there is no writer goroutine. A lazily
  armed timer enforces `WriteTimeout` and aborts the transport when a write stalls.
- **teardown** is a `context.AfterFunc` callback on the connection context; it runs
  once, whichever side ends the connection.
- the read-idle watchdog and WebSocket ping are `time.AfterFunc` timers, not goroutines.

Teardown flows from a single `cancel(cause)`; senders check `ctx.Done()` before writing
and while waiting for a result, so there is no write deadlock and no channel is closed
under a sender (no send-on-closed panic). Handlers run with panic recovery: a panicking
handler replies `InternalError` and the connection survives. See
[performance.md](performance.md) for the cost model.

## Code generation

`internal/codegen` reads the official JSON schemas (`schemas/<ver>/`) plus a profile YAML
(`internal/codegen/profiles/<ver>.yaml`) and emits:

- `<ver>/messages/*.go` — one file per message; shared nested types deduped; enums with
  `Valid()`; `$ref`/`definitions` resolved (2.x).
- `<ver>/profiles/*.go` — `ocppj.Message` vars + `RegisterSchemas`.
- `<ver>/schemas/*.json` + `embed.go` — embedded for runtime validation, with optional
  JSON-merge-patch overrides from `schemas/overrides/<ver>/`.

Output is deterministic: `make codegen && git diff --exit-code` must stay clean.

## Pluggability (Phase 4)

`core/storage`, `core/auth`, `core/observability` define interfaces with in-memory / NoOp
defaults, wired into the CSMS via options. Real adapters (Redis/NATS/Postgres) are
post-v1.0; the interfaces are the frozen candidates.
