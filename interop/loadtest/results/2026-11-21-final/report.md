# gocpp vs ocpp-go large-fleet load/soak report

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


## Side-by-side: 4,000 chargers, 40% mute, 25 min, 5 restarts

Latency columns are p50 / p95 / p99 in ms (histogram bucket upper bounds, 12% resolution). Steady = windows that are not within a restart recovery; restart = 10 s windows overlapping a restart recovery.

| server | Boot steady | Heartbeat steady | MeterValues steady | Boot restart | Heartbeat restart | MeterValues restart |
|---|---|---|---|---|---|---|
| knee-gocpp-lenient | 0.1 / 0.3 / 0.7 | 0.1 / 1.1 / 6.6 | 0.2 / 1.5 / 7.4 | - / - / - | - / - / - | - / - / - |
| knee-gocpp-lenient-gogc300 | 0.1 / 0.3 / 0.6 | 0.1 / 0.5 / 3.4 | 0.2 / 1.0 / 3.4 | - / - / - | - / - / - | - / - / - |
| knee-gocpp-pool64 | 0.1 / 0.3 / 0.5 | 0.1 / 1.2 / 6.6 | 0.2 / 1.4 / 7.4 | - / - / - | - / - / - | - / - / - |
| knee-gocpp-schemaoff | 0.1 / 0.3 / 0.5 | 0.1 / 0.4 / 4.2 | 0.2 / 0.8 / 4.7 | - / - / - | - / - / - | - / - / - |
| knee-ocppgo | 0.1 / 0.3 / 0.7 | 0.1 / 0.8 / 7.4 | 0.2 / 1.1 / 9.3 | - / - / - | - / - / - | - / - / - |
| knee-ocppgo-gogc300 | 0.1 / 0.3 / 0.6 | 0.1 / 0.4 / 2.7 | 0.2 / 0.9 / 2.4 | - / - / - | - / - / - | - / - / - |
| soak-gocpp-compat | 0.2 / 0.5 / 0.8 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 0.9 | 0.1 / 0.4 / 1.4 | 0.2 / 0.6 / 0.9 | 0.3 / 0.7 / 1.0 |
| soak-gocpp-default | 0.2 / 0.5 / 0.8 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 0.9 | 0.1 / 0.5 / 1.5 | 0.2 / 0.5 / 0.8 | 0.3 / 0.7 / 1.0 |
| soak-gocpp-pool64 | 0.2 / 0.5 / 0.8 | 0.2 / 0.5 / 0.7 | 0.2 / 0.5 / 0.8 | 0.1 / 0.5 / 1.7 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 0.9 |
| soak-ocppgo | 0.2 / 0.5 / 0.8 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 0.9 | 0.1 / 0.6 / 1.9 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 0.9 |

| server | freeze | crashes | median up | max RSS MB | max goroutines | mean CPU cores | t99 per restart (s from healthy) | CSMS call outcomes |
|---|---|---|---|---|---|---|---|---|
| knee-gocpp-lenient | no | 0 | 3990 | 151 | 5315 | 1.17 |  | not_connected:4, ok:8847, timeout:5545 |
| knee-gocpp-lenient-gogc300 | no | 0 | 3990 | 270 | 5304 | 1.12 |  | not_connected:5, ok:8858, timeout:5535 |
| knee-gocpp-pool64 | no | 0 | 3991 | 152 | 5372 | 0.92 |  | not_connected:4, ok:8851, timeout:5543 |
| knee-gocpp-schemaoff | no | 0 | 3989 | 146 | 5309 | 0.82 |  | not_connected:5, ok:8838, timeout:5554 |
| knee-ocppgo | no | 0 | 3992 | 213 | 10016 | 1.05 |  | not_connected:5, ok:8850, timeout:5543 |
| knee-ocppgo-gogc300 | no | 0 | 3991 | 368 | 10044 | 1.01 |  | not_connected:5, ok:8841, timeout:5554 |
| soak-gocpp-compat | no | 0 | 3989 | 149 | 5318 | 0.10 | 6.5, 6.8, 6.8, 6.3, 6.0 | http_eof:3019, http_refused:500, http_reset:6, not_connected:287, ok:44103, timeout:26477 |
| soak-gocpp-default | no | 0 | 3989 | 154 | 5323 | 0.10 | 6.3, 6.5, 6.8, 6.3, 6.0 | http_eof:3018, http_refused:501, http_reset:6, not_connected:287, ok:44077, timeout:26503 |
| soak-gocpp-pool64 | no | 0 | 3989 | 158 | 5373 | 0.10 | 6.3, 6.5, 6.8, 6.3, 6.0 | http_eof:3015, http_refused:504, http_reset:5, not_connected:284, ok:44085, timeout:26499 |
| soak-ocppgo | no | 0 | 3989 | 212 | 9972 | 0.09 | 6.3, 6.8, 6.8, 6.3, 5.8 | http_eof:3018, http_refused:502, http_reset:6, not_connected:284, ok:44075, timeout:26507 |

