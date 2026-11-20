# gocpp vs ocpp-go large-fleet load/soak report

Date: 2026-11-20. Everything ran locally on one WSL2 machine over loopback.

## Setup

- Servers: the parity test CSMS pair (`interop/parity/cs-gocpp`, `cs-ocppgo`), started with `-lite` (no event history kept), `-call-timeout 30s` and `-pprof`. gocpp is current main (b9cf356, serialized outbound calls by default), ocpp-go is the `v0.17.2-fix.3` fork. `gocpp-compat` is cs-gocpp with `-compat`.
- Server pinned to 4 cores (`taskset -c 0-3`, GOMAXPROCS=4); load generator pinned to 6 other cores (`taskset -c 4-9`, GOMAXPROCS=6). Total cores used: 10.
- Fleet: 4,000 OCPP 1.6J chargers, connect ramp over 60 s, 40% mute (they connect, send their own traffic and answer pings, but never answer a CSMS call), 3 flappers (drop and reconnect every 3-8 s), 20 chargers run by two clients with the same id (replace storm), 15% doing transactions (Authorize, StartTransaction, MeterValues every 5 s for 25 s, StopTransaction, about every 40 s), 2 connectors each.
- Time compression: Heartbeat every 30 s (jittered 10%) instead of 60 s, so message traffic is about 280 msg/s at 4,000 chargers.
- CSMS driver: 50 calls/s through `/control/call` (GetConfiguration 50%, TriggerMessage 30%, RemoteStartTransaction 20%) to random chargers; about 40% land on mute chargers and time out after 30 s.
- 25 min per soak run, 5 restarts (SIGTERM, 2 s pause, start). Chargers reconnect with 0-3 s initial jitter then exponential backoff (1 s base, 30 s cap, full jitter).
- Freeze detector: two canary chargers send a Heartbeat every second; a round trip over 10 s or three stalled `/control/health` polls is a freeze (goroutine dump from pprof). The detector was checked once by SIGSTOP-ing a server for 18 s: it fired after 9 s via the health check and saved a goroutine dump.
- Latency percentiles come from log-bucket histograms (12% resolution, bucket upper bound). Restart recovery times are dominated by the client behaviour (2.2 s until the server is healthy again, plus reconnect jitter and backoff after refused connections), so they are comparable between servers but not a server-only number.

## Findings

1. **No freeze, crash, leak or unbounded growth in any run, for either library**: gocpp default, gocpp `-compat` and the ocpp-go fork all kept the canary Heartbeat round trip under ~5 ms in steady state, never stalled `/control/health`, and logged no panic. The mute-charger call timeouts (about 26,500 per run) did not stall anything.
2. All three servers reconnect 99% of the fleet within 6.0-7.0 s of being healthy after each restart (about 8.3-9.1 s after the kill); gocpp and the fork are indistinguishable here.
3. Steady-state latency at 4,000 chargers is sub-millisecond for all of them (Heartbeat p50 0.2 ms, p99 0.7-0.9 ms).
4. **Memory and goroutines**: gocpp uses about 2.3x the memory and 3.3x the goroutines of the fork for the same fleet (4,000 connections: about 490 MB / 33,300 goroutines vs 210 MB / 9,950; 8.3 vs 2.5 goroutines per connection; `-compat` 457 MB / 29,300). Both scale linearly with connections (10,000: 1,198 MB / 83,000 vs 503 MB / 24,600). RSS sawtooths with GC and restarts but plateaus at the same level in every segment; goroutine counts are flat per connection (no drift), file descriptor counts do not drift.
5. **Hot-spot test** (90% of 300 calls/s aimed at 100 mute chargers, so each of them has ~80 calls waiting at any time): no freeze; gocpp reached 800 MB / 11 goroutines per connection (flat after warm-up), the fork 479 MB / 5. gocpp answers 7,832 of those calls with `ocpp: outbound queue full` (kind `other`) because its per-connection wait queue is bounded (`AsyncQueueSize`); the fork has no equivalent outcome and lets all of them time out.
6. **Knee**: no knee in fleet size up to 10,000 chargers (server CPU 0.22 core, canary RTT 5 ms at most, Heartbeat p99 3.4 ms on gocpp, 0.6 ms on the fork). Increasing the message rate instead (4,000 chargers, timers sped up 1x-320x): both stay flat to about 15,000 msg/s; at 30,000 msg/s gocpp Heartbeat p99 is 18 ms (1.56 of 4 cores) against 1.4 ms for the fork (1.09 cores); at about 58,000 msg/s gocpp p99 is 29 ms (2.54 cores) against 6.6 ms (2.0 cores). Neither is CPU-saturated at that point (4 cores available), so the gocpp tail growth beyond ~15-30k msg/s is something other than raw CPU (not investigated; candidates are scheduler or GC pressure from the larger per-message allocation and goroutine footprint, or contention in the per-connection path).


