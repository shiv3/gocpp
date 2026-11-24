# Staged releases: backend canary

Status: phase 7a. Design: [design/gateway.md](design/gateway.md) section 5. The
gateway canary (partial drain, rebalance) is phase 7b; automatic promotion is the
optional phase 7c.

A backend canary is a *routing decision at the gateway*. Backends attach with a
pool name; the gateway asks its router which pool serves each message; a rollout
is a sequence of policy revisions. No socket is touched, so chargers never notice.

**Promotion is external.** The gateway only ever *suspends* a rule (its local
guard); every promote or rollback step is written by a person or a tool with
`gocppctl route` (or, optionally, by `addons/rollout`).

## 1. Entry criteria (read this first)

Backends share the application's stores, so a transaction can span the stable and
the canary version: through a rollback, through a reconnect, through an
action-scoped rule. gocpp cannot check this for you. Before you start a canary:

- Versions N and N+1 both read what the other writes. Use expand/contract
  migrations: add in N+1, stop using in N+2.
- Transaction messages are idempotent by content (a redelivered `StopTransaction`
  or `TransactionEvent` is harmless). The gateway never redispatches after a
  message may have run, but a charger retries.
- N+1 answers the CALLs N answers, with the same CALLERROR behaviour, unless you
  deliberately route an action only to the canary (section 4).
- The canary pool can serve the whole `default` pool's load if the rule is later
  raised to 100%, or you promote by rolling the stable Deployment instead (section 5).

## 2. Deploy the canary pool (no traffic yet)

Run a second Deployment of the same image family with its own pool and version:

```go
csms.NewServer(csms.WithGatewayLink(link),
    csms.WithBackendInfo(gateway.BackendInfo{Pool: os.Getenv("GOCPP_POOL"), Version: buildVersion}))
```

On GKE: `backend-canary`, 1-2 replicas, `GOCPP_POOL=canary`; both Deployments dial
every gateway. **A gateway without a policy sends work only to the `default`
pool**, so starting the canary changes nothing. Check
`gocpp_gateway_pool_members{pool="canary"}` is non-zero on every gateway.

## 3. The policy

```yaml
revision: 0            # ignored by apply: the CLI writes stored revision + 1
default: default       # pool for everything no rule matches
salt: rollout-0-9-3    # percentage hash salt: keep it for all steps of one rollout
rules:                 # first match wins
  - name: lab
    pool: canary
    match:
      attrs: { label.canary: ["1"] }          # AND across keys, OR across values, "*" suffix = prefix
  - name: step
    pool: canary
    match:
      attrs: { tenant: ["t1"], firmware: ["2.1.*"] }
      ocpp: ["1.6"]
      cp_ids: []                               # non-empty: only these ids
      percent: 5                               # of the chargers passing the matchers above
    actions: []                                # empty = all actions
    on_unavailable: fallback                   # fallback (to `default`) | error
    hold: busy                                 # none | busy
    suspend_for: 60s                           # after a fallback (default 60s)
    guard: { min_requests: 200, max_error_ratio: 0.05, window: 60s }
```

Semantics (fixed so that tools can reproduce them):

- **Percentage**: `bucket = uint64_be(SHA-256(salt || 0x00 || cpID)[0:8]) mod 10000`;
  the charger is in when `bucket < percent*100`. Raising the percentage only adds
  chargers, lowering only removes. Use a new salt for the next rollout so the risk
  falls on different chargers. `routepolicy.Bucket` computes it.
- **Sticky**: a decision is a pure function of the revision, charger id, attributes,
  OCPP version and action; two gateways on one revision agree.
- **Attributes** come from `ConnectReply.Attrs` and `Conn.SetAttrs` (backend side).
  `$connect` has no attributes, so attribute rules do not match it; use `cp_ids` and
  `percent` for rules that must hold across reconnects, or put the label in the
  connect-time attributes.
- **Action rules** route only the listed actions; the validator refuses an action
  set that splits a transaction group (1.6: Authorize, StartTransaction,
  StopTransaction, MeterValues, StatusNotification; 2.x: Authorize,
  TransactionEvent, MeterValues, StatusNotification) unless `allow_split: true`.
