# Rollout under load: routing policy and automatic promotion (2026-11-21)

Topology: 2 gateways (`cmd/gocpp-gateway`), 5 backends (`cs-gocpp -mode backend`): pool `default` x3 and pool `canary` x2, a Redis-compatible
registry (in-process miniredis, also the policy store), 4,000 chargers (OCPP 1.6J, 40% mute, 15% doing transactions, heartbeat every 15 s,
no flappers, no duplicate ids), 50 CSMS-initiated calls/s from the control backend (default pool) into the whole fleet. Pinned to
6 cores: generator 14-15, gateways 16-17, backends 18-19 (another acceptance run was not running; `uptime` load average 1.4).
Binaries from `integ/gateway` at #62 plus the harness changes of this branch (`-hook`, `-pool-args`, `-handled-log`, `-fail-rate`).
Reproduce with `rollout/run.sh OUTDIR` and `python3 rollout_report.py OUTDIR/<run>`.

Three runs:

- **manual**: 90 s baseline without a policy, then `gocppctl route apply --wait` with 1% -> 10% -> 50% -> 100% every 2 minutes (the policy is one
  `percent` rule to the `canary` pool, salt `rollout-lt`), then an empty policy (everything back to `default`).
- **auto-healthy**: `gocppctl rollout run` (`rollout/plan-auto.yaml`: stages 1/10/50/100%, 60 s soak, 10 s evaluation interval, 30 s window,
  minimum 100 canary requests, gateway `/metrics` as the source) with a healthy canary.
- **auto-error**: the same plan with the canary backends answering 30% of Heartbeat / MeterValues / StatusNotification with an `InternalError` CALLERROR
  (`cs-gocpp -fail-rate 0.3`).

How routing is judged: every backend writes `handled-<name>.log` (`unix_ms charger action` for Heartbeat / MeterValues). From the Heartbeat
lines the report rebuilds, per charger, the sequence of pools that served it. "Sticky" means a charger changes pool at most once while the
percentage only grows (default -> canary) and once more at the rollback. The share per stage is the last pool seen per charger in the stage
(excluding its first 25 s, so the revision has been applied and each charger has had a heartbeat), compared with the nominal percentage within 3 sigma of
a binomial. Latency per pool is the gateway's own `upstream_seconds` histogram (backend round trip as the gateway sees it), differenced between the phase edges.
"Charger request errors" are what the simulated chargers counted (a CALLERROR to a request is counted as `callerror`; the harness charger reconnects after one).


## Summary

| | manual | auto-healthy | auto-error |
|---|---|---|---|
| canary share vs nominal (1/10/50/100%) | 0.9 / 9.4 / 48.7 / 100% | same chargers, same shares | 0.9% (33 chargers) at 1% |
| chargers that changed pool more than once before the rollback / flipped back | 0 / 0 | 0 / 0 (exactly one change each) | 0 / 0 |
| after the rollback, chargers still on the canary | 0 of 4,000 | - | 0 of 4,000 |
| policy revision active on every gateway after `route apply --wait` | 14-140 ms | (controller waited) | (controller waited) |
| disconnects, connect errors, freezes | none (4,000 up throughout) | none | none (3,997 up at the lowest: the harness charger reconnects after a CALLERROR) |
| charger-visible errors | 0 | 0 | 40 CALLERRORs on the 33 chargers that were on the canary, then 0 |
| outcome | rolled back by the operator at the end | `done: ready to promote` after 4 stages (about 5 minutes) | `rolled back` 20 s after the stage was applied (failed ratio 0.21 against 0.00) |
| gateway upstream p50 per pool | 0.25-0.27 ms both pools | 0.26-0.28 ms | 0.25-0.26 ms |

Read-outs, in short:

- Routing is sticky and correct: each charger moved from `default` to `canary` exactly once as the percentage grew (the hash is monotonic), no charger moved back
  before the rollback, the observed share is within 3 sigma of the nominal percentage at every stage, and one revision back to an empty policy moved all 4,000 back.
- Applying a revision is cheap: `gocppctl route apply --wait` reports every gateway on the new revision within 14-140 ms at 4,000 sessions.
- The injected-error canary is caught at the smallest stage: 1% of the fleet (33 chargers) saw 40 CALLERRORs in 20 s; the controller rolled back at its second
  evaluation after the stage was applied (the first is the warm-up interval), and no charger saw an error after that.