## Side-by-side: 4,000 chargers, 40% mute, 25 min, 5 restarts

Latency columns are p50 / p95 / p99 in ms (histogram bucket upper bounds, 12% resolution). Steady = windows that are not within a restart recovery; restart = 10 s windows overlapping a restart recovery.

| server | Boot steady | Heartbeat steady | MeterValues steady | Boot restart | Heartbeat restart | MeterValues restart |
|---|---|---|---|---|---|---|
| gocpp-compat | 0.2 / 0.5 / 0.8 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 1.0 | 0.2 / 1.5 / 5.3 | 0.2 / 0.5 / 0.8 | 0.3 / 0.6 / 1.0 |
| gocpp-default | 0.2 / 0.4 / 0.7 | 0.2 / 0.5 / 0.7 | 0.3 / 0.5 / 0.9 | 0.2 / 1.4 / 4.2 | 0.2 / 0.5 / 0.8 | 0.3 / 0.5 / 0.9 |
| ocppgo-fork | 0.2 / 0.6 / 0.9 | 0.2 / 0.6 / 0.9 | 0.3 / 0.7 / 0.9 | 0.1 / 0.6 / 2.4 | 0.2 / 0.6 / 0.9 | 0.3 / 0.7 / 1.0 |

| server | freeze | crashes | median up | max RSS MB | max goroutines | mean CPU cores | t99 per restart (s from healthy) | CSMS call outcomes |
|---|---|---|---|---|---|---|---|---|
| gocpp-compat | no | 0 | 3989 | 457 | 29314 | 0.10 | 6.3, 6.8, 7.0, 6.3, 6.0 | http_eof:3014, http_refused:502, http_reset:8, not_connected:288, ok:44101, timeout:26479 |
| gocpp-default | no | 0 | 3989 | 501 | 33326 | 0.10 | 6.5, 6.5, 6.8, 6.3, 6.3 | http_eof:3015, http_refused:502, http_reset:11, not_connected:290, ok:44087, timeout:26487 |
| ocppgo-fork | no | 0 | 3989 | 213 | 9964 | 0.09 | 6.3, 6.5, 6.8, 6.3, 6.3 | http_eof:3014, http_refused:500, http_reset:5, not_connected:285, ok:44099, timeout:26489 |

