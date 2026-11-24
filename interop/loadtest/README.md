# Large-fleet load/soak harness

`loadtest` supervises one of the parity CSMS binaries (`../parity/cs-gocpp` or
`../parity/cs-ocppgo-upstream` or `../parity/cs-ocppgo-compat`), simulates N OCPP 1.6J chargers against it, drives
CSMS-initiated calls through `/control/call`, restarts the server on a
schedule, and records connection counts, per-request latency, call outcomes and
server resources (RSS/CPU/threads/fds from `/proc`, goroutines from pprof).

```sh
(cd ../parity/cs-gocpp  && go build -o /tmp/lt/cs-gocpp .)
(cd ../parity/cs-ocppgo-upstream && go build -o /tmp/lt/cs-ocppgo-upstream .)
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
`/control/count`) and `-pprof ADDR` for this harness. Run output (`results/`) is git-ignored.

## High-rate runs (allocation, GC, scheduler)

Both parity servers expose `GET /control/runtime` (runtime/metrics: heap allocs,
GC cycles, GC assist/total CPU, heap objects, stacks, scheduler-latency histogram).
The sampler turns it into timeline columns `alloc_mb_s`, `alloc_kb_req` (heap
allocation per completed charger request), `gc_cycles_s`, `gc_cpu_cores`,
`gc_assist_cores`, `sched_p99_ms`, `heap_obj_mb`, `stacks_mb` (-1 when unavailable).
`-gctrace` starts the server with `GODEBUG=gctrace=1`; `report.py` then prints the
GC mark-phase share of wall time per step ("High-rate columns"). `-knee` is the
preset `-steps 4000:50:150s:160,4000:50:150s:320` (about 30k, then 60k requests/s)
with `-gctrace`. Pin server and generator to disjoint cores, for example
`-taskset 14-16 -server-procs 3` for the server and `taskset -c 17-19` for the harness.

## Split topology (gateway mode)

`-gateways G` runs the split deployment instead of one server: G `cmd/gocpp-gateway`
processes, backends (`../parity/cs-gocpp -mode backend`) in named pools and a
Redis-compatible session registry (an in-process miniredis in the harness).
Chargers connect to a random gateway on every (re)connect, like an L4 load
balancer; the first backend also serves the `/control` API at `-addr`.

```sh
(cd ../parity/cs-gocpp && go build -o /tmp/lt/cs-gocpp .)
(cd ../../cmd/gocpp-gateway && go build -o /tmp/lt/gocpp-gateway .)
go build -o /tmp/lt/loadtest .
/tmp/lt/loadtest -server-bin /tmp/lt/cs-gocpp -gateway-bin /tmp/lt/gocpp-gateway \
  -gateways 2 -backend-pools default:3,canary:1 \
  -gw-taskset 8-9 -be-taskset 10-13 -server-procs 2 -out results/split1 \
  -steps 4000:50:6m -restarts 5 -restart-target backend -restart-mode term
```

- `-backend-pools name:count,...`: backends per pool (the `default` pool is the
  one gateways without a routing policy send work to; other pools attach and
  receive nothing).
- `-restart-target server|backend|gateway|pool:NAME` and `-restart-mode term|kill`
  choose what `-restarts K` acts on: SIGTERM drains (backend: Nack + redispatch,
  gateway: paced 1012 closes), kill is SIGKILL. The recovery numbers are the same
  as for a single server (`chargers_up` back to 99%).
- Per-process resources go to `procs.csv` (`t_s,name,role,pool,pid,rss_mb,cpu_cores,
  threads,fds`); `timeline.csv` columns keep their meaning, with `server_connected`
  the sum of the gateways' connection gauges.
- `-gw-taskset`, `-be-taskset`, `-gw-args`, `-server-args` pin and tune the tiers.

Partial-drain / soft-cap scenarios (a gateway canary fill) are driven with the
gateway flags `--soft-max-sessions` and the `Server.Drain` options and are not
automated here; the load balancer behaviours they rely on need a real cluster
(see `examples/gke-gateway/README.md`).
