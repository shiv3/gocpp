# Gateway final acceptance run, 2026-11-21

Code: integ/gateway at f337e64 plus #60 (idle footprint tests) and the harness changes on this branch
(`-plan`, per-process aggregation in `report.py`, `Recorder.AssumeConnected` for backends).
Machine: 20-core shared box (load 2-6 from other jobs). Embedded and gateway runs ran concurrently on disjoint,
pinned cores: embedded server cores 2-5 (generator 6-7); gateways 8-9, backends 10-13, generator 14-17.
Both: 4,000 chargers, 25 min, `-mute 0.4 -flappers 3 -dups 20 -tx-frac 0.15`, 50 CSMS calls/s, Heartbeat 30 s,
7 restarts spread over the run in ONE run.

* embedded: `-plan server:term,server:kill,server:term,server:term,server:kill,server:term,server:term`
  (SIGTERM drains over 20 s, then exits; kill is SIGKILL).
* gateway: 2 gateways, 3 backends in one pool, registry (miniredis in the harness):
  `-plan backend:term,backend:kill,gateway:term,backend:term,backend:kill,gateway:term,backend:term`
  = 5 backend restarts (3 SIGTERM drains, 2 SIGKILL) and 2 paced gateway drains (SIGTERM, 20 s window).
* gateway-10k-rolling-drain: 10,000 chargers, 3 gateways, 3 backends, 8 min (90 s ramp), 3 gateway drains in
  sequence (SIGTERM, ~97 s apart, so every gateway is drained once, at 3.3k sessions each).

## Result

| | embedded | gateway (2 gw + 3 be) |
|---|---|---|
| freeze / crashes / aborted | none / 0 / no | none / 0 / no |
| median chargers up (of 4,000) | 3,988 | 3,996 |
| Heartbeat steady p50 / p95 / p99 ms | 0.3 / 0.4 / 0.5 | 0.5 / 0.9 / 1.1 |
| BootNotification steady p50 / p95 / p99 ms | 0.2 / 0.4 / 0.5 | 0.6 / 1.0 / 1.2 |
| latency in restart windows (Heartbeat p99) | 0.5 | 1.1 |
| backend restart (term or kill): chargers disconnected | all 4,000 (server restart) | none (min up 3,987-3,991 of 3,993-3,999: flappers only) |
| gateway drain (SIGTERM, paced over 20 s) | - | deepest dip 3,625 and 3,270 of 4,000 up (9% and 18% of the fleet reconnecting at the deepest point; about half of the fleet is on each gateway and is closed over the 20 s window); 99% back within 1.8 s of the gateway being ready again |
| server restart recovery (t99 from healthy) | 6-27 s | backends 0 s; gateway drains 0-1.8 s after ready |
| CSMS calls ok / total | 42,252 / 74,385 (mute chargers time out) | 44,427 / 74,392 |
| CSMS call errors other than mute timeouts | http_eof 3,567, refused 705, not_connected 2,873 (restart windows) | not_connected 324, other 642 |
| charger request errors | 41 (closed during restarts) | 12 |
| total CPU, mean over the steady windows (~300 msg/s) | 0.12 cores | 0.32 cores (gateways 0.18, backends 0.13) |
| max RSS | 166 MB | gateways 148 MB each, backends 28-64 MB |

Notes:

* A backend restart, SIGTERM or SIGKILL, did not disconnect any charger and produced no error visible to the
  chargers in the timeline (chargers up never dropped below 3,987; 12 `closed` request errors over the whole run, which
  were not attributed to a specific event). The embedded server drops every connection at every restart and needs 6-27 s for 99% to be back
  (jittered reconnect backoff).
* A gateway drain is the only event that disconnects chargers in the split topology, and only those on that
  gateway, paced over the drain window; the harness' chargers reconnect through the other gateway at once.
* Added latency of the split at this load: +0.2 ms p50, +0.6 ms p99 (Heartbeat 0.3 -> 0.5 ms p50,
  0.5 -> 1.1 ms p99).