### gocpp-compat

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 260, max 457; CPU cores mean 0.10, max 0.15; threads max 9; fds max 4665; goroutines max 29314; generator RSS max 264 MB
- Canary heartbeat max RTT ms (steady): 3.5; /control/health ms max (steady): 2.7
- CSMS calls: http_eof=3014, http_refused=502, http_reset=8, not_connected=288, ok=44101, timeout=26479
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.3%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/30010.9, http_refused 0.1/14.1, http_reset 3.8/23.8, not_connected 0.2/10.4, ok 0.5/1.5, timeout 30030.6/30030.6
- Charger request errors: {'Authorize/closed': 3, 'BootNotification/closed': 19, 'Heartbeat/closed': 16, 'MeterValues/closed': 11}; connect errors: {'refused': 21322, 'reset': 43}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1665, max 3995)
rss MB      ▃▆▇▇▇▇█████▅▆▇▇▇▇▇▇▇▇▆▆▇▇▇▇▇▇▇▇▆▆▇▇▇▇▇▇▇▅▆▇▇▇▇▇▇▇▇▆▆▆▆▇▇▇▇▇  (min 163.9, max 457)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1.278e+04, max 2.931e+04)
cpu cores  ▃▆▅▄▁▃▃▃▃▆▃▄▅▆▂▂▂▂▃▂▃▅▃▁▃▁▃▃▂▄▄▄▄▁▂▁▂▂▃ ▃▅▂█▅▅▄▆▆▅█▆▆▅▄▆▆▆▄▅  (min 0.08, max 0.15)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3991 | 42 | 2.2 | 2.5 | 6.3 | 8.5 | 14.8 | True |
| 2 | 540 | 3988 | 37 | 2.0 | 2.8 | 6.8 | 8.8 | 11.3 | True |
| 3 | 780 | 3988 | 44 | 2.3 | 2.5 | 7.0 | 9.3 | 10.5 | True |
| 4 | 1020 | 3988 | 39 | 2.2 | 2.5 | 6.3 | 8.5 | 9.3 | True |
| 5 | 1260 | 3991 | 38 | 2.3 | 2.5 | 6.0 | 8.3 | 12.0 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 451 | 457 | +1.1 | -8 | 7.32 | +2 |
| 390 | 540 | 29 | 436 | 451 | +8.5 | -18 | 7.31 | +1 |
| 630 | 780 | 29 | 439 | 449 | +4.7 | +9 | 7.32 | +5 |
| 870 | 1020 | 29 | 422 | 441 | +8.7 | -5 | 7.31 | +4 |
| 1110 | 1260 | 29 | 435 | 450 | +6.8 | +22 | 7.31 | +12 |
| 1350 | 1500 | 29 | 404 | 453 | +19.9 | +4 | 7.31 | +0 |

### gocpp-default

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 260, max 501; CPU cores mean 0.10, max 0.20; threads max 9; fds max 4673; goroutines max 33326; generator RSS max 264 MB
- Canary heartbeat max RTT ms (steady): 4.2; /control/health ms max (steady): 8.8
- CSMS calls: http_eof=3015, http_refused=502, http_reset=11, not_connected=290, ok=44087, timeout=26487
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.3%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/30021.6, http_refused 0.1/16.2, http_reset 13.1/36.2, not_connected 0.2/28.9, ok 0.5/1.4, timeout 30060.0/30060.0
- Charger request errors: {'Authorize/closed': 4, 'BootNotification/closed': 20, 'Heartbeat/closed': 13, 'MeterValues/closed': 13}; connect errors: {'refused': 21308, 'reset': 44}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇  (min 1664, max 3996)
rss MB      ▃▅▇▇▇▇▇▇▇██▄▅▆▆▆▇▇▇▇▇▄▆▆▆▇▇▇▇▇▇▅▆▆▇▇▇▇▇▇▄▅▇▇▇▇▇▇▇▇▅▆▆▇▇▇▇▇▇  (min 168, max 501.1)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1.447e+04, max 3.333e+04)
cpu cores  ▁▃▃ ▂▂▂▂▄▄▃▂▂▃▄▁▃ ▂ ▂▂ ▂▁ ▃▁▃ █▂ ▃▃▃▁▂▂▄▁▂▁▂▁▂▁▂▁▂▂▂▃▁▄▃▂▅▁▂  (min 0.09, max 0.2)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3994 | 63 | 2.3 | 2.5 | 6.5 | 8.8 | 15.3 | True |
| 2 | 540 | 3990 | 41 | 2.2 | 2.5 | 6.5 | 8.8 | 11.3 | True |
| 3 | 780 | 3985 | 43 | 2.3 | 2.5 | 6.8 | 9.0 | 9.8 | True |
| 4 | 1020 | 3987 | 41 | 2.2 | 2.5 | 6.3 | 8.5 | 7.8 | True |
| 5 | 1260 | 3990 | 37 | 2.1 | 2.8 | 6.3 | 8.3 | 11.8 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 476 | 501 | +6.1 | -10 | 8.32 | +0 |
| 390 | 540 | 29 | 433 | 479 | +19.5 | -18 | 8.31 | +1 |
| 630 | 780 | 29 | 458 | 487 | +13.9 | +8 | 8.32 | +5 |
| 870 | 1020 | 29 | 458 | 478 | +9.3 | -4 | 8.31 | +5 |
| 1110 | 1260 | 29 | 475 | 488 | +1.6 | +23 | 8.31 | +13 |
| 1350 | 1500 | 29 | 453 | 488 | +12.5 | +3 | 8.31 | -0 |