- **hold: busy**: a session whose `gocpp.busy` attribute is set keeps the membership
  it had before, until the attribute is cleared, the session ends or `max_hold`
  (default 4h) passes. The backend sets `gocpp.busy` with `SetAttrs` when a
  transaction starts and clears it when it ends. Holding is a comfort, not the
  correctness mechanism (section 1).
- **Unavailable canary** (`on_unavailable: fallback`): no member attached or all
  Nack: the message goes to `default` at once (no 20 s hold) and the rule is
  suspended on that gateway for `suspend_for`, so a flapping pool moves a charger
  at most once per window. `error`: hold, then CALLERROR InternalError.
- **guard**: more failed outcomes (`timeout`, `stream_broken`, CALLERROR
  `InternalError`) than `max_error_ratio` over `window` (after `min_requests`)
  suspends the rule until the next revision; the lease label `suspended` says so.
  It is deliberately crude; comparing canary against stable is the promoter's job
  (section 6).

## 4. Operate with `gocppctl route`

`gocppctl` talks to the session registry (`--redis-addr`, or
`GOCPPCTL_REDIS_ADDR`; `--prefix` if you changed the registry key prefix).

```sh
gocppctl route validate policy.yaml           # offline: schema, split lint, known pools
gocppctl route apply policy.yaml --expect-rev 7
gocppctl route status
gocppctl route show --history
gocppctl route rollback                       # previous revision, immediately
```

- `apply` writes revision stored+1 with compare-and-set (`--expect-rev` guards
  against a concurrent operator) and then **waits until every gateway lease reports
  the revision** (`--wait`, default 30s). If one does not, it prints the lagging
  gateways and exits 3: it never reports success it cannot see. `status` exits 3
  while gateways differ.
- `apply` refuses a policy with rules while a gateway lease lacks the `route.v1`
  capability (it would ignore the policy); `--force` overrides.
- `rollback` writes the contents of the previous (or `--to`) revision as a *new*
  revision with holds ignored: every session is back on `default` from its next
  message. Handlers already running on the canary finish; nothing is redispatched.
- A gateway that cannot reach the registry keeps its last policy; one that starts
  without any policy routes to `default` and raises
  `gocpp_gateway_route_policy_missing`. A policy that fails validation is refused
  by the gateway (`gocpp_gateway_route_policy_rejected_total`) and the previous
  one stays.
- For development or a GitOps-only setup a gateway can read a file instead:
  `--route-policy-file`. With a registry both apply; the higher revision wins.

A typical label-based staged release (labels live in your application; its connect
handler copies the release-relevant ones into `Attrs` as `label.<name>=1`):

1. rule `lab` on `label.canary` (internal sites): `apply`, soak.
2. rule `step` at `percent: 1`, then 10, 50, 100, one revision each, same salt.
3. promote (section 5).

A canary that only understands a new action: `actions: [NewAction]`,
`on_unavailable: error`, so the stable pool never answers `NotImplemented` for
chargers in the rule.

## 5. Promote and roll back

- **Rolling form** (default): raise to `percent: 100`, roll the stable Deployment to
  the new version (Nack and redispatch, zero disconnects), then remove the rule
  and scale the canary to 0.
- **Blue/green**: one revision with `default: canary`, then retire the old pool.
- **Rollback**: `gocppctl route rollback`, or a revision with `percent: 0`.

## 6. What to watch

Metrics on every gateway (constant labels `gateway_id`, `gateway_version`):