- Policy evaluation is not visible end to end. Gateway CPU per request is 370-410 core-us in every phase with and without a policy (noise level;
  the 100% phase of the manual run read 450 and shares a window with an external latency spike); the CSMS call latency and the charger-side Heartbeat latency
  (p50 0.5 ms, median window p99 1.1 ms) do not change with the percentage. The micro-benchmark below puts the evaluation itself at 130-185 ns per message
  (one core), 0 allocations.
- A shared machine: isolated 10 s windows show a Heartbeat p99 of 18-32 ms (manual 570 s and 580 s; auto-healthy 220, 390, 410, 560 s). They fall on no policy step and in
  both pools; the machine carried other agents' builds and tests (load average up to 6.6). The median window p99 is 1.0-1.1 ms in every phase. They are not attributed to the gateway.
- Harness notes: `not_connected` in the call table is the harness pre-check (the control backend only knows chargers whose session events reached it);
  the CALLERROR counting was added to the charger after the first auto-error run (its controller log is kept as `hook-0.run1-before-callerror-counting.log`; that run rolled back at 30 s with 59 failed requests).
  A first manual run overlapped with another acceptance run on cores 2-5 and was discarded; all three runs here ran on cores 14-19 with no other load test running.

### Cost of evaluating the policy (micro-benchmark, `go test -bench BenchmarkRoute ./gateway/routepolicy`, one gateway core and 6 cores, i5-13600K)