### ocppgo-fork

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 120, max 213; CPU cores mean 0.09, max 0.13; threads max 8; fds max 4666; goroutines max 9964; generator RSS max 257 MB
- Canary heartbeat max RTT ms (steady): 2.5; /control/health ms max (steady): 5.6
- CSMS calls: http_eof=3014, http_refused=500, http_reset=5, not_connected=285, ok=44099, timeout=26489
  - shares: http_eof 4.1%, http_refused 0.7%, http_reset 0.0%, not_connected 0.4%, ok 59.3%, timeout 35.6%
  - call latency ms (p50/p99): http_eof 16485.8/29996.3, http_refused 0.1/14.0, http_reset 4.2/12.9, not_connected 0.2/2.1, ok 0.6/1.9, timeout 30033.5/30033.5
- Charger request errors: {'Authorize/closed': 1, 'BootNotification/closed': 20, 'Heartbeat/closed': 10, 'MeterValues/closed': 5}; connect errors: {'refused': 21222, 'reset': 42}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 1657, max 3995)
rss MB      ▃▆▇▇▇▇▇▇▇██▄▅▇▇▇▇▇▇▇▇▅▆▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇▅▆▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇  (min 79.4, max 213.1)
goroutines  ▅▇▇▇▇█▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 4751, max 9964)
cpu cores  ▃▆█▁  ▁     ▁  ▁    ▁▃ ▁     ▁▁▄   ▁     ▃  ▁  ▁  ▁▁          (min 0.1, max 0.15)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 300 | 3993 | 25 | 2.2 | 2.5 | 6.3 | 8.5 | 21.0 | True |
| 2 | 540 | 3988 | 24 | 2.2 | 2.5 | 6.5 | 8.7 | 9.0 | True |
| 3 | 780 | 3986 | 33 | 2.2 | 2.5 | 6.8 | 9.0 | 10.5 | True |
| 4 | 1020 | 3987 | 27 | 2.2 | 2.5 | 6.3 | 8.5 | 7.5 | True |
| 5 | 1260 | 3992 | 30 | 2.2 | 2.5 | 6.3 | 8.5 | 13.3 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 300 | 36 | 209 | 213 | +1.6 | -26 | 2.47 | +1 |
| 390 | 540 | 29 | 199 | 211 | +5.1 | -28 | 2.46 | +1 |
| 630 | 780 | 29 | 204 | 213 | +3.3 | +10 | 2.47 | +5 |
| 870 | 1020 | 29 | 205 | 208 | +1.2 | -6 | 2.46 | +5 |
| 1110 | 1260 | 29 | 204 | 213 | +1.8 | +41 | 2.47 | +13 |
| 1350 | 1500 | 29 | 208 | 209 | +0.3 | +12 | 2.46 | +0 |

## Hot-spot test: most CSMS calls aimed at 100 mute chargers (serialized per connection)

