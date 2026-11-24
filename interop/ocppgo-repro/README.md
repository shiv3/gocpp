# ocpp-go v0.19.0 repro programs

Small programs behind
[docs/ocpp-go-structural-issues.md](../../docs/ocpp-go-structural-issues.md).
Each one shows a single behaviour of `github.com/lorenzodonini/ocpp-go`
v0.19.0, and the same source can be built against the gocpp drop-in module to
compare.

This directory is its own Go module (three, counting `go.compat.mod` and
`native/`). It is not part of the root module, and the repository's CI does not
build or run it: several programs end in a deliberate crash or deadlock.

## Running

```sh
./run.sh r01              # upstream ocpp-go v0.19.0            (go.mod)
./run.sh r01 compat       # same source on the gocpp drop-in    (go.compat.mod: one replace line; needs Go 1.26+)
./run.sh r05 compat-safe  # drop-in with its opt-in settings: last-wins duplicates, 1 MiB limit, close frame on idle
./run.sh r07 native       # gocpp native API                    (native/, r07 only)
./run.sh all              # every program against upstream; "./run.sh all compat" for the drop-in
```

Output goes to the terminal and to `out/<name>.<variant>.txt`. Every program
ends with a `RESULT [...]: REPRODUCED` or `NOT REPRODUCED` line, except where a
process crash is the expected result (r02 sometimes, r08 on upstream).

The server side is built with ocpp-go's public API (`ocpp16.NewCentralSystem`),
as its README shows. The charge points are plain `gorilla/websocket` clients
that follow a script (`h/h.go`). Where a timeout is shortened to keep a run
short, the program's header comment and its output say so (the library
defaults are 30 s for calls and 10 s for writes).

## Programs

| Program | What it shows | upstream v0.19.0 | drop-in | Run time |
|---|---|---|---|---|
| `r01_dispatcher_freeze` | CSMS sends one call to each of 200 healthy charge points in a loop; the send blocks and no charge point is answered any more | reproduced (10/10, `out/…upstream.tally.txt`) | not reproduced | 20 s |
| `r02_timeout_selfdeadlock` | call timeouts of 300 mute charge points while 8 healthy ones answer: the dispatcher stops, or the process exits with a nil dereference | reproduced (`out/…upstream.tally.txt`, `…crash-run.txt`) | not reproduced | 25 s |
| `r03_stalled_reader_freeze` | one charge point that stops reading its socket stops all writes and new connections after the write timeout | reproduced | not reproduced | 15 s |
| `r04_handler_panic` | a panic in one handler ends the server process | reproduced | not reproduced (`InternalError`) | 2 s |
| `r05_unbounded_frame` | one 64 MiB frame is accepted; peak RSS grows by about 350 MiB | reproduced | reproduced by default; `compat-safe`: close 1009 | 5 s |
| `r06_duplicate_lockout` | a charge point reconnecting after a half-open drop is refused for 60 s | reproduced | reproduced by default; `compat-safe`: served after 5 s | 65 s |
| `r07_number_precision` | 64-bit `transactionId` values are changed silently or rejected | reproduced | reproduced | 1 s |
| `r08_callback_fifo` | A: an invalid answer surfaces as a timeout. B: an answer racing the timeout goes to another request's callback. C: without a disconnect handler, a stale callback gets the first answer after a reconnect. C2: the same through the typed API panics | A as described; B and C reproduced; C2 ends the process | B not reproduced; C reproduced up to `v0.19.0-gocpp.2`, fixed by default after it; C2 does not end the process | 15 s |
| `r09_decode_bench` | cost of ocpp-go's inbound decode path against one typed decode (`go test -bench`) | 1.3x to 2.2x | same code | 20 s |
| `r10_globals` | settings are process-wide; `SetMessageValidation(false)` leaves inbound validation on; `SetLogger` races with a running server | reproduced | reproduced | 10 s |
| `r11_client_crash` | client: A: `Stop` twice panics. B: sending while connections drop (no crash; the client stalls while drops continue: `-flap-for 12s` with `-call-timeout 30s` or `3s` shows recovery after one call timeout) | A reproduced; B no crash | A fixed by default after `v0.19.0-gocpp.2` (reproduced up to it); B: no crash, a request lost at a drop still waits for the call timeout (ocpp-go's design) | 50 s |
| `r12_per_connection` | goroutines per idle server connection | 2.00 | 1.00; with `-bridge` (app-side `WsServer` wrapper, 1,000 connections) 3.00 (upstream with the wrapper: 2.00) | 5 s |
| `r13_tag_drift` | three messages that are valid against the OCA schemas are rejected by the struct tags | reproduced (3/3) | reproduced (3/3) | 1 s |

`native/r07` sends the same numbers to a gocpp server built with the native API
and then makes one handler panic.

## Stored outputs

`out/` holds the outputs of the runs the document quotes (2026-11-24, Go 1.27,
linux/amd64, loopback). Three edits were made to them: the module cache prefix in
stack traces is written as `$GOMODCACHE/`, the path of this directory as `./`,
and the module name as it is in this repository. Races give different counts on each run (r02, r08 part B); the tallies
record ten consecutive runs.

## Versions

- `go.mod`: `github.com/lorenzodonini/ocpp-go v0.19.0`.
- `go.compat.mod`: the same, with
  `replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.2`.
  Files tagged `//go:build compat` in `h/` use the extra `compat` package.
- `native/go.mod`: `github.com/shiv3/gocpp v0.3.3`.
