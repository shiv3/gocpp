# Gateway mode: connection tier and processing tier

A gocpp CSMS can run as two cooperating parts instead of one process:

- the **gateway** (connection tier) owns the TLS/WebSocket sessions. It runs the
  whole of a normal `csms.Server` (framing, CALL/CALLRESULT correlation, call
  slot, timeouts, keepalive, frame observer) but no handlers: it forwards every
  inbound CALL, payload untouched, to the processing tier and writes the answer
  back. It is rarely redeployed because it never reads a payload.
- the **backend** (processing tier) holds no per-charger state. It receives the
  forwarded messages, runs the payload pipeline (signed-message unwrap, schema
  validation, lenient coercion, time encoding) and your handlers, and reaches a
  charger through the gateway that holds it. Deploying a backend disconnects
  nobody.

The typed API does not change: `csms.On`, `csms.Call`, `csms.Send`, the generated
`handlers` registrations and every schema option mean the same in both modes. The
conformance and e2e suites run in both (see "Testing").

The full design, the failure semantics and the rationale are in
[design/gateway.md](design/gateway.md); this page is the user guide.

## Wiring

```go
hub := inproc.New() // in-process link; the gRPC link is a separate module (below)

// connection tier
gw := csms.NewGatewayServer(hub.Gateway(),
    csms.WithGatewayID("gw-1"),
    csms.WithSubProtocols("ocpp1.6"), // every connection option keeps its meaning
)
http.ListenAndServe(":8080", gw.Handler())

// processing tier
be := csms.NewServer(csms.WithGatewayLink(hub.Backend()),
    csms.WithLenientSchema(), csms.WithAuthenticator(myAuth))
v16handlers.RegisterCSMS(be, myHandler)   // unchanged generated registration
go be.ServeGateway(ctx)                   // blocks like http.Serve; drains when ctx ends

// anywhere in the backend
conn, err := be.Lookup(ctx, "CP_1")
resp, err := csms.Call(ctx, conn, profiles.Reset, req)
```

`examples/gateway-inproc` is a runnable version. One option list can configure
both tiers: on the gateway the processing-tier options (schema, handlers,
OnConnect/OnDisconnect, time encoding) are ignored.

