# Rolling restart: SIGTERM versus Server.Drain (2026-11-21)

`go run ./drain` (see `drain/main.go`): 4,000 simulated OCPP 1.6J chargers are
connected through an L4 balancer to an old cs-gocpp process; a second (new)
process is ready. The balancer sends new connections to the first backend whose
`/ready` answers 200. The old process gets SIGTERM:

* `kill`: it exits at once (what a plain SIGTERM does without Drain);
* `drain`: `cs-gocpp -drain-window W` runs `Server.Drain` (rate = 4000 / 0.9W).

Chargers reconnect with jittered exponential back-off (2 s start, 30 s cap).
Old and new server each pinned to 2 cores, the generator to 2 other cores
(`taskset`), no TLS, no auth, lenient schema, `-lite`. Machine shared with other
jobs (load 2 to 7 during the runs).

| run | connect attempts at new, peak /s | upgrades /s, peak | new server CPU, peak (250 ms) | fleet back to 99% | min connected |
|---|---|---|---|---|---|
| kill-1 | 2007 | 2007 | 0.40 cores | 3.2 s | 0 |
| kill-2 | 1972 | 1969 | 0.32 cores | 3.2 s | 0 |
| drain, window 60 s (target 74/s), runs 1 and 2 | 90 / 92 | 90 / 91 | 0.12 cores | 55.5 / 55.7 s | 3839 / 3841 |
| drain, window 30 s (target 148/s) | 161 | 161 | 0.12 cores | 29.1 s | 3683 |
| kill + `-accept-rate 200` on the new server (burst 400) | 984 | 594 | 0.24 cores | 33.3 s | 0 |

`timeline.csv` has the per-second curve (connected, connect attempts at the new
server, upgrades, drops, CPU of both servers). The CPU-seconds in `summary.json` are
not comparable between modes: the observation window is as long as the drain.