### hot-gocpp

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 12.0 min; chargers up (client side, after ramp) median 3989
- Server RSS MB: min 283, max 800; CPU cores mean 0.16, max 0.24; threads max 9; fds max 10013; goroutines max 44024; generator RSS max 517 MB
- Canary heartbeat max RTT ms (steady): 1.8; /control/health ms max (steady): 36.3
- CSMS calls: driver_saturated=52601, http_eof=11995, http_refused=1208, http_reset=23, not_connected=1091, ok=9474, other=7832, timeout=125769
  - shares: driver_saturated 25.0%, http_eof 5.7%, http_refused 0.6%, http_reset 0.0%, not_connected 0.5%, ok 4.5%, other 3.7%, timeout 59.9%
  - call latency ms (p50/p99): driver_saturated 0.0/0.0, http_eof 20679.8/30040.7, http_refused 10.4/16.4, http_reset 20.6/40.6, not_connected 0.2/16.4, ok 0.5/1.7, other 0.3/1.1, timeout 30062.8/30062.8
- Charger request errors: {'Authorize/closed': 1, 'BootNotification/closed': 19, 'Heartbeat/closed': 11, 'MeterValues/closed': 3, 'StatusNotification/closed': 1}; connect errors: {'refused': 8591, 'reset': 29}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▁▃▅▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇  (min 660, max 3995)
rss MB      ▁▂▄▅▆▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▃▄▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▃▄▅▅▅▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 125.9, max 800.4)
goroutines  ▂▄▆▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▄▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇  (min 1.013e+04, max 4.402e+04)
cpu cores  ▄▅▅█▆▅▃▂▆▃▂▆▂▂▆▂▃▅▃▃▃▃▅▃▅▂▄▅▂▅▂▂▅ ▂▅▂▄▁▂▂█▄▃▂▅▂▅▂▂▅▅▂▅▂▃▃▂▂▅  (min 0.13, max 0.24)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 280 | 3990 | 62 | 2.3 | 2.5 | 6.3 | 8.5 | 13.3 | True |
| 2 | 500 | 3988 | 68 | 2.3 | 2.5 | 6.8 | 9.1 | 11.5 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 280 | 32 | 768 | 800 | +7.9 | +5 | 11.00 | +0 |
| 370 | 500 | 25 | 748 | 785 | +18.9 | +6 | 11.00 | +0 |
| 590 | 720 | 25 | 750 | 798 | +24.2 | +3 | 11.00 | -0 |

### hot-ocppgo

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 12.0 min; chargers up (client side, after ramp) median 3988
- Server RSS MB: min 134, max 479; CPU cores mean 0.13, max 0.17; threads max 8; fds max 10012; goroutines max 20389; generator RSS max 520 MB
- Canary heartbeat max RTT ms (steady): 1.2; /control/health ms max (steady): 5.7
- CSMS calls: driver_saturated=60545, http_eof=11991, http_refused=1202, http_reset=28, not_connected=1067, ok=9023, timeout=126142
  - shares: driver_saturated 28.8%, http_eof 5.7%, http_refused 0.6%, http_reset 0.0%, not_connected 0.5%, ok 4.3%, timeout 60.1%
  - call latency ms (p50/p99): driver_saturated 0.0/0.0, http_eof 20679.8/30033.5, http_refused 10.4/16.3, http_reset 14.6/31.6, not_connected 0.3/20.6, ok 0.5/1.7, timeout 30059.0/30059.0
- Charger request errors: {'Authorize/closed': 1, 'BootNotification/closed': 19, 'Heartbeat/closed': 9, 'MeterValues/closed': 1, 'StatusNotification/closed': 1}; connect errors: {'refused': 8562, 'reset': 30}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▁▃▅█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 659, max 3995)
rss MB      ▁▃▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▃▄▄▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▂▃▅▅▅▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 87.7, max 478.6)
goroutines  ▃▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▁▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 6280, max 2.039e+04)
cpu cores  ▅▆▄█▆▃▅▁▃▄▃▃▂▄▄▃▅▂▄▃▃▃▂▆▅▂▅▃ ▅▃▃▃ ▃▂▅▃ ▅▃▃▅▃▅▄▂▃▅▃▃▂▃▃▅▃▂▃▅▃  (min 0.1, max 0.19)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 280 | 3988 | 54 | 2.1 | 2.8 | 6.5 | 8.6 | 10.5 | True |
| 2 | 500 | 3989 | 56 | 2.3 | 2.5 | 6.8 | 9.0 | 11.5 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 280 | 32 | 471 | 478 | +3.2 | -4 | 5.09 | +0 |
| 370 | 500 | 25 | 466 | 476 | +4.7 | -1 | 5.09 | -0 |
| 590 | 720 | 25 | 449 | 475 | +11.4 | -0 | 5.08 | +0 |