### knee-gocpp-lenient

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 5.0 min; chargers up (client side, after ramp) median 3990
- Server RSS MB: min 149, max 151; CPU cores mean 1.17, max 1.20; threads max 10; fds max 4675; goroutines max 5315; generator RSS max 256 MB
- Canary heartbeat max RTT ms (steady): 1.2; /control/health ms max (steady): 1.1
- CSMS calls: not_connected=4, ok=8847, timeout=5545
  - shares: not_connected 0.0%, ok 61.5%, timeout 38.5%
  - call latency ms (p50/p99): not_connected 0.1/0.3, ok 0.3/7.4, timeout 30020.7/30020.7
- Charger request errors: {'BootNotification/closed': 19, 'StatusNotification/write': 1}; connect errors: {}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up           ▁▂▂▃▄▅▅▆▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▁  (min 317, max 3994)
rss MB       ▁▂▃▄▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 26.4, max 151.4)
goroutines  ▁▂▃▄▅▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 0, max 5315)
cpu cores      ▁▁▁▂▂▂▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 0, max 2.17)
```

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 6 | 150 | 151 | +0.4 | +59 | 1.32 | +2 |

### knee-gocpp-lenient-gogc300

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 5.0 min; chargers up (client side, after ramp) median 3990
- Server RSS MB: min 264, max 270; CPU cores mean 1.12, max 1.15; threads max 9; fds max 4663; goroutines max 5304; generator RSS max 462 MB
- Canary heartbeat max RTT ms (steady): 2.8; /control/health ms max (steady): 0.3
- CSMS calls: not_connected=5, ok=8858, timeout=5535
  - shares: not_connected 0.0%, ok 61.5%, timeout 38.4%
  - call latency ms (p50/p99): not_connected 0.3/0.7, ok 0.3/3.8, timeout 30061.6/30061.6
- Charger request errors: {'BootNotification/closed': 20, 'Heartbeat/closed': 3, 'MeterValues/closed': 4}; connect errors: {}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up           ▁▂▃▃▄▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇   (min 201, max 3994)
rss MB       ▁▂▃▄▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇████████████  (min 28.6, max 269.5)
goroutines  ▁▂▃▄▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 0, max 5304)
cpu cores     ▁▁▁▁▂▂▂▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇  (min 0, max 2.05)
```

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 6 | 264 | 264 | +1.1 | +44 | 1.32 | +2 |

### knee-gocpp-pool64

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 5.0 min; chargers up (client side, after ramp) median 3991
- Server RSS MB: min 151, max 152; CPU cores mean 0.92, max 0.93; threads max 9; fds max 4669; goroutines max 5372; generator RSS max 256 MB
- Canary heartbeat max RTT ms (steady): 0.3; /control/health ms max (steady): 1.0
- CSMS calls: not_connected=4, ok=8851, timeout=5543
  - shares: not_connected 0.0%, ok 61.5%, timeout 38.5%
  - call latency ms (p50/p99): not_connected 0.1/0.2, ok 0.3/7.4, timeout 30019.4/30019.4