| policy | 1 core ns/message | 6 goroutines ns/message (wall per op) |
|---|---:|---:|
| none (no policy loaded) | 4.0 | 0.8 |
| one `percent` rule | 131 | 25 |
| `label` + `vendor/firmware` + `percent` rules | 185 | 38 |
| `percent` with `hold: busy` | 183 | 284 (the engine's session-state lock) |

All 0 B/op, 0 allocs/op. The gateway spends about 0.4 ms of CPU per request in these runs (heartbeats at 4,000 sessions, mostly idle connections), so one
evaluation is 0.03-0.05% of it. The `hold: busy` case serialises on a mutex: about 3.5 million decisions per second across 6 cores, far above a gateway's message rate.


## Run 1: manual steps

Phases: baseline 90-180s, policy 1% 180-300s, policy 10% 300-420s, policy 50% 420-540s, policy 100% 540-660s, policy 0 (none) 660-780s

#### Routing correctness

4000 chargers with heartbeats in the handled logs.

| phase | window | chargers seen | on canary | share | nominal | within 3 sigma |
|---|---|---:|---:|---:|---:|---|
| baseline | 90-180s | 4000 | 0 | 0.0% | 0% | yes |
| policy 1% | 180-300s | 4000 | 36 | 0.9% | 1% | yes |
| policy 10% | 300-420s | 4000 | 376 | 9.4% | 10% | yes |
| policy 50% | 420-540s | 4000 | 1948 | 48.7% | 50% | yes |
| policy 100% | 540-660s | 4000 | 4000 | 100.0% | 100% | yes |
| policy 0 (none) | 660-780s | 4000 | 0 | 0.0% | 0% | yes |

Pool changes per charger over the whole run (every change is a heartbeat served by a different pool than the one before): 2: 4000.
Chargers with more than one pool change before the rollback (not sticky): 0; chargers that left the canary and came back before the rollback (flip): 0.

#### Errors and connections

| phase | window | charger request errors | chargers up (min) | server_connected (min) |
|---|---|---|---:|---:|
| baseline | 90-180s | 0 | 4000 | 4002 |
| policy 1% | 180-300s | 0 | 4000 | 4002 |
| policy 10% | 300-420s | 0 | 4000 | 4002 |
| policy 50% | 420-540s | 0 | 4000 | 4002 |
| policy 100% | 540-660s | 0 | 4000 | 4002 |
| policy 0 (none) | 660-780s | 0 | 4000 | 4002 |

Connect errors (whole run): none; freezes: None; final chargers up: 4000.

#### Upstream latency and outcomes per pool (gateway view)

| phase | pool | requests | failed | callerror InternalError | p50 ms | p99 ms |
|---|---|---:|---:|---:|---:|---:|
| baseline | default | 35303 | 0 | 0 | 0.25 | 0.73 |
| policy 1% | canary | 424 | 0 | 0 | 0.26 | 0.95 |
| policy 1% | default | 46536 | 0 | 0 | 0.25 | 0.67 |
| policy 10% | canary | 4427 | 0 | 0 | 0.26 | 0.77 |
| policy 10% | default | 42601 | 0 | 0 | 0.25 | 0.70 |
| policy 50% | canary | 22900 | 0 | 0 | 0.25 | 0.74 |
| policy 50% | default | 24103 | 0 | 0 | 0.25 | 0.72 |
| policy 100% | canary | 47076 | 0 | 0 | 0.27 | 7.55 |
| policy 100% | default | 3 | 0 | 0 | 0.25 | 0.49 |
| policy 0 (none) | canary | 2 | 0 | 0 | 0.25 | 0.49 |
| policy 0 (none) | default | 47117 | 0 | 0 | 0.26 | 0.82 |

#### Charger-side latency (harness, Heartbeat)

| phase | requests | p50 ms (n-weighted) | median window p99 ms | max window p99 ms |
|---|---:|---:|---:|---:|
| baseline | 22706 | 0.50 | 1.1 | 1.1 |
| policy 1% | 30285 | 0.48 | 1.1 | 1.1 |
| policy 10% | 30270 | 0.50 | 1.1 | 1.1 |
| policy 50% | 30258 | 0.50 | 1.1 | 1.1 |
| policy 100% | 30314 | 0.57 | 1.1 | 25.8 |
| policy 0 (none) | 30326 | 0.51 | 1.1 | 1.6 |

#### CSMS-initiated calls (issued from the control backend in the default pool to chargers in either pool)

| phase | ok | ok p50 ms | ok p99 ms (max of windows) | timeout (muted chargers) | not_connected |
|---|---:|---:|---:|---:|---:|
| baseline | 851 | 0.78 | 1.6 | 579 | 3073 |
| policy 1% | 1157 | 0.72 | 2.1 | 765 | 4086 |
| policy 10% | 1164 | 0.76 | 3.4 | 775 | 4042 |
| policy 50% | 1281 | 0.75 | 2.1 | 781 | 3939 |
| policy 100% | 1209 | 0.86 | 22.2 | 799 | 4005 |
| policy 0 (none) | 1225 | 0.78 | 2.9 | 758 | 4020 |

Whole run: calls {'not_connected': 26141, 'ok': 7801, 'timeout': 4866}, delivered {'muted_dropped': 5012, 'unmuted': 7801} (a timeout is a call to a muted charger, which drops it by design; not_connected is the harness' own pre-check: the control backend only knows the chargers whose session events it received, a third to a half of the fleet in split mode, so it is not a CSMS failure).

#### Gateway CPU per phase (added cost of policy evaluation, end to end)

| phase | gateway cores (sum of both) | requests/s | gateway core-us per request | route decisions/s |
|---|---:|---:|---:|---:|
| baseline | 0.149 | 392 | 381.2 | 0 |
| policy 1% | 0.147 | 392 | 374.8 | 392 |
| policy 10% | 0.152 | 392 | 387.4 | 392 |
| policy 50% | 0.149 | 392 | 379.3 | 391 |
| policy 100% | 0.176 | 392 | 449.9 | 392 |
| policy 0 (none) | 0.153 | 392 | 390.0 | 410 |

#### Hooks

**policy 1%** at 180s
(`route apply --wait` took 140 ms until every gateway reported the revision)
```
1795250646.559926599
revision 1 written
revision 1 is active on 2/2 gateways
1795250646.700180168
```
**policy 10%** at 300s
(`route apply --wait` took 14 ms until every gateway reported the revision)
```
1795250766.555590216
revision 2 written
revision 2 is active on 2/2 gateways
1795250766.569877080
```
**policy 50%** at 420s
(`route apply --wait` took 126 ms until every gateway reported the revision)
```
1795250886.559397917
revision 3 written
revision 3 is active on 2/2 gateways
1795250886.685541910
```
**policy 100%** at 540s
(`route apply --wait` took 28 ms until every gateway reported the revision)
```
1795251006.556393455
revision 4 written
revision 4 is active on 2/2 gateways
1795251006.584611735
```
**policy 0 (none)** at 660s
(`route apply --wait` took 115 ms until every gateway reported the revision)
```
1795251126.571522250
revision 5 written
revision 5 is active on 2/2 gateways
1795251126.686358270
```


## Run 2: automatic rollout, healthy canary

Phases: baseline 90-180s, stage 0 1% 180-250s, stage 1 10% 250-330s, stage 2 50% 330-400s, stage 3 100% 400-480s, done 480-600s

#### Routing correctness

4000 chargers with heartbeats in the handled logs.

| phase | window | chargers seen | on canary | share | nominal | within 3 sigma |
|---|---|---:|---:|---:|---:|---|
| baseline | 90-180s | 4000 | 0 | 0.0% | 0% | yes |
| stage 0 1% | 180-250s | 4000 | 36 | 0.9% | 1% | yes |
| stage 1 10% | 250-330s | 4000 | 376 | 9.4% | 10% | yes |
| stage 2 50% | 330-400s | 4000 | 1948 | 48.7% | 50% | yes |
| stage 3 100% | 400-480s | 4000 | 4000 | 100.0% | 100% | yes |
| done | 480-600s | 4000 | 4000 | 100.0% | 100% | yes |

Pool changes per charger over the whole run (every change is a heartbeat served by a different pool than the one before): 1: 4000.
Chargers with more than one pool change before the rollback (not sticky): 0; chargers that left the canary and came back before the rollback (flip): 0.

#### Errors and connections

| phase | window | charger request errors | chargers up (min) | server_connected (min) |
|---|---|---|---:|---:|
| baseline | 90-180s | 0 | 4000 | 4002 |
| stage 0 1% | 180-250s | 0 | 4000 | 4002 |
| stage 1 10% | 250-330s | 0 | 4000 | 4002 |
| stage 2 50% | 330-400s | 0 | 4000 | 4002 |
| stage 3 100% | 400-480s | 0 | 4000 | 4002 |
| done | 480-600s | 0 | 4000 | 4002 |

Connect errors (whole run): none; freezes: None; final chargers up: 4000.

#### Upstream latency and outcomes per pool (gateway view)

| phase | pool | requests | failed | callerror InternalError | p50 ms | p99 ms |
|---|---|---:|---:|---:|---:|---:|
| baseline | default | 35305 | 0 | 0 | 0.25 | 0.73 |
| stage 0 1% | canary | 242 | 0 | 0 | 0.27 | 3.59 |
| stage 0 1% | default | 27202 | 0 | 0 | 0.28 | 8.68 |
| stage 1 10% | canary | 2957 | 0 | 0 | 0.26 | 0.80 |
| stage 1 10% | default | 28289 | 0 | 0 | 0.26 | 0.75 |
| stage 2 50% | canary | 13377 | 0 | 0 | 0.28 | 11.55 |
| stage 2 50% | default | 14065 | 0 | 0 | 0.28 | 11.51 |
| stage 3 100% | canary | 31322 | 0 | 0 | 0.27 | 8.73 |
| stage 3 100% | default | 2 | 0 | 0 | 0.25 | 0.49 |
| done | canary | 47102 | 0 | 0 | 0.27 | 7.05 |
| done | default | 0 | 0 | 0 | - | - |

#### Charger-side latency (harness, Heartbeat)

| phase | requests | p50 ms (n-weighted) | median window p99 ms | max window p99 ms |
|---|---:|---:|---:|---:|
| baseline | 22707 | 0.49 | 1.0 | 1.2 |
| stage 0 1% | 20196 | 0.64 | 1.1 | 32.4 |
| stage 1 10% | 20190 | 0.50 | 1.1 | 1.1 |
| stage 2 50% | 17647 | 0.97 | 1.1 | 32.4 |
| stage 3 100% | 20172 | 0.65 | 1.1 | 23.0 |
| done | 27786 | 0.63 | 1.1 | 28.9 |

#### CSMS-initiated calls (issued from the control backend in the default pool to chargers in either pool)

| phase | ok | ok p50 ms | ok p99 ms (max of windows) | timeout (muted chargers) | not_connected |
|---|---:|---:|---:|---:|---:|
| baseline | 825 | 0.72 | 8.8 | 612 | 3023 |
| stage 0 1% | 749 | 1.13 | 67.1 | 538 | 2739 |
| stage 1 10% | 750 | 0.72 | 2.1 | 489 | 2752 |
| stage 2 50% | 657 | 1.96 | 54.7 | 467 | 2361 |
| stage 3 100% | 792 | 0.86 | 31.3 | 529 | 2676 |
| done | 1042 | 1.32 | 39.8 | 738 | 3716 |

Whole run: calls {'not_connected': 20374, 'ok': 5643, 'timeout': 3759}, delivered {'muted_dropped': 3921, 'unmuted': 5643} (a timeout is a call to a muted charger, which drops it by design; not_connected is the harness' own pre-check: the control backend only knows the chargers whose session events it received, a third to a half of the fleet in split mode, so it is not a CSMS failure).

#### Gateway CPU per phase (added cost of policy evaluation, end to end)

| phase | gateway cores (sum of both) | requests/s | gateway core-us per request | route decisions/s |
|---|---:|---:|---:|---:|
| baseline | 0.146 | 392 | 373.2 | 0 |
| stage 0 1% | 0.164 | 392 | 417.5 | 393 |
| stage 1 10% | 0.151 | 391 | 387.7 | 390 |
| stage 2 50% | 0.178 | 392 | 453.3 | 392 |
| stage 3 100% | 0.162 | 391 | 414.8 | 391 |
| done | 0.172 | 392 | 438.9 | 405 |

#### Hooks

**rollout run** at 180s
```
.384s, stable 16.384s, allowed x1.50"
time=2026-11-21T18:28:47.212+09:00 level=INFO msg="rollout decision" action=hold stage=2 percent=50 revision=3 reason="gates pass; soaking (50s of 1m0s)" dry_run=false gate.samples="pass: 5709 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s, stable 16.384s, allowed x1.50"
time=2026-11-21T18:28:57.204+09:00 level=INFO msg="rollout decision" action=apply stage=3 percent=100 revision=4 reason="applied stage all, waiting for the gateways" dry_run=false
time=2026-11-21T18:29:07.210+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="every gateway runs the stage; soaking" dry_run=false
time=2026-11-21T18:29:17.207+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="gates pass; soaking (10s of 1m0s)" dry_run=false gate.samples="pass: 3895 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
time=2026-11-21T18:29:27.211+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="gates pass; soaking (20s of 1m0s)" dry_run=false gate.samples="pass: 7844 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
time=2026-11-21T18:29:37.205+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="gates pass; soaking (30s of 1m0s)" dry_run=false gate.samples="pass: 11689 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
time=2026-11-21T18:29:47.210+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="gates pass; soaking (40s of 1m0s)" dry_run=false gate.samples="pass: 7819 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
time=2026-11-21T18:29:57.205+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="gates pass; soaking (50s of 1m0s)" dry_run=false gate.samples="pass: 11730 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
time=2026-11-21T18:30:07.201+09:00 level=INFO msg="rollout decision" action=hold stage=3 percent=100 revision=4 reason="gates pass; soaking (1m0s of 1m0s)" dry_run=false gate.samples="pass: 11784 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
time=2026-11-21T18:30:17.204+09:00 level=INFO msg="rollout decision" action=done stage=3 percent=100 revision=4 reason="every stage passed: ready to promote (roll the stable Deployment, then remove the rule)" dry_run=false gate.samples="pass: 11706 canary requests" gate.failed_ratio="pass: canary 0.0000, stable 0.0000, allowed +0.0050" gate.callerror_codes="pass: no code above its stable share" gate.p99="pass: canary 16.384s"
1795253417.206152693
```


## Run 3: automatic rollout, error-injecting canary

Phases: baseline 90-180s, stage 0 1% 180-200s, rolled back 200-480s

#### Routing correctness

4000 chargers with heartbeats in the handled logs.

| phase | window | chargers seen | on canary | share | nominal | within 3 sigma |
|---|---|---:|---:|---:|---:|---|
| baseline | 90-180s | 4000 | 0 | 0.0% | 0% | yes |
| stage 0 1% | 180-200s | 3864 | 33 | 0.9% | 1% | yes |
| rolled back | 200-480s | 4000 | 0 | 0.0% | 0% | yes |

Pool changes per charger over the whole run (every change is a heartbeat served by a different pool than the one before): 0: 3967, 2: 33.
Chargers with more than one pool change before the rollback (not sticky): 0; chargers that left the canary and came back before the rollback (flip): 0.

#### Errors and connections

| phase | window | charger request errors | chargers up (min) | server_connected (min) |
|---|---|---|---:|---:|
| baseline | 90-180s | 0 | 4000 | 4002 |
| stage 0 1% | 180-200s | Heartbeat/callerror: 18, MeterValues/callerror: 3, StatusNotification/callerror: 19 | 3997 | 3999 |
| rolled back | 200-480s | 0 | 4000 | 4002 |

Connect errors (whole run): none; freezes: None; final chargers up: 4000.

#### Upstream latency and outcomes per pool (gateway view)

| phase | pool | requests | failed | callerror InternalError | p50 ms | p99 ms |
|---|---|---:|---:|---:|---:|---:|
| baseline | default | 35302 | 0 | 0 | 0.26 | 0.83 |
| stage 0 1% | canary | 195 | 40 | 40 | 0.25 | 0.68 |
| stage 0 1% | default | 7747 | 0 | 0 | 0.25 | 0.74 |
| rolled back | canary | 0 | 0 | 0 | - | - |
| rolled back | default | 107748 | 0 | 0 | 0.26 | 0.76 |

#### Charger-side latency (harness, Heartbeat)

| phase | requests | p50 ms (n-weighted) | median window p99 ms | max window p99 ms |
|---|---:|---:|---:|---:|
| baseline | 22707 | 0.51 | 1.1 | 1.2 |
| stage 0 1% | 7585 | 0.50 | 1.0 | 1.1 |
| rolled back | 68108 | 0.50 | 1.1 | 1.2 |

#### CSMS-initiated calls (issued from the control backend in the default pool to chargers in either pool)

| phase | ok | ok p50 ms | ok p99 ms (max of windows) | timeout (muted chargers) | not_connected |
|---|---:|---:|---:|---:|---:|
| baseline | 856 | 0.80 | 3.5 | 570 | 3061 |
| stage 0 1% | 283 | 0.80 | 1.4 | 182 | 1024 |
| rolled back | 2645 | 0.82 | 7.5 | 1713 | 9141 |

Whole run: calls {'not_connected': 16304, 'ok': 4643, 'other': 2, 'timeout': 2853}, delivered {'muted_dropped': 3015, 'unmuted': 4643} (a timeout is a call to a muted charger, which drops it by design; not_connected is the harness' own pre-check: the control backend only knows the chargers whose session events it received, a third to a half of the fleet in split mode, so it is not a CSMS failure).

#### Gateway CPU per phase (added cost of policy evaluation, end to end)

| phase | gateway cores (sum of both) | requests/s | gateway core-us per request | route decisions/s |
|---|---:|---:|---:|---:|
| baseline | 0.159 | 392 | 405.2 | 0 |
| stage 0 1% | 0.147 | 400 | 367.0 | 408 |
| rolled back | 0.154 | 392 | 392.4 | 385 |

#### Hooks

**rollout run** at 180s
```
1795252544.658506496
time=2026-11-21T18:15:44.680+09:00 level=INFO msg="rollout decision" action=apply stage=0 percent=1 revision=1 reason="applied stage one, waiting for the gateways" dry_run=false
time=2026-11-21T18:15:54.684+09:00 level=INFO msg="rollout decision" action=hold stage=0 percent=1 revision=1 reason="every gateway runs the stage; soaking" dry_run=false
time=2026-11-21T18:16:04.680+09:00 level=WARN msg="rollout decision" action=rollback stage=0 percent=1 revision=2 reason="rolled back: gate failed: failed_ratio (canary 0.2091, stable 0.0000, allowed +0.0050)" dry_run=false gate.samples="pass: 110 canary requests" gate.failed_ratio="fail: canary 0.2091, stable 0.0000, allowed +0.0050" gate.callerror_codes="fail: callerror_InternalError: canary share 0.2091, stable 0.0000, allowed +0.0100" gate.p99="pass: canary 16.384s, stable 16.384s, allowed x1.50"
gocppctl: rolled back
1795252564.681814434
```

