# Operator runbook: staged releases on GKE

Two kinds of release, two procedures. This page is the checklist; the semantics
are in [staged-releases.md](staged-releases.md) (routing policy) and
[design/gateway.md](design/gateway.md) section 5. Commands are `gocppctl`
(`--redis-addr` or `GOCPPCTL_REDIS_ADDR` names the session registry; add
`--admin-token` or `GOCPPCTL_ADMIN_TOKEN` for the `gateway` commands).

Promotion is **external**: a person, a pipeline or the optional
`addons/rollout` controller runs the steps. A gateway never promotes itself; it
only suspends a rule when its local guard trips.

## Part 1. Backend canary (the processing tier)

Entry criteria: [staged-releases.md section 1](staged-releases.md) (N and N+1
compatible, idempotent transaction messages, expand/contract migrations).

1. **Deploy the canary pool with no traffic.** A second Deployment of the new
   image with `GOCPP_POOL=canary` (see `examples/gke-gateway/backend.yaml`). Check
   `gocpp_gateway_pool_members{pool="canary"}` on every gateway. A gateway without a
   policy sends work only to `default`.
2. **Confirm every gateway can route.** `gocppctl route apply` refuses a policy
   with rules while a gateway lease lacks the `route.v1` capability (an old
   release would ignore it); `--force` overrides.
3. **Step 1, the label-based release.** The application's connect handler copies
   its release labels into `ConnectReply.Attrs` as `label.canary=1` (or an
   external source supplies them, see Part 3). Apply:

   ```yaml
   salt: rollout-2026-11
   rules:
     - name: lab
       pool: canary
       match: { attrs: { label.canary: ["1"] } }
       hold: busy
       guard: { min_requests: 200, max_error_ratio: 0.05, window: 60s }
   ```

   `gocppctl route apply lab.yaml` returns once every gateway lease reports the
   revision. Exit code 3 means some gateway did not: do not proceed; the
   lagging gateways are named.
4. **Steps 2 to 5, percentage.** Keep the `lab` rule, add `step` with
   `percent: 1`, then 10, 50, 100, one `apply` each, same `salt`. Soak each step
   against the criteria of [staged-releases.md section 6](staged-releases.md) (failed
   outcome ratio, CALLERROR mix, p99 latency, your application checks).
5. **Promote.** Roll the stable Deployment to the new image (Nack and redispatch,
   no disconnects), then apply a policy without the rule and scale `backend-canary`
   to 0. Blue/green: apply `default: canary`, retire the old pool.
6. **Roll back at any step.** `gocppctl route rollback` writes the previous
   revision as a new one with holds ignored; every charger is back on `default`
   from its next message. Handlers already running on the canary finish.

What the gateways do on their own: a missing canary (no member attached, or all
Nack) sends the message to `default` immediately and suspends the rule for
`suspend_for`; the guard suspends a rule whose failure ratio is too high until
the next revision. Both show in `gocpp_gateway_route_suspended{rule}` and the
lease label `suspended` (`gocppctl route status`). A suspended rule needs a
decision from you: fix and re-apply, or roll back.

## Part 2. Gateway canary (the connection tier)

Constraints: sockets cannot move between pods, a passthrough load balancer has no
per-pod weight, and a restarted gateway is empty. The share is therefore set with
a soft session cap (readiness) and filled by a paced partial drain of the others.
Entry criteria: capacity so that `G-1` gateways hold the fleet, PDB
`maxUnavailable: 1`, `terminationGracePeriodSeconds` above `--drain-window`,
`minReadySeconds` so a crash-looping pod stops the rollout; each pod has
`--advertise-admin $(POD).gateway-link:9100` and an admin token
(`examples/gke-gateway/gateway.yaml`).

1. **Canary pod.** `kubectl patch statefulset gateway -p '{"spec":{"updateStrategy":{"rollingUpdate":{"partition":G-1}}}}'`
   with the new image. Pod `gateway-(G-1)` drains (the same drain as a full
   deploy) and restarts. The startup probe (`gocpp-gateway probe`) must pass
   before it becomes ready: it never receives a charger it cannot serve.
