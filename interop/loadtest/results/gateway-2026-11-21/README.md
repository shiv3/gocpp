# Gateway split vs embedded: load-test results (2026-11-21)

Harness: this directory's `loadtest` with the split topology (`-gateways`,
`-backend-pools`), 4,000 simulated OCPP 1.6J chargers (40% mute, 3 flappers,
20 duplicate ids, 15% doing transactions, compressed timers), loopback on one
WSL2 host (20 vCPUs, shared with other jobs; load average about 1.5 during the runs).
Gateway = `cmd/gocpp-gateway` (schema off, Redis registry = in-process miniredis),
backend = `interop/parity/cs-gocpp -mode backend` (lenient/schema off, `-lite`),
link = gRPC over loopback. The load generator is pinned to cpus 14-19.

CPU pinning: embedded server on cpus 8-13 (6 cores); split gateway on 8-9
(2 cores), 2 backends on 10-13 (2 cores each). `msgs/s` is requests completed by
the chargers; `cores` is the mean CPU of the process(es) over the steady part of
the step (`/proc` ticks), so cores per tier are directly comparable.

## Rate ladder (A vs B: speed 1, 10, 40; F: speed 1, 100, 200)

| run | step | msgs/s | server / gateway cores | backend cores (2 procs) | Heartbeat p50 / p99 ms |
|---|---|---|---|---|---|
| embedded | speed 1 | 322 | 0.10 | | 0.20 / 0.55 |
| split | speed 1 | 322 | 0.14 | 0.11 | 0.39 / 0.96 |
| embedded | speed 10 | 1,987 | 0.25 | | 0.16 / 0.44 |
| split | speed 10 | 1,987 | 0.39 | 0.38 | 0.35 / 0.86 |
| embedded | speed 40 | 7,663 | 0.65 | | 0.20 / 0.49 |
| split | speed 40 | 7,661 | 0.77 | 0.84 | 0.35 / 0.96 |
| embedded | speed 100 | 18,931 | 1.20 | | 0.20 / 0.77 |
| split | speed 100 | 18,765 | 1.31 | 1.50 | 0.44 / 25.8 (gateway on 2 cores) |
| embedded | speed 200 | 37,655 | 2.42 | | 0.20 / 5.3 |
| split | speed 200 | 36,299 | 1.60 | 1.70 | 0.69 / 25.8 (gateway on 2 cores) |

Reading: at 7.7k msg/s the split adds about 0.15 ms at p50 and 0.5 ms at p99
(targets 0.5 / 2 ms at 5k msg/s); the gateway handles about 22,700 msg/s per
core at 36k msg/s (target >= 20,000), but it was pinned to 2 cores and ran at 80%,
which is where the p99 of the two top steps comes from (queueing, not the hop).
Total CPU of the split is about 1.4x the embedded server (3.3 vs 2.4 cores at
37k msg/s): the cost of the link is about 25 us of CPU per message over both ends.

## Idle footprint, 10,000 sessions (G)

| | RSS |
|---|---|
| embedded server | 263 MB (26 KB/session) |
| split gateway | 253 MB (25 KB/session) |
| split backends (2 procs, stateless) | 69 MB (the process baseline) |

The gateway does not cost more per session than an embedded server (-4%), but
both are above the 16 KB/session target and above docs/performance.md's 13.6 KB
(that figure is in-process heap of a footprint benchmark, this one is whole-process
RSS including GC headroom and the registry bookkeeping). Not investigated further here.

## Rollouts (4,000 chargers, 6 / 4 / 6 minutes)

| scenario | result |
|---|---|
| C: backend SIGTERM x5 (drain: Nack + redispatch) | 0 disconnects (min connected 3,999 of 4,000 + 2 canaries), 0 CALLERRORs, chargers_up never below 3,985 (the 20 duplicate-id shadows), restart windows Heartbeat p99 0.96 ms |
| D: backend kill -9 x3 | same; the tiny handlers were not caught mid-flight (the mid-handler case is in the chaos tests: one InternalError, no retry, no disconnect) |
| E: gateway SIGTERM x2 (2 gateways, 20 s paced drain, 1012) | the drained gateway's ~2,000 chargers reconnect through the other one: chargers_up minimum 3,639 and 3,268 (the reconnect dip), back to 99% within 1 s of the pod being ready again; 5,173 connect attempts answered 503 and 946 refused during the two drains (the harness chargers ignore Retry-After) |
| freezes | none in any run |

## Profiles (30 s, speed 200)

`profiles/F-split-high.gw.cpu.pprof` (gateway), `profiles/F-split-high.be.cpu.pprof`
(backend), `profiles/F-embedded-high.cpu.pprof`. Gateway: 27% of CPU is
`write(2)` (WebSocket reply write 16%, gRPC stream write 15.7%); the gRPC hop is
`peer.Handle` at 21% cumulative, almost all of it the send syscall. The obvious
next steps are coalescing writes of the link stream and a second stream per
backend; protobuf marshalling is below 3%. Backend: gRPC transport (reader,
loopy writer, flush) about 45%, the handler path (`backendUpstream.Handle`) 23%,
of which `codec.Unmarshal` 10%.