- Charger request errors: {'BootNotification/closed': 19, 'MeterValues/closed': 1}; connect errors: {}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up           ▁▂▃▃▄▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 212, max 3994)
rss MB       ▁▂▃▄▅▅▆▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█████████▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 26.9, max 151.7)
goroutines  ▁▂▃▄▅▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 0, max 5372)
cpu cores     ▁▁▁▁▂▂▂▃▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 0, max 1.77)
```

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 6 | 151 | 152 | +1.5 | +51 | 1.34 | +2 |

### knee-gocpp-schemaoff

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 5.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 144, max 146; CPU cores mean 0.82, max 0.83; threads max 9; fds max 4669; goroutines max 5309; generator RSS max 258 MB
- Canary heartbeat max RTT ms (steady): 1.2; /control/health ms max (steady): 0.4
- CSMS calls: not_connected=5, ok=8838, timeout=5554
  - shares: not_connected 0.0%, ok 61.4%, timeout 38.6%
  - call latency ms (p50/p99): not_connected 0.2/0.5, ok 0.3/3.4, timeout 30060.3/30060.3
- Charger request errors: {'BootNotification/closed': 19, 'StatusNotification/closed': 1}; connect errors: {}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▁▁▂▃▃▄▅▅▆▇█▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 153, max 3993)
rss MB       ▁▂▃▄▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇██████████████████████████████  (min 23.3, max 145.5)
goroutines  ▁▂▃▄▅▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 0, max 5309)
cpu cores     ▁▁▁▁▂▂▂▂▃▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇▇▇█▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 0, max 1.57)
```

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 6 | 144 | 144 | +0.0 | +46 | 1.32 | +2 |

### knee-ocppgo

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 5.0 min; chargers up (client side, after ramp) median 3992
- Server RSS MB: min 211, max 213; CPU cores mean 1.05, max 1.06; threads max 9; fds max 4663; goroutines max 10016; generator RSS max 254 MB
- Canary heartbeat max RTT ms (steady): 0.6; /control/health ms max (steady): 0.4
- CSMS calls: not_connected=5, ok=8850, timeout=5543
  - shares: not_connected 0.0%, ok 61.5%, timeout 38.5%
  - call latency ms (p50/p99): not_connected 0.2/0.4, ok 0.3/9.3, timeout 30021.4/30021.4
- Charger request errors: {'BootNotification/closed': 19, 'StatusNotification/write': 1}; connect errors: {}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▁▁▂▃▃▄▅▅▆▇█▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 121, max 3995)
rss MB       ▁▂▃▄▄▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇███████  (min 26.9, max 213.1)
goroutines  ▁▂▃▃▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   (min 0, max 1.002e+04)
cpu cores     ▁▁▁▁▂▂▂▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 0, max 1.92)
```

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 6 | 211 | 211 | +0.0 | +35 | 2.48 | +2 |

### knee-ocppgo-gogc300

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 5.0 min; chargers up (client side, after ramp) median 3991
- Server RSS MB: min 364, max 368; CPU cores mean 1.01, max 1.03; threads max 9; fds max 4673; goroutines max 10044; generator RSS max 463 MB
- Canary heartbeat max RTT ms (steady): 0.3; /control/health ms max (steady): 0.2
- CSMS calls: not_connected=5, ok=8841, timeout=5554
  - shares: not_connected 0.0%, ok 61.4%, timeout 38.6%
  - call latency ms (p50/p99): not_connected 0.2/0.4, ok 0.3/2.7, timeout 30020.5/30020.5
- Charger request errors: {'BootNotification/closed': 20, 'MeterValues/closed': 1}; connect errors: {}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▁▁▂▃▃▄▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█   (min 123, max 3994)
rss MB        ▁▂▃▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇██████████████  (min 28.4, max 368)
goroutines  ▁▂▃▃▄▅▅▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█   (min 0, max 1.004e+04)
cpu cores     ▁▁▁▁▂▂▂▂▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 0, max 1.9)
```

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 6 | 364 | 364 | +0.0 | +50 | 2.48 | +2 |

