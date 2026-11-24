# Staged rollout: switching an ocpp-go server to the gocpp runtime

This is the plan for an application that runs a central system on
`github.com/lorenzodonini/ocpp-go` (upstream, API line `v0.19`) and wants the
gocpp runtime under it without changing its code. The client side of ocpp-go is
unchanged by this module and is not part of the rollout.

## 0. What changes, what does not

- One line in `go.mod`; no source change:

  ```
  replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.N
  ```

  Pick `N` from the releases of this module. The replacement requires gocpp
  itself; `go mod tidy` adds it and raises some `golang.org/x/*` versions. The
  minimum Go version is that of the module's `go.mod`.
- Servers built with `ocpp16.NewCentralSystem(nil, srv)` or
  `ocppj.NewServer(srv, nil, ...)` run on gocpp. A server that injects its own
  `ServerDispatcher` keeps ocpp-go's original code (see README, "Choosing the runtime").
- Behaviour differences are listed in the README ("Known differences"). The ones a
  rollout can notice: a panicking handler is answered with `InternalError` and the
  process survives (upstream ocpp-go crashes the process); outbound requests are ordered per charger, not
  globally; idle connections get a close frame 1000 instead of a TCP drop; upgrades
  during shutdown get HTTP 503.

## 1. Before any traffic

1. Build the application with the `replace` line in a branch. `go build ./...`,
   `go vet ./...`, then its own test suite. Any compile error is a bug in this
   module's API compatibility: report it (CI diffs the exported API against the
   pinned tag, so there should be none).
2. Run the tests that exercise the OCPP layer with
   `OCPPGO_COMPAT_RUNTIME=gocpp` (also routes an injected default dispatcher to
   gocpp) and once with `OCPPGO_COMPAT_RUNTIME=legacy` (the original code) to
   confirm the replace line alone changes nothing on the legacy path.
3. Add operations wiring (optional but recommended before production; see section 4):
   metrics, readiness, drain on shutdown.
4. Load test with the application's own simulator at the production fleet size:
   a connect storm, a mass call timeout (mute chargers) and a restart. The
   expectations from the module's own runs are in `interop/loadtest/results/compat-*` of the gocpp repository:
   no freeze, memory in line with gocpp's own server.

## 2. Canary

1. Deploy the build with the `replace` line to one instance (or one shard) that
   takes a small share of chargers (1-5 %), the rest of the fleet staying on the
   current build. Keep `OCPPGO_COMPAT_RUNTIME` unset on the canary.
2. Hold for at least one full business cycle of the fleet (one day with a peak,
   including a restart of the canary). Then 25 %, 50 %, 100 %, each held long
   enough to see a peak.
3. Advance only while every item of section 3 is within its limits.

## 3. What to watch

Compare canary with the baseline instances, not with absolute numbers.

| Signal | Source | Expect |
|---|---|---|
| Connected chargers per instance, reconnect rate | `observability.Metrics.ConnectionCount`, `SessionMetrics.SessionClosed` (code and cause) | same as baseline; close codes 1000/1008/1012 as documented, rate of `dropped` not higher |
| Upgrade results | `SessionMetrics.UpgradeResult` | mostly `ok`; `full` / `rate_limited` only if you set limits |
| Request latency and failures per action | `CallCompleted` (inbound and outbound), the application's own timers | p99 not worse; no growth of `error` / `timeout` status |
| Outbound request outcomes | the callbacks' errors, `Request timed out` count | unchanged |
| Handler panics | the application log: `handler panic recovered` | none expected; any is an application bug that used to kill the process |
| Goroutines, RSS, CPU, GC | runtime metrics | goroutines roughly one per connection at idle; RSS no higher than the baseline |
| Malformed frames | `InvalidMessageHook` calls, error log lines | same per-charger pattern as baseline |
| Freeze signal | a canary charger or the application's own heartbeat round trip | no round trip over 10 s |

## 4. Operating hooks (the `compat` package)

```go
srv := ws.NewServer()
endpoint := ocppj.NewServer(srv, nil, nil, core.Profile /* all profiles in use */)
compat.SetObservability(endpoint, metrics, tracerProvider, nil) // metrics: gocpp prom/otel
compat.SetAdmission(srv, compat.Admission{MaxConnections: 8000, AcceptRate: 200, AcceptBurst: 400, SoftMaxConnections: 6000})
cs := ocpp16.NewCentralSystem(endpoint, srv)
srv.AddHttpHandler("/ready", compat.ReadyHandler(srv).ServeHTTP)
// on SIGTERM:
//   compat.Drain(ctx, srv, compat.DrainOptions{Window: 5 * time.Minute}); srv.Stop()
```

Use `Drain` in the deploy procedure of the canary and later of every instance:
it moves the chargers off at a fixed rate with close code 1012, which avoids the
reconnect storm a plain `Stop` causes.

## 5. Rollback

Rollback never needs a code change, in order of speed:

1. **Per instance, runtime switch** (restart the process): set
   `OCPPGO_COMPAT_RUNTIME=legacy`. The module keeps compiling and runs ocpp-go's
   original server code (`ocppj` dispatcher, gorilla-based behaviour of the
   request path). Use it first when the symptom is in request handling.
   Note: the module's own `ws.Server` (coder/websocket) is still used under
   the legacy runtime; if the symptom is connection handling, use 2.
2. **Per instance, previous build**: redeploy the previous image (the `go.mod`
   without the replace line). This is the full rollback and the one to use when
   in doubt.
3. Roll back the instance that shows the symptom first; the canary design means
   the others are still on the old build.

After a rollback, keep the instance's logs, a goroutine dump (if it froze) and
the metrics of the window and open an issue with the module version.

## 6. Done criteria

100 % on the new build for one full cycle without a rollback, the old build
retired, `OCPPGO_COMPAT_RUNTIME` not set anywhere. From then on, upgrades of this
module follow the same canary, shortened: the API line (`v0.19.0`) does not move,
only `-gocpp.N`.