## Step test: steps-gocpp

| N | call rate/s | msg speed x | median up (2nd half) | max RSS MB | mean CPU cores | max goroutines | canary max RTT ms | health max ms | Heartbeat p50/p99 ms | MeterValues p50/p99 ms | msgs/s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4000 | 50 | 1 | 3991 | 493 | 0.13 | 33331 | 1 | 0 | 0.2 / 0.8 | 0.3 / 0.9 | 282 |
| 6000 | 75 | 1 | 5990 | 732 | 0.16 | 49945 | 1 | 0 | 0.2 / 0.8 | 0.3 / 0.9 | 409 |
| 8000 | 100 | 1 | 7990 | 966 | 0.19 | 66520 | 1 | 0 | 0.2 / 0.7 | 0.2 / 0.8 | 541 |
| 10000 | 125 | 1 | 9990 | 1198 | 0.22 | 83159 | 5 | 2 | 0.2 / 3.4 | 0.2 / 3.8 | 672 |

Freeze: no; aborted: False ; calls: {'not_connected': 9, 'ok': 39279, 'timeout': 25169}

```
up           ▁▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▃▃▄▄▄▄▄▄▄▄▄▄▄▄▅▅▅▆▆▆▆▆▆▆▆▆▆▆▆▇▇▇▇▇▇▇█▇▇▇█  (min 662, max 9993)
rss MB       ▁▁▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▅▅▅▅▅▆▆▆▆▆▆▆▆▆▇▇▇▇▇▇▇▇▇▇█  (min 70.8, max 1198)
cpu cores  ▁▁▁▂▂▁  ▁  ▁    ▁ ▁▂▂▄▂▂▃▁▂▁▁▂▁▂▂▄▃▅▃▄▂▂▄▂▂▄▂▄▃▃▆▆▂▃▆▃▆▃▆█▄▆  (min 0.12, max 0.32)
canary ms          ▁                    ▁ ▁         ▁              █▂▃   (min 0.2, max 5.1)
```

## Step test: steps-knee-gocpp

| N | call rate/s | msg speed x | median up (2nd half) | max RSS MB | mean CPU cores | max goroutines | canary max RTT ms | health max ms | Heartbeat p50/p99 ms | MeterValues p50/p99 ms | msgs/s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4000 | 50 | 1 | 3990 | 488 | 0.13 | 33316 | 1 | 0 | 0.2 / 0.8 | 0.3 / 0.9 | 282 |
| 4000 | 50 | 5 | 3990 | 497 | 0.21 | 33303 | 1 | 2 | 0.2 / 0.6 | 0.2 / 0.7 | 1041 |
| 4000 | 50 | 10 | 3989 | 501 | 0.27 | 33303 | 0 | 0 | 0.2 / 0.5 | 0.2 / 0.6 | 1989 |
| 4000 | 50 | 20 | 3990 | 501 | 0.35 | 33307 | 0 | 3 | 0.1 / 0.4 | 0.2 / 0.5 | 3887 |
| 4000 | 50 | 40 | 3989 | 502 | 0.50 | 33298 | 0 | 0 | 0.1 / 0.4 | 0.2 / 0.6 | 7679 |

Freeze: no; aborted: False ; calls: {'not_connected': 9, 'ok': 26739, 'timeout': 17643}