2. **Cap its share.** `gocppctl gateway cap --gateway gateway-3 --share 5`
   (5% of the fleet's sessions, from the leases; or `--max N`). At or above the
   cap the pod reports not-ready, so the load balancer stops sending it new
   connections; it keeps the sessions it has.
   `gocppctl gateway status --live` shows sessions, share and cap per gateway.
3. **Fill.** Shed from the old gateways, one at a time, paced:
   `gocppctl gateway shed --gateway gateway-0 --count 200 --rate 20`.
   The shedding gateway reports not-ready; the displaced chargers reconnect through
   the load balancer to the other ready pods, the canary among them until it is
   at its cap. Sessions younger than `--min-age` (default 10 min) are skipped so
   one charger is not moved repeatedly; `--attr key=value` sheds only matching
   sessions (labelled lab chargers move first). Churn to move X sessions to the
   canary is about `X x (G-1)` reconnects, all paced at `--rate` and admitted
   through the gateways' accept limiter.
4. **Observe** for the soak time, comparing canary and stable (every gateway metric
   carries `gateway_id` and `gateway_version`): `gocpp_gateway_accepts_total{result}`,
   `closes_total{code,cause}`, `session_duration_seconds`, `claims_total`,
   `upstream_requests_total{outcome}`, `link_attach_total`, the probe sidecar's
   `gocpp_probe_runs_total{result}`. Gates: handshake error ratio not above stable
   + 0.5 pp; abnormal closes not above 1.5x stable; no charger reconnecting more
   than 3 times in 10 minutes; zero registry corrections after the fill settles.
5. **Complete.** Lower `partition` one ordinal at a time (each step is an ordinary
   drain). Then `gocppctl gateway cap --gateway gateway-3 --off`.
6. **Rebalance.** The last restarted pod is empty. `gocppctl gateway rebalance`
   computes the fair share from the leases and sheds the excess from gateways
   above `fair x 1.1` (`--tolerance`), in rounds with a settle time
   (`--rounds`, `--settle`); `--dry-run` prints the plan. Run it after any gateway
   rollout or crash, canary or not.
7. **Roll back.** Raise `partition` again (or roll the StatefulSet back): the canary
   drains like any gateway and, because its share was capped, the reconnect volume
   is bounded by the cap. A canary that harms sessions (corrupt frames, stuck
   writes): `kubectl delete pod` it; the lease lapses within the TTL and the accept
   limiter paces the return. A canary that never passed the startup probe holds no
   charger and rolling back is free.

Variant: a second StatefulSet `gateway-canary` behind its own Service and load
balancer address, with lab chargers pointed at its hostname or a weighted DNS
record set (stable address, canary address). It costs a second address and depends
on how chargers cache DNS, so it is the variant for the lab-charger case, not the
default. Everything above (probe, cap, shed, rebalance) applies to it unchanged.

Verify on your cluster (not measured): that a pod turning not-ready keeps its
established connections (connection persistence on unhealthy backends of your L4
load balancer), and how new connections spread when every pod is not-ready
(expected: fail open across all pods). The `gocppctl gateway` procedures assume
both.

## Part 3. Labels from outside the backend

If the labels a release selects on live in a system the backend does not own, run
the gateways with `--attr-source-url https://labels.internal/v1/chargers/{cpid}`
(a JSON object of strings; 404 = none; `--attr-source-token` for a bearer token) or
`--attr-source-file labels.yaml`. The attributes appear on every session as
`ext.<key>` (`--attr-source-timeout`, default 200 ms at connect; a failure means no
external attributes and is counted in `gocpp_gateway_attr_source_total{result}`;
`--attr-source-refresh` re-reads for connected sessions). They never override what
the backend set. Rules then use `ext.<key>`:

```yaml
rules:
  - name: lab
    pool: canary
    match: { attrs: { ext.label.canary: ["1"] } }
```

Library: `csms.WithAttrSource(src, csms.AttrSourceOptions{...})` with any
`gateway.AttrSource`; `gateway/attrsource` has the file and HTTP sources.

## Checklists

Before: entry criteria met; canary pool attached everywhere; every gateway
`policy_rev` equal to the stored revision; probe sidecars green; alerts on
`route_suspended`, `route_policy_rejected_total`, `probe_runs_total{result="error"}`.

After: the rule removed or promoted; `partition: 0`; no soft cap anywhere
(`gocppctl gateway status --live`: `soft_max=0`); sessions balanced
(`gocppctl gateway rebalance --dry-run` prints nothing to do).
