# gocpp footprint/latency work: final acceptance report

Date: 2026-11-21. gocpp is `origin/main` at 6ced030 (footprint stages 0-11, PRs #23-#36); ocpp-go is the `v0.17.2-fix.3` fork; same harness, fleet and parameters as the 2026-11-20 report (`../2026-11-20/report.md`), except for the rig below.

## Rig

- One WSL2 machine, loopback. Server pinned to 4 cores (`taskset -c 0-3`, GOMAXPROCS=4), load generator pinned to 4 other cores (`taskset -c 4-7`, GOMAXPROCS=4): 8 cores in total (the 2026-11-20 rig used 4 + 6). The generator reached the target rates (30k and 59.5k msg/s) with this budget. Runs were serial; load average was checked before each (< 1 for the soaks, 3.7-5.7 on the knee runs because of unrelated background jobs on other cores).
- Soak: 4,000 chargers, 40% mute, 3 flappers, 20 duplicate ids, 15% transactions, 50 CSMS calls/s, 30 s call timeout, 25 min, 5 SIGTERM restarts. Variants: gocpp default (lenient), `-compat`, `-handler-pool 64` (stage 11), ocpp-go fork.
- Knee: `-knee` (4,000 chargers, timers sped up 160x then 320x, 150 s per step, `-gctrace`; the preset now implies `-restarts 0`, because restarts inside the first step diluted its rate), run for gocpp lenient, `-schema off`, `-handler-pool 64`, ocpp-go, and gocpp / ocpp-go at `GOGC=300`. Rates in the tables are from the second half of each step.
- Idle: 10,000 raw-TCP OCPP websocket connections (`idle/`), server 4 cores, client 4 cores. See `idle.txt`.
- Latency percentiles are log-bucket upper bounds (12% resolution); every row is one run, so differences of a bucket or two are noise.

## Final acceptance

| metric | target | 2026-11-20 | measured | |
|---|---|---|---|---|
| resident goroutines per connection | 1 | 8 | 1.00 idle (10,000 conns: 10,004 goroutines); 1.31 in the soak (handlers and call timers in flight) | pass |
| idle footprint per connection, heap + stack | <= 9 + 5 KB | 34 + 27 KB | 7.8 + 4.1 KB (`BenchmarkIdleFootprint`, 2,000 conns); 7.6 KB heap at 10,000 | pass |
| idle RSS per connection | <= 15 KB | 61 KB | 13.6 KB (151.4 MB at 10,000 minus 15.7 MB process base); 15.1 KB if the base is counted. ocpp-go: 22.7 KB | pass |
| 4,000 chargers, 25 min soak: max RSS | <= 170 MB | 501 MB | 154 MB (compat 149, pool 158; ocpp-go 212) | pass |
| 4,000 chargers, 25 min soak: max goroutines | <= 5,500 | 33,326 | 5,323 (compat 5,318, pool 5,373; ocpp-go 9,972) | pass |
| 10,000 idle connections RSS | <= 170 MB | about 610 MB | 151 MB (ocpp-go 240 MB). After a Heartbeat from each: 163 MB with the pool, 191 MB without (transient garbage, not yet collected) | pass |
| allocation per request at 30k req/s, lenient | <= 3.5 KB | 7.6 KB | 3.54 KB at 30k, 3.48 KB at 59.6k (ocpp-go 2.94 / 2.90) | marginal miss at 30k (1%) |
| allocation per request at 30k req/s, schema off | <= 1.5 KB | 3.3 KB | 1.39 KB (1.37 at 59.7k) | pass |
| `BenchmarkDispatchRoundTrip/schema=off` | <= 16 allocs, <= 1.0 KB | 42 allocs, 1.9 KB | 14 allocs, 704 B (Heartbeat), 14 allocs, 696 B (StatusNotification); measured on 2 cores next to the running soak, so ns/op is not comparable, allocations are exact | pass |
| Heartbeat p99 at 30k req/s, GOGC=100, lenient | <= ocpp-go | 18 ms (ocpp-go 1.4) | 1.7 ms (ocpp-go 2.4) | pass |
| Heartbeat p99 at 30k req/s, GOGC=100, schema off | <= 0.6 x ocpp-go | n/a | 0.8 ms (0.6 x 2.4 = 1.4) | pass |
| Heartbeat p99 at 60k req/s, lenient | <= ocpp-go | 29 ms (ocpp-go 6.6) | 7.4 ms (ocpp-go 9.3) | pass |
| Heartbeat p99 at 30k req/s, GOGC=300 | <= 2 ms, RSS <= 400 MB | n/a | 1.0 ms, 264 MB (ocpp-go: 0.9 ms, 364 MB) | pass |
| freezes, restart recovery t99, call outcome mix | unchanged | baseline | no freeze, no crash; t99 6.0-6.8 s from healthy in every run (baseline 6.0-7.0); call outcomes within 0.1% of each other and of the baseline (ok 59.3%, timeout 35.6%) | pass |

Notes on the one marginal miss: lenient allocation per request is 3.54 KB at 30k and 3.48 KB at 60k msg/s (the per-request share of fixed costs shrinks with rate); the stage-9 measurement that set the 3.5 KB target was taken at about 60k. Per-request allocation is 20% above ocpp-go's (2.94 KB) in lenient mode and 53% below it with `-schema off`.

## Findings

1. Everything the design set out to fix is fixed. Soak memory fell from 501 MB to 154 MB (-69%) and goroutines from 33,326 to 5,323 (-84%); gocpp now uses 27% less memory and 47% fewer goroutines than the ocpp-go fork for the same fleet (212 MB / 9,972). At 10,000 idle connections it uses 151 MB against 240 MB.
2. The knee is gone for the tail: at 30k msg/s gocpp Heartbeat p99 is 1.7 ms (was 18 ms) and below ocpp-go's 2.4 ms in the same series; at 60k it is 7.4 ms against 9.3 ms. gocpp uses slightly more CPU than ocpp-go at 30k (1.16 vs 1.04 cores) and at 60k (2.11 vs 1.89) with lenient validation; with `-schema off` it is clearly cheaper (0.81 / 1.52 cores).
3. GOGC=300 keeps p99 at about 1 ms at 30k and about 4 ms at 60k for both libraries; gocpp pays 113 MB (151 -> 264 MB), ocpp-go 153 MB (211 -> 364 MB).
4. The handler pool (64 workers) lowers CPU by 22% at 30k (0.91 vs 1.16 cores) and 18% at 60k (1.72 vs 2.11) with the same RSS and no p99 change (1.9 vs 1.7 ms, 7.4 vs 7.4 ms: within bucket noise). In the low-rate soak it makes no difference (154 vs 158 MB, same latencies).
5. Soak behaviour is unchanged from 2026-11-20: no freeze, no crash, no leak (RSS slope within +-3 MB/min, goroutines per connection flat), 99% reconnect within 6.0-6.8 s of healthy after each restart, identical call-outcome mix. In steady state the three gocpp variants and ocpp-go are indistinguishable (Heartbeat p50/p95/p99 0.2 / 0.5 / 0.8 ms).
6. The first knee attempt (with five restarts inside the first step) is not reported: its first step averaged only 13.7k msg/s because of the restarts. The preset was fixed and every knee run repeated.