### soak-gocpp-compat

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 77, max 149; CPU cores mean 0.10, max 0.12; threads max 9; fds max 4678; goroutines max 5318; generator RSS max 258 MB
- Canary heartbeat max RTT ms (steady): 3.8; /control/health ms max (steady): 6.8
- CSMS calls: http_eof=3019, http_refused=500, http_reset=6, not_connected=287, ok=44103, timeout=26477
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.3%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/30010.6, http_refused 0.1/16.4, http_reset 3.4/9.8, not_connected 0.2/1.7, ok 0.5/1.5, timeout 30024.3/30024.3
- Charger request errors: {'BootNotification/closed': 19, 'Heartbeat/closed': 5, 'MeterValues/closed': 3, 'StatusNotification/closed': 1, 'StatusNotification/write': 1}; connect errors: {'refused': 21351, 'reset': 36}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1663, max 3996)
rss MB      ▃▆▇▇▇▇▇▇▇▇▇▄▆▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▅▆▇▇▇▇▇▇▇█▅▇▇▇▇▇▇▇▇  (min 69.5, max 148.8)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 2800, max 5318)
cpu cores  ▃██▁▁▁▁▁▁▁▁ ▁▁▁▁▁▁▁▁▁▃ ▁▁ ▁ ▁▁▁▁ ▁▁▃▁ ▁▁▁▃ ▁▁▁▁▁▁▁▃▁▁▁▁▁▁▃▁▁  (min 0.1, max 0.14)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3991 | 23 | 2.2 | 2.5 | 6.5 | 8.7 | 11.8 | True |
| 2 | 540 | 3986 | 23 | 2.0 | 2.8 | 6.8 | 8.8 | 9.8 | True |
| 3 | 780 | 3986 | 25 | 2.2 | 2.5 | 6.8 | 9.0 | 9.8 | True |
| 4 | 1020 | 3988 | 24 | 2.2 | 2.5 | 6.3 | 8.5 | 7.8 | True |
| 5 | 1260 | 3989 | 25 | 2.2 | 2.5 | 6.0 | 8.2 | 9.8 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 145 | 146 | +0.2 | -11 | 1.32 | -0 |
| 390 | 540 | 29 | 141 | 146 | +1.4 | -17 | 1.31 | +1 |
| 630 | 780 | 29 | 145 | 147 | +1.4 | +9 | 1.32 | +5 |
| 870 | 1020 | 29 | 143 | 144 | +0.7 | -4 | 1.31 | +5 |
| 1110 | 1260 | 29 | 148 | 149 | +0.0 | +22 | 1.31 | +13 |
| 1350 | 1500 | 29 | 143 | 144 | +0.5 | +5 | 1.31 | +0 |