`csms.WithSplitInProcess()` builds both tiers inside one `*csms.Server` (Handler
returns the gateway's handler, `Get` returns a handle). It exists so the whole
test suite can run through the contract; it is also a way to adopt the topology
in a single binary.

## What runs where

| Concern | Gateway | Backend |
|---|---|---|
| TLS, client certificate, Basic header, origin check, subprotocol | yes | |
| Credential decision, per-connection timeouts | asks the backend (`Upstream.Connect`) | decides (`WithAuthenticator`, `WithConnConfig`) |
| OCPP-J framing, message ids, max size, malformed-frame CALLERRORs | yes | |
| CALL correlation, one outstanding CSMS call, call timeout | yes | supplies a deadline |
| Keepalive, read idle, write watchdog, frame observer | yes | |
| Signed-message verify, schema validation, lenient coercion, outbound validate / time encoding | **no** | yes |
| Handlers, error mapping (`ErrorCodeMapper`), BootNotification gating | | yes |

Because the backend decides every payload-level error, the CALLERROR on the wire
is byte-identical to embedded mode (`TestGateway_WireIdenticalToEmbedded`).

## Behaviour to know

- **Session handles.** A backend has no sockets: `Get` and `Connections` are empty
  (use `Lookup`). A `*csms.Conn` on a backend is a handle for a session:
  `ID()`, `Version()`, `Subprotocol()`, `RemoteAddr()`, `RequestHeader()`, `TLS()`
  (peer certificates only) work from what the gateway reported; `Close` and
  `Send` go through the link; `Stats()` is zero and `Done()` nil.
  `conn.GatewaySession()` returns the session reference (gateway id, boot id,
  epoch).
- **Session events.** `WithOnConnect` / `WithOnDisconnect` are driven by session
  events the gateway sends to one backend. Delivery is at least once and the
  backend ignores a close that is older than the newest session it knows, so the
  old session's disconnect is always reported before the new session's connect.
  Make the callbacks idempotent per `(cpID, epoch)`.
- **No backend.** An inbound CALL is held up to the link's wait (default 20 s)
  for a backend to attach, then the charger gets an `InternalError` CALLERROR,
  well inside its own timeout. An upgrade needs a connect decision, so with no
  backend attached new connections get `503` + `Retry-After`; existing sessions
  stay up.
- **At most once after dispatch.** If a backend takes a CALL and then fails, the
  charger gets `InternalError` and the gateway never retries on another backend.
  A backend that answers `Nack` (draining) or a write that provably did not
  happen is redispatched within the same deadline. `ServeGateway` drains: when
  its context ends the backend takes no new work and returns once the messages
  in flight are answered.
- **CSMS calls and `Sent`.** `csms.Call` on a backend validates the request,
  sends it through the link, and validates the answer. A failed call carries what
  is known about delivery: `gateway.SentOf(err)` is `NotSent` (safe to retry),
  `SentNoResponse` (the frame was written; the charger may have executed it) or
  `Answered`. A stale or vanished session is looked up once more automatically
  when nothing was sent.
- **Fencing.** Every request carries the session epoch; a gateway rejects a call
  whose epoch is not the session it holds (`gateway.ErrSessionStale`).
- **Orphan results.** If the caller is gone by the time the charger answers a
  command that was already written, the answer is delivered to a backend as an
  orphan result (`WithOnOrphanResult`) instead of being dropped.
- **Idempotency key.** `CallRequest.CallID` makes duplicate requests that arrive
  while the first is in flight share its outcome (the charger sees one CALL).

## Edge Heartbeat and per-tenant limits

Both are opt-in options of the gateway (`csms.NewGatewayServer`) and default off;
`cmd/gocpp-gateway` has flags for them.

**Edge responder.** `csms.WithEdgeHeartbeat()` makes the gateway answer
`Heartbeat` with its own clock (`{"currentTime": ...}`, RFC 3339 UTC, formatted
per `WithTimeEncoding`), so the most frequent idle message of a fleet never takes
the backend hop. `csms.WithEdgeResponder(action, fn)` does the same for any
action; `fn` returns the CALLRESULT payload, or `false` to forward the CALL as
usual, so a responder can decide per session from the attributes the backend set
(`csms.WithEdgeHeartbeat(func(r csms.EdgeRequest) bool { return r.Attrs["boot"] == "accepted" })`
answers only once the backend accepted the charger's BootNotification). The
trade-offs are real, which is why it is off by default: the gateway becomes
action-aware for that action, the request is not validated at the edge, and the
backend no longer sees those messages (use `csms.WithOnEdgeResponse` and the
`gocpp_edge_responses_total{version,action}` metric for liveness bookkeeping).
`gocpp-gateway --edge-heartbeat`.

**Per-tenant limits.** `csms.WithTenantLimits(csms.TenantLimits{...})` limits, per
tenant, simultaneous sessions and inbound messages per second:

- The tenant is resolved once, at connect: `Resolver(r, cpID)` (a header, a path
  segment, an id mapping; `csms.TenantFromRequest` adapts the resolvers of
  `addons/tenant`), else the session attribute `AttrKey` (default `tenant`) that
  the backend returned from its connect decision. A charger without a tenant
  belongs to tenant `""` and gets `Default`.
- **Sessions** (`MaxSessions`) are checked at admission, before the upgrade: an
  upgrade over the cap is a plain 503 with a jittered `Retry-After`
  (`RetryAfter`), so nothing is established and torn down, and a charger
  replacing its own live socket is not counted twice.
- **Messages** (`MessageRate`, `MessageBurst`) are one token bucket per tenant,
  summed over its sessions. A message over budget is answered with a CALLERROR
  (`RejectCode`, default `GenericError`; OCPP has no throttling code) and never
  reaches a handler or the backend; an over-budget SEND is dropped; the session
  stays up. The bucket is shared fairly: while it is more than half empty, a
  session can only use its equal share of the rate, so one noisy charger cannot
  starve the other chargers of its tenant, and a tenant never consumes
  another's budget.
- **In-flight CALLs** (`TenantLimits.InFlightPool`, `TenantLimit.Weight`,
  `TenantLimit.MaxInFlight`) bound how many inbound CALLs and SENDs run at once,
  from admission until the handler (on a gateway: the backend hop) has answered.
  The pool is split by weight among the tenants that currently have sessions. A
  tenant is always admitted up to its weighted share (at least one slot), so a
  noisy tenant can never crowd out a quiet one; above its share it may borrow
  capacity, but only what remains after reserving the unused shares of the other
  active tenants, so idle capacity is used and a tenant that wakes up finds its
  share free. `MaxInFlight` is a hard per-tenant ceiling on top. A refused
  message gets the same answer as an over-rate one (`RejectCode` CALLERROR, SEND
  dropped, session stays up) and is counted as `kind="inflight"`. Slots of frames
  that never finished because the session ended are returned when it closes.
  This protects the processing tier (and the gateway's goroutines) from a tenant
  whose backend calls are slow; the message rate bounds arrivals, the in-flight
  cap bounds concurrency.
- Metrics: `gocpp_tenant_sessions{tenant}`, `gocpp_tenant_inflight{tenant}`,
  `gocpp_tenant_rejected_total{tenant,kind}`
  (`kind` = `sessions` | `messages` | `inflight`) and `gocpp_accepts_total{result="tenant_limit"}`;
  the tenant label is bounded by `MaxMetricTenants` (default 100, the rest
  reported as `other`).
- On a gateway the check runs at the edge, before the backend hop; an embedded
  server can use it too. Flags: `--tenant-header`, `--tenant-attr`,
  `--tenant-max-sessions`, `--tenant-message-rate`, `--tenant-message-burst`,
  `--tenant-inflight-pool`, `--tenant-max-inflight`, `--tenant-weights a=3,b=1`.

Overhead, measured with `benchmarks` (`BenchmarkEdgeAndTenant`, one Heartbeat over
a real WebSocket, pinned to 4 CPUs on a shared host): the tenant check adds no
allocation (17 allocs/op embedded, 31 split, with and without it) and its time is
below the run-to-run noise; the edge Heartbeat takes a split round trip from 31
allocs / 2.4 KB to 19 allocs / 1.8 KB and about 20% off its latency (the
in-process hop; over a network link the saving is the network round trip).
`TestGateEdgeAndTenantOverhead` keeps both honest in CI.

## The gRPC link and the reference binary

`addons/link-grpc` (its own module, so the main module gains no gRPC dependency)
is the link for separate processes. **Backends dial gateways**: a gateway serves
the `Attach` stream (`Gateway.Register(grpcServer)`), a backend keeps one stream
per gateway (`NewBackend(StaticTargets(...) | DNSTargets("gateway-link", "9090"))`,
then `csms.WithGatewayLink(backend)` and `ServeGateway`). Every message kind rides
the stream with correlation ids; HTTP/2 flow control plus a per-stream in-flight
cap is the backpressure and the keepalive detects a dead peer in seconds. The
first frame is `Hello` (protocol range, capabilities, pool, version) and `HelloAck`
carries the negotiated protocol and the capability intersection; a disjoint range
is rejected loudly (`link_attach_total{result="proto_mismatch"}`). A frame body or
control op a peer does not know is answered `Unsupported` and the stream stays up.
A backend that is stopped sends `Drain`, the gateway stops choosing it, answers
every request it had in flight and acknowledges (`DrainAck`), and only then does
the backend close: a backend deploy is invisible to chargers. The wire contract is
`addons/link-grpc/proto/gocpp/link/v1/link.proto` (`buf lint`, `buf generate`).

`Backend.Locate` (used by `Server.Lookup` on a backend without
`csms.WithSessionRegistry`) asks every attached gateway that offers the `locate`
capability which session it holds for a charge point; an older gateway counts as
"not here". With a session registry the registry stays the authority.

Writes on a stream are coalesced: while one write is in progress, frames queued by other
goroutines wait and are then written together, as a single `Batch` message when both ends
negotiated the `batch` capability (otherwise as consecutive messages). There is no timer,
so a frame on an idle stream is written at once and the added latency at low rate is
zero. `WithStreams(n)` keeps n streams (each on its own connection) to every gateway for
pairs that carry more than one stream can; the default is 1.

`cmd/gocpp-gateway` (own module) is the reference gateway: WebSocket listener
(TLS, optional client-certificate verification, shared TLS ticket keys, accept
rate limit), the link listener (optional mTLS between tiers), the Redis session
registry, `/metrics` (labels `gateway_id`, `gateway_version`, `pool`,
`backend_version`), `/healthz`, `/readyz` (not draining, under
`--soft-max-sessions`, and a worker attached to the default pool), SIGTERM drain
(`--drain-window`), and `gocpp-gateway probe` (upgrade + BootNotification +
Heartbeat with the reserved identity `$probe-<id>`; the backend must answer it
without side effects) for use as a startup probe. Flags have `GOCPP_GATEWAY_*`
environment equivalents. Example manifests: `examples/gke-gateway`.

Failure behaviour is exercised with real processes in
`cmd/gocpp-gateway/chaos` (kill -9 a backend mid-handler, SIGTERM a backend under
load, kill -9 a gateway, gateway/backend and gateway/registry partitions with a
TCP fault proxy, the same charger id on two gateways).

## The NATS link

`addons/link-nats` (its own module) carries the same contract (`gocpp.link.v1`
messages, Hello/HelloAck with protocol range and capabilities, Drain/DrainAck,
Nack, `Sent` classification, unknown requests answered `Unsupported`) over NATS
core request-reply and queue groups. Use it where NATS already runs, where
non-Go consumers must handle some actions, or where per-action worker pools by
subject are wanted; gRPC stays the default (the design explains why: the broker
sits on the path of every message and NATS drops rather than backpressures).

```go
// gateway
nc, _ := linknats.Connect("nats://nats:4222", "gw-3")
link, _ := linknats.NewGateway(nc, linknats.WithGatewayVersion(v))
gw := csms.NewGatewayServer(link, csms.WithGatewayID("gw-3"), ...)

// backend
nc, _ := linknats.Connect("nats://nats:4222", "backend")
be, _ := linknats.NewBackend(nc)
srv := csms.NewServer(csms.WithGatewayLink(be), csms.WithBackendInfo(gateway.BackendInfo{Pool: "default"}), ...)
srv.ServeGateway(ctx)
```

`cmd/gocpp-gateway --link nats --nats-url nats://...` runs the reference gateway
on it (no link listener). Subjects (prefix `gocpp`, configurable, one token per
dynamic part): inbound CALLs on `gocpp.in.<pool>.<version>.<action>` in the queue
group `gocpp.<pool>` (`WithActionSubjects` makes a backend subscribe only to
those actions, which is how a per-action worker pool is built), `connect.<pool>`
and `notify.<pool>` in the same group, CSMS-initiated work on
`gocpp.gw.<gatewayID>.call|send|control|locate`, `hello`/`drain`/`who`/`cancel` for
membership.

What is different from the gRPC link, all inherent to NATS core:

- **Membership by lease.** A backend sends `Hello` every interval
  (`WithHelloInterval`, default 5 s); a gateway answers `HelloAck` and drops a
  backend after three missed intervals, a backend drops a gateway likewise. That
  is the failure detection time of the link (there is no stream to break), so
  size the interval like the gRPC keepalive.
- **Who has the request.** The broker, not the gateway, picks the backend of a
  queue group. The backend therefore sends an interim "accepted" frame the moment
  it receives a request; the gateway knows who works on it and fails it
  (InternalError, never a redispatch) when that backend's lease lapses. A request
  no backend acknowledged within `WithAcceptTimeout` (default 5 s) is failed the
  same way. "No responders" (nobody subscribed) is the fast path to the hold and
  `ErrNoBackend`.
- **At most once, no flow control.** A backend over `WithMaxInFlight` answers
  `Nack` and the gateway asks again (the broker picks another member); the
  gateway's own in-flight caps remain the only other limit.
- **Drain.** A stopping backend unsubscribes from its queue group first (what it
  had buffered is Nacked), tells the gateways, and each answers `DrainAck` when
  the requests it had dispatched to that backend are answered.
- **Connection options.** `linknats.Connect` sets no reconnect buffering (a
  request must never be delivered after its deadline), endless reconnects and a
  2 s ping. A dropped connection fails every request in flight on it
  (`stream_broken`): after a broker restart both tiers reconnect by themselves.

Failure behaviour is exercised by the chaos suite with the NATS link
(`GOCPP_CHAOS_LINK=nats go test ./chaos`, an embedded `nats-server` per test):
the same scenarios as for gRPC plus a broker restart.

## Links

| Link | Package | Use |
|---|---|---|
| in-process | `gateway/inproc` | tests, conformance, single-binary deployments |
| gRPC (backends dial gateways) | `addons/link-grpc` (own module) | the default for separate processes |
| NATS (queue groups) | `addons/link-nats` (own module) | deployments that already run NATS, non-Go consumers, per-action worker pools |

A link implements `gateway.GatewayLink` (the gateway end: `Upstream` plus
`Attach`) and `gateway.BackendLink` (the backend end: `Downstream` plus `Serve`
and `Locate`). `gateway.Pool` is the shared backend selection (power of two
choices on in-flight count, the hold, redispatch on `Nack`/`ErrNotDelivered`,
drain) that links reuse.

## Staged releases: pools and routing (seams)

Backends announce themselves with `gateway.BackendInfo` (pool, version, role,
protocol range, capabilities, labels; `csms.WithBackendInfo`). A gateway sends
inbound work to the pool its `gateway.Router` names (`csms.WithRouter`; the
router sees the charger id, action, OCPP version and the session attributes and
must be pure, so a charger is sticky by construction). **Without a router only
`gateway.DefaultPool` gets work**: starting a canary backend is never a traffic
change by itself. A pool with no available member falls back to
`Route.Fallback` at once; if every member Nacks the message is redispatched on
the fallback. `PoolSet` (used by every link) implements this; `Envelope.Route`
tells the backend which pool and rule the message went through.

`BackendInfo.Actions` is filled in by `csms` from the handlers registered when
`ServeGateway` starts (an empty list means "all", as for a backend with no
handlers). A message routed to a pool in which no available member lists its
action goes to `Route.Fallback` if that pool serves it, otherwise it stays on
the routed pool (an `on_unavailable: error` rule: the backend answers
`NotImplemented`); either way it is counted (`Gateway.Unhandled`,
`PoolSet.SetUnhandledObserver`). Inside a pool, members that do not list the
action are not picked while others do, so a partially rolled-out action reaches
only the builds that have it.

Session attributes are the routing labels: `ConnectReply.Attrs` at connect, and
`Conn.SetAttrs` (or `Server.SetSessionAttrs`) from the
backend afterwards (merge; an empty value deletes; 1 KiB cap); the
router sees them from the next message. A rollback is swapping the router; no
socket is touched.

The built-in router is `gateway/routepolicy`: a declarative first-match policy
(charger ids, attributes, OCPP versions, a stable-hash percentage, per-rule
actions, `on_unavailable`, `hold: busy`, `suspend_for`, an error-rate `guard`),
stored in the session registry with compare-and-set and pushed to every gateway
in well under a second, operated with `gocppctl route`. The runbook is
[staged-releases.md](staged-releases.md).

The connection tier has its own staged release (a canary gateway: the highest
StatefulSet ordinal with a soft session cap, filled by a paced partial drain of
the others, then rebalanced): each gateway serves `/admin/status`, `/admin/shed`
and `/admin/soft-max` on its admin listener (`gateway/admin`, `--admin-token`,
`--advertise-admin`), driven by `gocppctl gateway status|shed|cap|rebalance`.
`probe --interval` is a sidecar that exercises an idle gateway continuously, and
`--attr-source-file` / `--attr-source-url` (`csms.WithAttrSource`) add labels from
outside the backend as `ext.*` session attributes. The runbook is
[rollout.md](rollout.md).

## Performance

Measured with the load-test harness (`interop/loadtest` on branch `test/loadtest-4k`,
`results/gateway-2026-11-21`): 4,000 chargers, loopback, gateway on 2 pinned cores,
2 backends on 2 cores each, gRPC link, schema off, compared with one embedded server
on 6 cores. Targets are those of design section 2.7.

| Target | Result |
|---|---|
| Gateway >= 20,000 inbound CALLs/s per core | 22,700 msg/s per core at 36k msg/s (1.6 cores); the same gateway at 7.7k msg/s used 0.77 cores |
| Added latency, p50 <= 0.5 ms, p99 <= 2 ms at 5k msg/s | +0.15 ms p50, +0.5 ms p99 at 7.7k msg/s (Heartbeat p50 0.20 -> 0.35 ms, p99 0.49 -> 0.96 ms) |
| Gateway memory <= 16 KB per idle session | **not met**: 25 KB/session at 10,000 idle sessions (embedded server: 26 KB). Same per-session cost as embedded, so it is not the split; it is above docs/performance.md's 13.6 KB (a different measurement: heap of a benchmark vs process RSS) and is not investigated yet |
| Backend rollout: 0 disconnects, 0 attributable CALLERRORs | met: 5 SIGTERM and 3 kill -9 restarts under load, 0 disconnects, restart-window Heartbeat p99 0.96 ms |
| Gateway rollout, reconnects within the pace | drain of one of two gateways (2,000 chargers, 20 s window) moved all sessions through the other gateway; ~9% of chargers were disconnected at the deepest point; no freeze. The 10,000-charger / 5-minute / 3-gateway scale of the design was not run |
| Gateway crash, accept capacity (500 handshakes/s per core) | not measured here (no TLS in the harness) |

Cost of the split: total CPU is about 1.4x the embedded server (3.3 vs 2.4 cores at 37k
msg/s); the gRPC hop is about 25 us of CPU per message over both ends. The profile
(`results/.../profiles`) shows the gateway at 27% in `write(2)`: the WebSocket reply write
and the link stream write, in roughly equal parts. Link write coalescing, a second stream
per backend and stack reservation in the backend's request goroutines followed (see
below): in the same harness at 35k msg/s total CPU went from 1.35x to 1.29x of embedded
(79.8 to 76.2 us per message against 59.1); a second stream did not change it. When the gateway is the bottleneck (it
ran at 80% of its 2 cores in the two top steps) p99 grows to tens of milliseconds, which
is queueing, not hop cost: size gateways below about 70% CPU.

Gates in this repository (they fail the build on a regression, they do not claim the
numbers above):

- `benchmarks.TestGateHopOverhead` (CI, without `-race`): through the in-process link a
  message may cost at most 0.5 ms more on average and 2 ms more at p99 than embedded
  (measured: +13 us mean, +31 us p99). `BenchmarkGatewayHop` shows the allocation side:
  17 allocs / 1.8 KB per round trip embedded, 31 allocs / 2.4 KB split.
- `addons/link-grpc: TestGateGRPCHopLatency`: the gRPC hop at 5,000 msg/s open loop stays
  under 5 ms p99 over loopback (measured: p50 0.10 ms, p99 0.3 ms). `BenchmarkGRPCHop`:
  72 us sequential round trip, 4.6 us per message with 256 in flight (61 / 50 allocs).

Verify on a real cluster (not measured): that an L4 load balancer keeps established
connections to a pod that turns not-ready, and how new connections are spread when every
pod is not-ready; TLS handshake capacity; the 10,000-charger gateway rollout of the design.

## Connect-decision cache

`csms.WithGatewayAuthCache(csms.AuthCacheConfig{TTL, StaleIfError, MaxEntries})`
on a gateway remembers accepted connect decisions (key: hash of charge point id,
path, `Authorization`, offered subprotocols and client certificate). Within `TTL`
(default 5 minutes) a reconnect skips the backend; after it the backend decides
again, and if it cannot be reached an accepted decision up to `StaleIfError`
(default 1 hour) old is reused. Rejections are never cached.
`Server.InvalidateAuthCache(cpID)` drops entries after a credential change. Leave
it off when the backend decides on other request headers.

## Testing

`GOCPP_TEST_SPLIT=1 go test ./internal/...` runs the conformance and e2e suites
with every server split through the in-process link (CI does this on every PR).
Tests create servers with `internal/splitmode.NewServer`, which is
`csms.NewServer` plus `WithSplitInProcess` when the variable is set. The NATS link
has its own tests (`addons/link-nats`: the link contract over a real
`nats-server`, and a wire-identity test that diffs the frames of an embedded
server against a gateway with a NATS backend) and runs the whole chaos suite.

## Not yet

Without a registry a gateway numbers its sessions with a local counter, so
duplicate connections across two gateways are not detected yet.