* CPU: at ~300 msg/s the whole split deployment uses 0.32 cores against 0.12 for embedded (2.7x). That is not the
  throughput case: it is dominated by per-session fixed costs (30 s Heartbeats, link keepalives, registry
  leases, five processes) and the run is not CPU-bound anywhere. The throughput comparison at 37k msg/s is in
  docs/gateway.md (about 1.29x embedded in total, after link write coalescing, #56).
* Not comparable between the two columns: CSMS call counts per run (the same 50 calls/s ask a charger picked at
  random). A first run of this scenario showed 65% `not_connected` calls in gateway mode; that was a harness
  artifact (the control backend answered `not connected` from its own connect-event bookkeeping, which only sees
  1/N of the connects). Fixed with `Recorder.AssumeConnected` in backend mode and the whole run repeated; the
  numbers above are from the repeat. The first run's raw data was not kept.

### 10,000 chargers, 3 gateways, rolling drain

| | |
|---|---|
| chargers up before each drain | 9,997 / 9,997 / 9,999 |
| deepest dip within 90 s of the drain start | 9,565, 9,364, 9,015 (a third of the fleet is on each gateway and is drained over 20 s) |
| all chargers back | within 3.3 s of the gateway being ready again |
| freeze / crashes | none / 0 |
| Heartbeat steady p50 / p99 ms | 0.5 / 1.0 |
| CPU before the drains (6.6e2 msg/s) | gateways 0.12 cores each, backends 0.07-0.08 each |
| max RSS | gateways about 118 MB each (3.3k sessions), backends 33-48 MB; generator 705 MB; machine available memory never below 3.9 GB |

Each drain takes 62 s from SIGTERM to a ready replacement (20 s paced close plus the 60 s exit grace of the
harness), so a 3-gateway rolling drain with the replacement started after each exit lasts about 5 min; the
run's 8 min include the ramp.

---

# Generated report (report.py)


## Side-by-side: 4,000 chargers, 40% mute, 25 min, 5 restarts

Latency columns are p50 / p95 / p99 in ms (histogram bucket upper bounds, 12% resolution). Steady = windows that are not within a restart recovery; restart = 10 s windows overlapping a restart recovery.

| server | Boot steady | Heartbeat steady | MeterValues steady | Boot restart | Heartbeat restart | MeterValues restart |
|---|---|---|---|---|---|---|
| embedded | 0.2 / 0.4 / 0.5 | 0.3 / 0.4 / 0.5 | 0.3 / 0.5 / 0.6 | 0.2 / 0.5 / 2.1 | 0.3 / 0.4 / 0.5 | 0.3 / 0.5 / 0.6 |
| gateway | 0.6 / 1.0 / 1.2 | 0.5 / 0.9 / 1.1 | 0.5 / 1.0 / 1.1 | 0.6 / 0.9 / 1.1 | 0.5 / 0.9 / 1.1 | 0.5 / 1.0 / 1.1 |
| gateway-10k-rolling-drain | 0.6 / 0.9 / 1.1 | 0.5 / 0.9 / 1.0 | 0.5 / 0.9 / 1.0 | 0.5 / 0.9 / 1.0 | 0.5 / 0.9 / 1.0 | 0.5 / 0.9 / 1.1 |

| server | freeze | crashes | median up | max RSS MB | max goroutines | mean CPU cores | t99 per restart (s from healthy) | CSMS call outcomes |
|---|---|---|---|---|---|---|---|---|
| embedded | no | 0 | 3988 | 166 | 5318 | 0.12 | 25.1, 6.0, 26.6, 25.5, 6.0, 24.8, 25.3 | http_eof:3567, http_refused:705, http_reset:4, not_connected:2873, ok:42252, other:223, timeout:24761 |
| gateway | no | 0 | 3996 | 394 | 5318 | 0.31 | 0.0, 0.0, 0.0, 0.0, 0.0, 1.8, 0.0 | not_connected:324, ok:44427, other:642, timeout:28999 |
| gateway-10k-rolling-drain | no | 0 | 9988 | 662 | -1 | 0.71 | 0.0, 0.0, 0.0 | not_connected:91, ok:5723, other:248, timeout:3309 |

### embedded

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3988
- Server RSS MB: min 58, max 166; CPU cores mean 0.12, max 0.23; threads max 10; fds max 4675; goroutines max 5318; generator RSS max 263 MB
- Canary heartbeat max RTT ms (steady): 3.9; /control/health ms max (steady): 89.6
- CSMS calls: http_eof=3567, http_refused=705, http_reset=4, not_connected=2873, ok=42252, other=223, timeout=24761
  - shares: http_eof 4.8%, http_refused 0.9%, http_reset 0.0%, not_connected 3.9%, ok 56.8%, other 0.3%, timeout 33.3%
  - call latency ms (p50/p99): http_eof 18464.1/30016.7, http_refused 0.1/14.6, http_reset 10.4/16.4, not_connected 0.3/0.8, ok 0.5/1.1, other 0.3/0.5, timeout 30035.7/30035.7
- Charger request errors: {'Authorize/closed': 1, 'BootNotification/closed': 17, 'Heartbeat/closed': 13, 'MeterValues/closed': 9, 'StatusNotification/closed': 1}; connect errors: {'bad_status': 49880, 'refused': 21118, 'reset': 32}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇█▇▇▇▇▇▇▇▇██▇▇▇▇█▇▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▆▇▇▇▇▇▇  (min 1661, max 3995)
rss MB      ▃▅▇▇▇▇▇▇▇▇▅▆▇▇▇▇▄▅▆▇▇▇▇▇▄▆▇▇▇▇▇▃▆▆▆▇▇▇▅▆▆▆▇▇▇▇▆▆▆▆▇▇▇▅▆▇▇▇█  (min 76.6, max 165.8)
goroutines  ▅▇▇▇▇█▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▄▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▄▇▇▇▇▇▇  (min 2771, max 5318)
cpu cores  ▁▃▄▁▁▁▁▁▂██▁▄ ▄▂▁▁ ▅ ▄ ▂▇▄▄▃▁▄▁██ ▃ ▃ ▁ ▄ ▄▁▁▇▇ ▄ ▄▁█▇▁▃▁▄▁▁  (min 0.1, max 0.23)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 240 | 3988 | 15021 | 17.2 | 12.0 | 25.1 | 42.3 | 28.8 | True |
| 2 | 420 | 3989 | 41 | 2.2 | 2.5 | 6.0 | 8.3 | 9.0 | True |
| 3 | 600 | 3992 | 15021 | 17.2 | 12.8 | 26.6 | 43.8 | 33.8 | True |
| 4 | 780 | 3990 | 15024 | 17.2 | 11.8 | 25.5 | 42.8 | 27.8 | True |
| 5 | 960 | 3987 | 42 | 2.3 | 2.5 | 6.0 | 8.3 | 7.5 | True |
| 6 | 1140 | 3991 | 15026 | 17.2 | 12.0 | 24.8 | 42.0 | 27.8 | True |
| 7 | 1320 | 3989 | 15024 | 17.2 | 12.5 | 25.3 | 42.5 | 29.1 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 240 | 24 | 159 | 160 | +0.2 | -12 | 1.32 | +0 |
| 330 | 420 | 17 | 155 | 157 | -5.5 | +18 | 1.32 | +11 |
| 510 | 600 | 17 | 156 | 155 | -1.8 | +27 | 1.31 | +15 |
| 690 | 780 | 17 | 160 | 164 | +3.5 | +9 | 1.32 | +0 |
| 870 | 960 | 17 | 153 | 156 | +3.7 | -5 | 1.31 | +2 |
| 1050 | 1140 | 17 | 153 | 165 | +11.5 | -15 | 1.31 | +0 |
| 1230 | 1320 | 17 | 153 | 157 | +3.4 | -3 | 1.32 | -0 |
| 1410 | 1500 | 17 | 151 | 166 | +6.3 | +46 | 1.31 | +29 |

### gateway

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 25.0 min; chargers up (client side, after ramp) median 3996
- Server RSS MB: min 246, max 394; CPU cores mean 0.31, max 1.07; threads max 42; fds max 4737; goroutines max 5318; generator RSS max 305 MB
- Canary heartbeat max RTT ms (steady): 7.0; /control/health ms max (steady): 3.3
- CSMS calls: not_connected=324, ok=44427, other=642, timeout=28999
  - shares: not_connected 0.4%, ok 59.7%, other 0.9%, timeout 39.0%
  - call latency ms (p50/p99): not_connected 0.7/1.4, ok 0.9/1.5, other 20679.8/29967.2, timeout 30024.2/30024.2
- Charger request errors: {'BootNotification/closed': 12}; connect errors: {'bad_status': 4950, 'refused': 1057}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up          ▅▇▇▇▇▇▇█▇█▇█▇▇▇▇▇▇▇▇▇█▇▇▇▇▇█▇█▇▇▇██▇▇▇▇▇▇█▇█▇▇▇▇██▇█▇▇▇▇▇█▇  (min 1668, max 4000)
rss MB      ▂▃▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▅▅▅▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▅▇█▃▄▄▄▄▄▄▄▄▄▄▄  (min 185.9, max 394.2)
goroutines  ▅▇▇▇▇█▇▇▇▅▇▇▇▇▇▇▇▇▇▇▇▇▇▄▇▇▇▇▇▇▇▆▇▇▇▇▇▇▇▇▇▇▇▇▇▅▇▇▇▇▇▇▄▇▇▇▇▇▇  (min 2773, max 5318)
cpu cores  ▁▂▂                     ▄                    ▇█ ▁             (min 0.29, max 1.07)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 240 | 3993 | 3 | 2.0 | 0.0 | 0.0 | 2.0 | 0.0 | True |
| 2 | 420 | 3997 | 2 | 2.0 | 0.0 | 0.0 | 2.0 | 0.5 | True |
| 3 | 600 | 3998 | 60005 | 62.0 | 0.0 | 0.0 | 62.0 | 12.8 | True |
| 4 | 780 | 3999 | 3 | 2.0 | 0.0 | 0.0 | 2.0 | 0.0 | True |
| 5 | 960 | 3996 | 2 | 2.0 | 0.0 | 0.0 | 2.0 | 0.0 | True |
| 6 | 1140 | 4000 | 60015 | 62.0 | 0.0 | 1.8 | 63.8 | 0.0 | True |
| 7 | 1320 | 3999 | 2 | 2.0 | 0.0 | 0.0 | 2.0 | 0.0 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 120 | 240 | 24 | 294 | 299 | +2.2 | -12 | 1.32 | +9 |
| 330 | 420 | 17 | 296 | 297 | +0.7 | +18 | 1.31 | +6 |
| 510 | 600 | 17 | 295 | 296 | +0.8 | +27 | 1.30 | +4 |
| 690 | 780 | 17 | 290 | 295 | +3.6 | +9 | 1.31 | +9 |
| 870 | 960 | 17 | 295 | 296 | +0.9 | -5 | 1.30 | +1 |
| 1050 | 1140 | 17 | 295 | 295 | +0.1 | -15 | 1.31 | +4 |
| 1230 | 1320 | 17 | 295 | 299 | +3.8 | -2 | 1.32 | +3 |
| 1410 | 1500 | 17 | 297 | 298 | +1.0 | +46 | 1.31 | +4 |

### gateway-10k-rolling-drain

- Freeze detected: **no**; server crashes: 0; aborted: False 
- Duration 8.0 min; chargers up (client side, after ramp) median 9988
- Server RSS MB: min 426, max 662; CPU cores mean 0.71, max 1.78; threads max 51; fds max 10360; goroutines max -1; generator RSS max 705 MB
- Canary heartbeat max RTT ms (steady): 5.0; /control/health ms max (steady): 0.7
- CSMS calls: not_connected=91, ok=5723, other=248, timeout=3309
  - shares: not_connected 1.0%, ok 61.1%, other 2.6%, timeout 35.3%
  - call latency ms (p50/p99): not_connected 0.8/1.4, ok 0.9/1.7, other 23161.4/29996.2, timeout 30002.7/30002.7
- Charger request errors: {'BootNotification/closed': 7, 'StatusNotification/closed': 1}; connect errors: {'bad_status': 7716, 'refused': 518}
- Server log pattern counts: none

Timeline (client side chargers up / server RSS MB / server goroutines / CPU cores):
```
up           ▁▂▃▃▄▅▆▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  (min 551, max 1e+04)
rss MB        ▁▁▁▂▂▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄▅▅▅▅▅▅▆▄▄▄▄▄▅▅▆▆▆▆▆▄▄▄▄▄▅▆▇▇▇▇█▄▄▄▄▄  (min 162.6, max 661.9)
goroutines                                                               (min 0, max 0)
cpu cores   ▂▂▂▃▃▃▃▄▃▃▄▂▂▂▃▂▂▂▃▂▂▂▃▅▅▃▂▂▂▃▂▂▂▂▂▆▆▆▂▂▂▂▂▂▂▂▂▇█▅▂▂▂▂▂▂▂▂▂  (min 0, max 1.78)
```

Restarts (kill -TERM, 2 s gap, start; times from server healthy):

| # | at s | pre-up | exit ms | ready after kill s | t90 s | t99 s | t99 from kill s | all back s | reached 99% |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 188 | 9997 | 60007 | 62.0 | 0.0 | 0.0 | 62.0 | 3.3 | True |
| 2 | 285 | 9997 | 60012 | 62.0 | 0.0 | 0.0 | 62.0 | 3.3 | True |
| 3 | 383 | 9999 | 60015 | 62.0 | 0.0 | 0.0 | 62.0 | 5.3 | True |

Steady segments between restarts (leak check: slopes via least squares):

| from s | to s | samples | RSS start | RSS end | RSS MB/min | goroutines/min | goroutines per conn | fds/min |
|---|---|---|---|---|---|---|---|---|
| 150 | 188 | 7 | 470 | 473 | +4.4 | +nan | nan | +38 |

## High-rate columns (second half of every step)

alloc KB/req divides the server's heap allocation by completed charger requests (each is one CALL and one CALLRESULT). The in-mark share is the wall time inside GC mark phases from `GODEBUG=gctrace=1` (run the harness with `-gctrace` or `-knee`). Scheduler latency is the p99 bucket of `/sched/latencies:seconds` per 5 s sample.

| run | N | msg speed x | msgs/s | CPU cores | max RSS MB | alloc MB/s | alloc KB/req | GC cycles/s | in-mark share (gctrace) | GC CPU cores | GC assist cores | sched latency p99 ms (max window) | heap objects + stacks MB | Heartbeat p50 / p95 / p99 ms |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| embedded | 4000 | 1 | 301 | 0.12 | 166 | 3 | 12.06 | 0.08 | - | 0.00 | 0.00 | 3.67 | 68 + 30 | 0.3 / 0.4 / 0.5 |
| gateway | 4000 | 1 | 292 | 0.31 | 394 | 1 | 2.02 | 0.04 | - | 0.00 | 0.00 | 0.66 | 20 + 10 | 0.5 / 0.9 / 1.1 |
| gateway-10k-rolling-drain | 10000 | 1 | 847 | 0.72 | 662 | 1 | 1.18 | 0.10 | - | 0.00 | 0.00 | 0.33 | 15 + 5 | 0.5 / 0.9 / 1.0 |