### soak-gocpp-default

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 77, max 154; CPU cores mean 0.10, max 0.15; threads max 9; fds max 4683; goroutines max 5323; generator RSS max 261 MB
- Canary heartbeat max RTT ms (steady): 5.3; /control/health ms max (steady): 3.3
- CSMS calls: http_eof=3018, http_refused=501, http_reset=6, not_connected=287, ok=44077, timeout=26503
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.2%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/30020.4, http_refused 0.1/14.5, http_reset 7.4/11.6, not_connected 0.2/4.7, ok 0.5/1.5, timeout 30050.4/30050.4
- Charger request errors: {'BootNotification/closed': 19, 'Heartbeat/closed': 6, 'MeterValues/closed': 6, 'StatusNotification/write': 1}; connect errors: {'refused': 21311, 'reset': 34}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1659, max 3995)
rss MB      ▃▆▇▇▇▇▇▇▇▇▇▃▅▆▇▇▇▇▇▇▇▄▆▆█▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▄▅▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇  (min 68.7, max 153.9)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 2816, max 5323)
cpu cores  ▃▆█▁▁▁▃▁▃▃▃▁▁ █ █ ▃ ▆▁▁█▁ █ ▃ ▄▁ ▆ ▆▁▄▁▄▁▁▁█ ▆ ▃ ▄▃▁▆ ▆▁▄▃ ▆  (min 0.1, max 0.15)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3992 | 26 | 2.2 | 2.5 | 6.3 | 8.5 | 14.5 | True |
| 2 | 540 | 3987 | 23 | 2.2 | 2.5 | 6.5 | 8.7 | 8.8 | True |
| 3 | 780 | 3987 | 28 | 2.2 | 2.5 | 6.8 | 9.0 | 10.5 | True |
| 4 | 1020 | 3988 | 24 | 2.2 | 2.5 | 6.3 | 8.5 | 7.8 | True |
| 5 | 1260 | 3990 | 28 | 2.2 | 2.5 | 6.0 | 8.2 | 10.0 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 146 | 151 | +1.2 | -11 | 1.32 | +0 |
| 390 | 540 | 29 | 147 | 152 | +2.7 | -17 | 1.31 | +1 |
| 630 | 780 | 29 | 154 | 152 | -0.7 | +9 | 1.32 | +5 |
| 870 | 1020 | 29 | 150 | 152 | +1.5 | -4 | 1.31 | +5 |
| 1110 | 1260 | 29 | 144 | 150 | +1.0 | +22 | 1.31 | +13 |
| 1350 | 1500 | 29 | 147 | 152 | +2.8 | +5 | 1.31 | +0 |

### soak-gocpp-pool64

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 79, max 158; CPU cores mean 0.10, max 0.16; threads max 9; fds max 4668; goroutines max 5373; generator RSS max 262 MB
- Canary heartbeat max RTT ms (steady): 1.3; /control/health ms max (steady): 5.4
- CSMS calls: http_eof=3015, http_refused=504, http_reset=5, not_connected=284, ok=44085, timeout=26499
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.3%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/30005.9, http_refused 0.1/16.1, http_reset 5.3/10.0, not_connected 0.2/8.3, ok 0.5/1.7, timeout 30040.1/30040.1
- Charger request errors: {'BootNotification/closed': 19, 'Heartbeat/closed': 8, 'MeterValues/closed': 5}; connect errors: {'refused': 21347, 'reset': 31}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇██▇▇▇▇█▇▇▇▇▇▇█▇█▇▇▇▇▇██▇█▇▇█▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇  (min 1663, max 3994)
rss MB      ▃▅▇▇▇▇▇▇▇▇▇▃▅▇▇▇▇▇▇▇▇▄▆▇▇▇▇▇▇▇▇▅▆▇▇▇▇▇▇▇▄▆▇▇▇▇▇▇▇▇▄▇▇▇▇█▇▇▇  (min 70.3, max 157.6)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 2864, max 5373)
cpu cores  ▃▃▅▂▂▂▃▂▂▂▁▁▁▁█▁▅▁▃▁▄▂▁▅▂▁▄▁▂▁▄▁▁▅▁▆▂▃▁▄▂▁▁▅ ▆▁▃▁▄▁ ▅▁▅▁▂▄ ▃  (min 0.09, max 0.16)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3990 | 25 | 2.2 | 2.5 | 6.3 | 8.5 | 11.0 | True |
| 2 | 540 | 3987 | 26 | 2.2 | 2.5 | 6.5 | 8.8 | 8.8 | True |
| 3 | 780 | 3989 | 25 | 2.3 | 2.5 | 6.8 | 9.0 | 10.5 | True |
| 4 | 1020 | 3988 | 28 | 2.2 | 2.5 | 6.3 | 8.5 | 9.3 | True |
| 5 | 1260 | 3990 | 26 | 2.2 | 2.5 | 6.0 | 8.3 | 11.5 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 149 | 153 | +0.8 | -11 | 1.33 | +0 |
| 390 | 540 | 29 | 154 | 153 | -0.8 | -17 | 1.33 | +1 |
| 630 | 780 | 29 | 156 | 157 | +0.5 | +9 | 1.33 | +5 |
| 870 | 1020 | 29 | 154 | 153 | +0.0 | -4 | 1.32 | +5 |
| 1110 | 1260 | 29 | 154 | 155 | +0.5 | +22 | 1.33 | +13 |
| 1350 | 1500 | 29 | 153 | 154 | +0.4 | +5 | 1.33 | +0 |

