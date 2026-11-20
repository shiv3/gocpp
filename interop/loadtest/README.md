# Large-fleet load/soak harness

`loadtest` supervises one of the parity CSMS binaries (`../parity/cs-gocpp` or
`../parity/cs-ocppgo`), simulates N OCPP 1.6J chargers against it, drives
CSMS-initiated calls through `/control/call`, restarts the server on a
schedule, and records connection counts, per-request latency, call outcomes and
server resources (RSS/CPU/threads/fds from `/proc`, goroutines from pprof).

```sh
(cd ../parity/cs-gocpp  && go build -o /tmp/lt/cs-gocpp .)
(cd ../parity/cs-ocppgo && go build -o /tmp/lt/cs-ocppgo .)
go build -o /tmp/lt/loadtest .
/tmp/lt/loadtest -server-bin /tmp/lt/cs-gocpp -taskset 0-3 -out results/run1 \
  -steps 4000:50:25m -restarts 5 -call-timeout 30s      # add -server-args=-compat for the compat preset
python3 report.py results/<date>                          # writes report.md from every run dir
```

Notable flags: `-steps N:callRate:dur[:speed],...` (cumulative fleet size per
step; `speed` scales heartbeat/meter timers), `-mute 0.4`, `-flappers 3`,
`-dups 20` (two clients with the same id), `-hb 30s` (compressed from 60 s),
`-tx-frac 0.15`, `-hot N -hot-share 0.9` (aim most calls at N mute chargers),
`-restarts K` (SIGTERM, 2 s gap, start; chargers reconnect with jittered
backoff), `-min-avail-mb` (abort when the machine runs low on memory).

Freeze detection: two canary chargers send a Heartbeat every second; a
round trip over 10 s (or three stalled `/control/health` polls) is a freeze.
The harness then saves a goroutine dump from the server's pprof endpoint to
`dumps/`, and after 90 s of freeze sends SIGQUIT to get the stacks in
`server.log` and restarts the server.

The parity servers take `-lite` (keep no event history, counters only, see
`/control/count`) and `-pprof ADDR` for this harness. Raw CSV/logs are
git-ignored; `summary.json` and `report.md` are committed.