```
up          ▂▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 989, max 3995)
rss MB      ▁▃▄▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█  (min 96.4, max 501.8)
cpu cores    ▁▁        ▁▂▂▂▁▂▂▂▂▂▂▂▃▃▃▃▃▃▃▃▃▃▃▃▄▄▄▄▅▄▅▄▄▄▅▄▇▇█▇▇▇█████▇  (min 0.12, max 0.51)
canary ms  ▂▃▂▁▄▂▅▂▂▄▆▃▁▃▄ ▁ ▁ ▁█ ▂▄▂▂▁▃ ▁       ▂    ▂ ▁        ▁  ▁ ▁  (min 0.2, max 1.1)
```

## Step test: steps-knee-ocppgo

| N | call rate/s | msg speed x | median up (2nd half) | max RSS MB | mean CPU cores | max goroutines | canary max RTT ms | health max ms | Heartbeat p50/p99 ms | MeterValues p50/p99 ms | msgs/s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4000 | 50 | 1 | 3989 | 212 | 0.09 | 9971 | 1 | 0 | 0.2 / 0.8 | 0.3 / 0.9 | 282 |
| 4000 | 50 | 5 | 3990 | 215 | 0.16 | 9928 | 1 | 0 | 0.2 / 0.6 | 0.2 / 0.7 | 1041 |
| 4000 | 50 | 10 | 3989 | 216 | 0.21 | 9926 | 0 | 0 | 0.2 / 0.5 | 0.2 / 0.6 | 1989 |
| 4000 | 50 | 20 | 3990 | 216 | 0.28 | 9961 | 1 | 0 | 0.1 / 0.5 | 0.2 / 0.5 | 3887 |
| 4000 | 50 | 40 | 3989 | 218 | 0.37 | 9911 | 0 | 0 | 0.1 / 0.4 | 0.2 / 0.5 | 7678 |

Freeze: no; aborted: False ; calls: {'not_connected': 7, 'ok': 26734, 'timeout': 17650}

```
up          ▂▅█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇██▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 991, max 3994)
rss MB      ▂▃▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇████████  (min 56.7, max 217.5)
cpu cores    ▁▁        ▁▁▂▁▂▂▂▂▂▂▁▁▃▃▃▃▃▃▃▃▃▃▃▃▅▅▅▅▅▅▅▅▅▅▅▅▇▇▇▇▇▇▇▇█▇▇▇  (min 0.1, max 0.38)
canary ms  ▂▁▂▂▅█▂▄▂▃▁▃▁▂▂▂▂▂▂▂▄   ▃▂ ▄   ▂     ▃  ▂▃▁ ▃▄ ▂  ▁   ▁       (min 0.2, max 1.4)
```

## Step test: steps-knee2-gocpp

| N | call rate/s | msg speed x | median up (2nd half) | max RSS MB | mean CPU cores | max goroutines | canary max RTT ms | health max ms | Heartbeat p50/p99 ms | MeterValues p50/p99 ms | msgs/s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4000 | 50 | 1 | 3990 | 490 | 0.13 | 33316 | 1 | 0 | 0.2 / 0.8 | 0.2 / 0.8 | 282 |
| 4000 | 50 | 80 | 3989 | 502 | 0.84 | 33305 | 1 | 0 | 0.1 / 1.2 | 0.2 / 2.1 | 15221 |
| 4000 | 50 | 160 | 3990 | 498 | 1.56 | 33295 | 7 | 11 | 0.2 / 18.4 | 0.2 / 16.4 | 30098 |
| 4000 | 50 | 320 | 3989 | 494 | 2.54 | 33430 | 26 | 26 | 0.2 / 28.9 | 0.2 / 25.8 | 58013 |

Freeze: no; aborted: False ; calls: {'not_connected': 6, 'ok': 21349, 'timeout': 14048}