### soak-ocppgo

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 119, max 212; CPU cores mean 0.09, max 0.12; threads max 8; fds max 4670; goroutines max 9972; generator RSS max 256 MB
- Canary heartbeat max RTT ms (steady): 4.7; /control/health ms max (steady): 1.3
- CSMS calls: http_eof=3018, http_refused=502, http_reset=6, not_connected=284, ok=44075, timeout=26507
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.2%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/30011.6, http_refused 0.1/14.0, http_reset 8.3/18.2, not_connected 0.2/14.6, ok 0.6/1.7, timeout 30030.5/30030.5
- Charger request errors: {'BootNotification/closed': 17, 'Heartbeat/closed': 6, 'MeterValues/closed': 4, 'StartTransaction/closed': 1, 'StatusNotification/closed': 2}; connect errors: {'refused': 21315, 'reset': 27}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1662, max 3997)
rss MB      ▃▆▇▇▇██████▅▇▇▇▇▇▇▇▇▇▆▆▇▇▇▇▇▇▇▇▆▇▇▇▇▇███▄▆▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇  (min 80, max 212)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 4765, max 9972)
cpu cores  ▆██▁▁▁▁▁▁▁▁▁▃▁▁▁▁▁▁▁ ▄▃▁▁▁▁  ▁▁▄▁▁▁▁▁▁▁▁▁▃▁▁▁▁▁▁▁▁▄▁ ▁▁▁▁  ▁  (min 0.09, max 0.14)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3993 | 26 | 2.2 | 2.5 | 6.3 | 8.5 | 15.3 | True |
| 2 | 540 | 3989 | 34 | 2.0 | 2.8 | 6.8 | 8.8 | 12.3 | True |
| 3 | 780 | 3987 | 27 | 2.2 | 2.5 | 6.8 | 9.0 | 10.3 | True |
| 4 | 1020 | 3987 | 29 | 2.2 | 2.5 | 6.3 | 8.5 | 9.3 | True |
| 5 | 1260 | 3992 | 27 | 2.2 | 2.5 | 5.8 | 8.0 | 15.5 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 210 | 212 | +0.8 | -26 | 2.48 | +0 |
| 390 | 540 | 29 | 204 | 208 | +1.4 | -28 | 2.46 | +1 |
| 630 | 780 | 29 | 204 | 209 | +2.0 | +10 | 2.47 | +5 |
| 870 | 1020 | 29 | 204 | 212 | +3.6 | -6 | 2.46 | +5 |
| 1110 | 1260 | 29 | 203 | 209 | +2.0 | +41 | 2.47 | +13 |
| 1350 | 1500 | 29 | 204 | 211 | +1.3 | +12 | 2.46 | +0 |

## High-rate columns (second half of every step)

alloc KB/req divides the server's heap allocation by completed charger requests (each is one CALL and one CALLRESULT). The in-mark share is the wall time inside GC mark phases from `GODEBUG=gctrace=1` (run the harness with `-gctrace` or `-knee`). Scheduler latency is the p99 bucket of `/sched/latencies:seconds` per 5 s sample.