| Metric | Use |
|---|---|
| `gocpp_gateway_upstream_requests_total{pool,backend_version,action,outcome}` | canary vs stable failed-outcome ratio and CALLERROR mix |
| `gocpp_gateway_upstream_seconds{pool,backend_version,action}` | latency p99 per pool |
| `gocpp_gateway_late_replies_total{pool}` | backend answers that arrived after the gateway gave up (dropped); canary above stable is a latency or hang signal (rollout gate `late_orphan`) |
| `gocpp_gateway_orphan_results_total{pool,outcome}` | answers to CSMS calls whose caller was gone, by caller pool; `fallback`/`dropped` mean the caller pool had no backend (canary crashed) |
| `gocpp_gateway_route_decisions_total{rule,pool,rev}` | how much traffic each rule moves |
| `gocpp_gateway_route_fallback_total{rule,reason}` | canary unavailable (`unavailable`) or refusing (`nack`) |
| `gocpp_gateway_route_suspended{rule}` | 1 while a rule is suspended on this gateway |
| `gocpp_gateway_route_held_sessions{rule}` | busy sessions held back by `hold: busy` |
| `gocpp_gateway_route_policy_revision`, `..._policy_missing`, `..._policy_rejected_total` | convergence and policy health |
| `gocpp_gateway_pool_members{pool,backend_version}` | canary attached everywhere |

Promotion criteria (same actions, canary vs default, same window, at least 500
canary requests per evaluated action group): failed-outcome ratio not above stable
+ 0.5 pp; no CALLERROR code whose share exceeds stable's by more than 1 pp; p99
`upstream_seconds` not above 1.5x stable; plus your own application checks. The
controller can also require `late_replies` and `orphan_results` per request not above
stable (`late_orphan: true` in the plan, docs/rollout.md).

## 7. Measured: rollout under load

The load-test harness (`interop/loadtest/rollout/run.sh`) ran
the whole procedure on the split topology: 4,000 chargers (40% mute, 15% transactions),
2 gateways, backend pools `default` x3 and `canary` x2, 50 CSMS calls/s, pinned to 6
cores. The per-phase tables of that run are not kept in the repository; `run.sh` reproduces them.

| | result |
|---|---|
| manual steps 1% -> 10% -> 50% -> 100%, then back (`route apply --wait`) | canary share 0.9 / 9.4 / 48.7 / 100% (within 3 sigma of nominal); each charger changed pool exactly once on the way up, none went back before the rollback, all 4,000 were back on `default` after it |
| time until every gateway reported the new revision | 14-140 ms |
| disconnects, connect errors, charger-visible errors (manual and healthy auto rollout) | none; 4,000 chargers up throughout |
| `gocppctl rollout run`, healthy canary | four stages, `done: ready to promote` in about 5 minutes (60 s soak per stage) |
| `gocppctl rollout run`, canary answering 30% of requests with `InternalError` | rolled back 20 s after the 1% stage was applied: 40 CALLERRORs on the 33 chargers that were on the canary, none after the rollback |
| upstream latency as the gateway sees it | p50 0.25-0.28 ms in both pools, no difference between canary and default |
| cost of the policy on the gateway | evaluation 130-185 ns per message and 0 allocations (`BenchmarkRoute`), `hold: busy` 183 ns single-core and about 280 ns per decision when 6 cores contend for the session-state lock; end to end the gateway's CPU per request (about 0.4 ms) did not change between no policy, 1%, 10%, 50% and 100% |

Caveats: the machine was shared, so isolated 10 s windows show a Heartbeat p99 of 18-32 ms
in every kind of phase (none at a policy step; the median window p99 is about 1 ms); the
healthy canary is the same binary as stable, so the run shows the mechanics and the safety net,
not what your new version will do. Entry criteria and the rest of the runbook above still apply.

## 8. Library use

```go
engine := routepolicy.NewEngine(routepolicy.WithMetrics(m),
    routepolicy.WithOnApply(func(rev uint64) { srv.SetGatewayLabel(ctx, "policy_rev", strconv.FormatUint(rev, 10)) }))
link := linkgrpc.NewGateway(linkgrpc.WithRouteObserver(engine))     // or inproc.WithRouteObserver
gw := csms.NewGatewayServer(link, csms.WithRouter(engine))
go engine.Watch(ctx, routepolicy.StoreSource{Store: registry})      // or routepolicy.FileSource{Path: ...}
```

`routepolicy.Controller` is what the CLI uses (`Put`, `Rollback`, `Status`,
`WaitConverged`) and what a progressive-delivery tool can embed.