```
up          ▁▃▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇█▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇  (min 657, max 3996)
rss MB       ▂▃▅▆▆▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 71.7, max 501.5)
cpu cores                 ▁▁▂▂▂▂▂▂▂▂▂▂▂▂▂▄▄▄▄▄▄▄▄▄▄▅▄▄▄▄█▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 0.12, max 2.64)
canary ms                                  ▂▁▁▂     ▁  ▁▆▂▇█▇▇▅▆▆▆▆▅▅▆▆  (min 0.2, max 30.2)
```

## Step test: steps-knee2-ocppgo

| N | call rate/s | msg speed x | median up (2nd half) | max RSS MB | mean CPU cores | max goroutines | canary max RTT ms | health max ms | Heartbeat p50/p99 ms | MeterValues p50/p99 ms | msgs/s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4000 | 50 | 1 | 3989 | 209 | 0.10 | 9977 | 1 | 0 | 0.2 / 0.8 | 0.2 / 0.7 | 282 |
| 4000 | 50 | 80 | 3989 | 220 | 0.59 | 9928 | 1 | 0 | 0.1 / 0.6 | 0.2 / 0.9 | 15225 |
| 4000 | 50 | 160 | 3988 | 220 | 1.09 | 9933 | 8 | 0 | 0.1 / 1.4 | 0.2 / 2.4 | 30235 |
| 4000 | 50 | 320 | 3989 | 220 | 2.00 | 9970 | 10 | 2 | 0.1 / 6.6 | 0.2 / 8.3 | 59889 |

Freeze: no; aborted: False ; calls: {'not_connected': 9, 'ok': 21331, 'timeout': 14062}

```
up          ▁▃▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇  (min 658, max 3995)
rss MB      ▁▂▄▅▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 41.1, max 220.2)
cpu cores                 ▁▁▂▂▂▂▂▂▂▂▂▂▂▂▂▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇▇▇▇▇▇▇▇█▇▇  (min 0.09, max 2.04)
canary ms                                       ▅      ▁ ▂▁█  ▁ ▃▆ ▇▄▁▄  (min 0.2, max 10.1)
```

## Step test: steps-ocppgo

| N | call rate/s | msg speed x | median up (2nd half) | max RSS MB | mean CPU cores | max goroutines | canary max RTT ms | health max ms | Heartbeat p50/p99 ms | MeterValues p50/p99 ms | msgs/s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 4000 | 50 | 1 | 3990 | 210 | 0.10 | 9964 | 1 | 1 | 0.2 / 0.9 | 0.3 / 0.9 | 282 |
| 6000 | 75 | 1 | 5990 | 303 | 0.12 | 14893 | 1 | 0 | 0.2 / 0.8 | 0.2 / 0.8 | 409 |
| 8000 | 100 | 1 | 7990 | 406 | 0.14 | 19761 | 1 | 0 | 0.2 / 0.7 | 0.2 / 0.8 | 541 |
| 10000 | 125 | 1 | 9988 | 503 | 0.16 | 24648 | 1 | 0 | 0.2 / 0.6 | 0.2 / 0.7 | 671 |

Freeze: no; aborted: False ; calls: {'not_connected': 8, 'ok': 39291, 'timeout': 25168}

```
up           ▁▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▅▅▆▆▆▆▆▆▆▆▆▆▆▆▇▇▇▇▇▇█▇▇▇▇█  (min 660, max 9994)
rss MB       ▁▁▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▅▅▅▅▅▆▆▆▆▆▆▆▆▆▆▆▇▇▇▇▇▇▇██  (min 40.9, max 503.3)
cpu cores  ▁▂▂▂▃             ▂▃▃▃▄▄▂▂▂▂▂▂▂▂▄▄▅▅▅▄▄▃▅▃▅▄▄▄▅▆▅▆▆█▅▆▅▅▇▅▇▅  (min 0.09, max 0.2)
canary ms  ▂▄  ▄▂▄▂▁▄▂▂▂▄▁▃▄▁▂▄▃ █▂▃▅▂▂▂▁▁▄   ▁▁   ▄ ▁▁▃  ▃ ▂ ▁▁▄ ▄  ▁▁  (min 0.3, max 1.3)
```