| run | N | msg speed x | msgs/s | CPU cores | max RSS MB | alloc MB/s | alloc KB/req | GC cycles/s | in-mark share (gctrace) | GC CPU cores | GC assist cores | sched latency p99 ms (max window) | heap objects + stacks MB | Heartbeat p50 / p95 / p99 ms |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| knee-gocpp-lenient | 4000 | 160 | 30222 | 1.16 | 151 | 104 | 3.54 | 2.30 | 2.3% | 0.07 | 0.01 | 0.52 | 70 + 32 | 0.1 / 0.5 / 1.7 |
| knee-gocpp-lenient | 4000 | 320 | 59561 | 2.11 | 151 | 202 | 3.48 | 4.67 | 5.2% | 0.15 | 0.09 | 1.05 | 71 + 32 | 0.1 / 1.9 / 7.4 |
| knee-gocpp-lenient-gogc300 | 4000 | 160 | 30245 | 1.10 | 264 | 104 | 3.54 | 0.69 | 0.8% | 0.03 | 0.01 | 0.52 | 110 + 32 | 0.1 / 0.4 / 1.0 |
| knee-gocpp-lenient-gogc300 | 4000 | 320 | 59940 | 2.01 | 270 | 204 | 3.49 | 1.36 | 1.7% | 0.05 | 0.03 | 0.52 | 132 + 32 | 0.1 / 0.6 / 4.2 |
| knee-gocpp-pool64 | 4000 | 160 | 30216 | 0.91 | 152 | 100 | 3.39 | 2.30 | 2.3% | 0.07 | 0.02 | 0.66 | 61 + 34 | 0.1 / 0.3 / 1.9 |
| knee-gocpp-pool64 | 4000 | 320 | 59577 | 1.72 | 152 | 194 | 3.34 | 4.71 | 5.0% | 0.16 | 0.10 | 2.62 | 67 + 34 | 0.1 / 2.1 / 7.4 |
| knee-gocpp-schemaoff | 4000 | 160 | 30215 | 0.81 | 144 | 41 | 1.39 | 0.86 | 0.8% | 0.03 | 0.01 | 0.33 | 68 + 27 | 0.1 / 0.3 / 0.8 |
| knee-gocpp-schemaoff | 4000 | 320 | 59695 | 1.52 | 146 | 80 | 1.37 | 1.75 | 1.8% | 0.06 | 0.03 | 0.39 | 64 + 27 | 0.1 / 0.5 / 5.3 |
| knee-ocppgo | 4000 | 160 | 30215 | 1.04 | 211 | 87 | 2.94 | 1.39 | 1.4% | 0.04 | 0.02 | 0.46 | 103 + 58 | 0.1 / 0.4 / 2.4 |
| knee-ocppgo | 4000 | 320 | 59496 | 1.89 | 213 | 169 | 2.90 | 2.71 | 2.7% | 0.09 | 0.06 | 0.66 | 97 + 60 | 0.1 / 1.4 / 9.3 |
| knee-ocppgo-gogc300 | 4000 | 160 | 30234 | 1.00 | 364 | 87 | 2.94 | 0.43 | 0.5% | 0.02 | 0.01 | 0.46 | 167 + 58 | 0.1 / 0.4 / 0.9 |
| knee-ocppgo-gogc300 | 4000 | 320 | 59951 | 1.84 | 368 | 170 | 2.91 | 0.84 | 0.9% | 0.03 | 0.02 | 0.46 | 161 + 59 | 0.1 / 0.5 / 4.2 |
| soak-gocpp-compat | 4000 | 1 | 282 | 0.10 | 149 | 3 | 9.23 | 0.06 | - | 0.00 | 0.00 | 0.20 | 63 + 31 | 0.2 / 0.5 / 0.8 |
| soak-gocpp-default | 4000 | 1 | 282 | 0.10 | 154 | 2 | 8.69 | 0.05 | - | 0.00 | 0.00 | 0.23 | 65 + 31 | 0.2 / 0.5 / 0.8 |
| soak-gocpp-pool64 | 4000 | 1 | 282 | 0.10 | 158 | 2 | 8.56 | 0.05 | - | 0.00 | 0.00 | 0.33 | 66 + 33 | 0.2 / 0.5 / 0.7 |
| soak-ocppgo | 4000 | 1 | 282 | 0.09 | 212 | 2 | 8.02 | 0.03 | - | 0.00 | 0.00 | 0.13 | 94 + 57 | 0.2 / 0.5 / 0.8 |
