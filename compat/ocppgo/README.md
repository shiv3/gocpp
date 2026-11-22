# gocpp ocpp-go compatibility module

`github.com/shiv3/gocpp/compat/ocppgo` has the public API of
`github.com/lorenzodonini/ocpp-go` (the `v0.17.2-fix.x` line) and runs on the
gocpp runtime. An application switches to it with one line in its `go.mod`:

```
replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.17.2-gocpp.N
```

Status: Phase 1 (server side). See `NOTICE` for what is upstream code and what is
new, and `UPSTREAM` for the pinned source.

## What runs on gocpp

- `ocppj.Server` serves every connection on gocpp's per-connection dispatcher:
  frame parsing, handler goroutines with panic recovery (an application handler
  that panics is answered with `InternalError` and the process keeps running),
  one FIFO slot per charge point for outbound calls, call timeouts, teardown.
  Payload validation, error codes and descriptions are still ocpp-go's own code.
- `ws.Server` is implemented on `coder/websocket` with the exact API of ocpp-go
  (gorilla types stay in the signatures, for example
  `StopConnection(id, websocket.CloseError)`). Close codes are sent as close
  frames; `Stop` closes with 1000.
- The client side (`ws.Client`, `ocppj.Client`, `ocpp1.6.ChargePoint`, ...) is
  ocpp-go's unchanged code.

## Choosing the runtime

| How the server is built | Runtime |
|---|---|
| `ocpp16.NewCentralSystem(nil, ws)`, `ocppj.NewServer(ws, nil, nil)` | gocpp |
| the application passes its own `ServerDispatcher` | ocpp-go's original code |

`OCPPGO_COMPAT_RUNTIME=legacy` forces ocpp-go's original code path everywhere
(operational fallback); `=gocpp` also routes an injected
`*ocppj.DefaultServerDispatcher` to gocpp (used by CI for ocpp-go's own suites).
The package `compat` reports the runtime of a server and sets the call timeout
without injecting a dispatcher.

## Known differences

- Outbound requests are ordered per charge point, not globally.
- A panic in an application handler or response callback is contained.
- Requests pending at a disconnect are reported once (by the 1.6 / 2.0.1 glue's
  disconnect handler), as in ocpp-go.
- `SendResponse` / `SendError` return a hand-over error; write errors surface in
  the logs.
- Idle connections are closed with a close frame (1000) on the gocpp runtime.
- New upgrades are refused with HTTP 503 once `Stop` has begun.
- ocpp-go's package level state (`ocppj.FormationViolation`, loggers, ...) is
  unchanged, including its data races when several endpoints start at once.

## Maintenance

- `scripts/sync-upstream.sh [--check]` re-creates the upstream files from the
  pinned tag and `patches/`; `scripts/refresh-patches.sh` regenerates the patches.
- `scripts/apidiff.sh` fails on any difference in the exported API of any
  package against the pinned tag.
- The nested module is developed against the repository root through
  `replace github.com/shiv3/gocpp => ../..`; consumers get the version in `require`.
