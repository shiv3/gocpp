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

## Stability

Gateway mode is new. Its public API is **experimental**: it may change in minor
releases before v1.0, and every such symbol says so in its doc comment
("Experimental: this API may change before v1.0."). Pin the module version and
read the CHANGELOG when upgrading. Nothing here changes how an embedded
`csms.Server` behaves.

| Surface | Status |
|---|---|
| `csms.NewServer`, `On`, `Call`, `Send`, `Get`, the generated handlers and clients, schema and connection options, `core/ocppj`, `core/schema`, `core/auth` | stable (unchanged by gateway mode) |
| `csms.NewGatewayServer`, `WithGatewayLink`, `WithSplitInProcess`, `WithSplitLink`, `SplitLink`, `WithRouter`, `WithAttrSource`, `WithTenantLimits`, `TenantLimits`, `DrainOptions`, `Server.Drain`/`DrainStatus`/`ReadyHandler`/`Draining`, `Server.SetGatewayLabel`/`SetSessionAttrs`, `Conn.SetAttrs` | experimental |
| package `gateway` (the link contract, `Pool`, `PoolSet`, `Router`) and `gateway/inproc`, `gateway/routepolicy`, `gateway/admin`, `gateway/attrsource` | experimental |
| `core/storage`: `SessionRegistry`, `PolicyStore`, `PolicyHistory`, `RouteWriter`, `StateStore` and their `memory` implementations | experimental (`ConnectionRegistry`, `MessageRouter`, `TransactionStore`, `ConfigStore` keep their status) |
| `core/dispatcher`: `Pipeline`, `InboundGate`, `Conn.SetAttachment`/`Owner`/`SetInboundGate`/`SetInboundDone`/`Quiesce`/`TryDrain`/`ForceDrain`, `RawCallError`, `WithSentHook`, `ConnMetadata.Owner` | experimental |
| addons `link-grpc`, `link-nats`, `registry-redis`, `rollout` | experimental |
| the gRPC link protocol (`gocpp.link.v1`), the registry key layout, the policy document format | experimental: mixed-version fleets are only supported between adjacent releases while this holds |
| commands `gocpp-gateway`, `gocppctl` (flags, admin API) | experimental |

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
  backend ignores a close that is older than the newest session it knows (and,
  with a session registry, one the registry shows as superseded), so the old
  session's disconnect normally precedes the new session's connect. Without a
  registry a backend that never saw the newer session cannot tell a late close
  from a legitimate one, so an old session's disconnect can arrive after the new
  session's connect: ignore a disconnect whose epoch
  (`conn.GatewaySession().Epoch`) is older than your current session's.
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
  orphan result (`WithOnOrphanResult`) instead of being dropped. It goes to the
  pool of the backend that issued the call (any member), or to the default pool
  when that pool has no backend left (the caller died);
  `gocpp_gateway_orphan_results_total{pool,outcome}` counts them (`delivered`,
  `fallback`, `dropped`).
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

## Frame and session taps

For audit, analytics and replay a gateway (or an embedded server) can export what
crosses its sockets to an external sink without being slowed by it
(`core/observability/tap`):

```go
sink, _ := tap.JSONLFile("/var/log/gocpp/tap.jsonl") // or linknats.NewTapSink(nc), or tap.Multi(a, b)
ex := tap.New(sink, tap.WithSource("gw-1"), tap.WithBuffer(8192), tap.WithFrameSampling(10),
	tap.WithFrameFilter(func(ev observability.FrameEvent) bool { return ev.Action != "Heartbeat" }))
srv := csms.NewGatewayServer(link,
	csms.WithFrameObserver(ex.FrameObserver()),     // every raw frame, in and out, in wire order
	csms.WithSessionObserver(ex.SessionObserver())) // opened / closed (cause, duration, epoch, attrs)
defer ex.Close(ctx)                                 // flushes the buffer
```

- **Hot path.** A frame observer runs under the connection's write lock or on its
  reader goroutine, so the exporter only filters, samples (1 in N, a counter),
  copies a bounded prefix of the frame (`WithMaxFrame`, default 2 KiB; negative:
  metadata only) and does a non-blocking send into a bounded buffer
  (`WithBuffer`, default 4096). A full buffer drops the event and counts it;
  nothing ever waits for the sink (about 200 ns and one allocation per frame,
  `BenchmarkFrameObserver`).
- **Sink.** A background goroutine hands batches (`WithBatch`, default 256 events
  or 1 s) to a `tap.Sink`, which may block or fail: failed batches are counted,
  not retried. Provided: `tap.JSONL` / `tap.JSONLFile` (one JSON object per line),
  `linknats.NewTapSink` (core NATS subjects `<prefix>.tap.frame.<cp>` and
  `<prefix>.tap.session.<cp>`; bind a JetStream stream to `<prefix>.tap.>` to keep
  them), `tap.Multi`. Kafka, Pub/Sub or an object store are a `Sink` of a few
  lines each.
- **Delivery.** At most once per exporter, with `(source, seq)` on every event;
  `seq` counts dropped events too, so a consumer sees losses as gaps and
  deduplicates when its transport redelivers. Session events carry the registry
  epoch, which orders the connections of one charge point.
- **Session events** (`csms.WithSessionObserver`) fire where the socket lives:
  once on open and exactly once on close (including replacement by a newer
  connection), on a gateway as on an embedded server; the backend's
  `WithOnConnect` / `WithOnDisconnect` are unchanged.
- Metrics and flags of the reference binary: `gocpp_tap_events_total{result}`
  (`exported`, `dropped`, `sampled`, `failed`), `gocpp_tap_queue_depth`;
  `--tap-jsonl`, `--tap-nats` (uses `--nats-url`/`--nats-prefix`), `--tap-sample`,
  `--tap-buffer`, `--tap-max-frame`, `--tap-skip-heartbeat`, `--tap-no-frames`,
  `--tap-no-sessions`. Frames contain whatever the chargers send (idTags,
  possibly personal data): protect the sink like the traffic itself.

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
negotiated the `batch` capability (otherwise as consecutive messages). By default there is
no timer, so a frame on an idle stream is written at once and the added latency at low rate
is zero. `WithStreams(n)` keeps n streams (each on its own connection) to every gateway for
pairs that carry more than one stream can; the default is 1.

### Write linger

Optional and off by default: `linkgrpc.WithGatewayWriteLinger(d)` for the frames a gateway
writes to a backend (`gocpp-gateway --link-write-linger d`,
`GOCPP_GATEWAY_LINK_WRITE_LINGER`) and `linkgrpc.WithBackendWriteLinger(d)` for the frames
a backend writes to a gateway (the replies; an option of your backend program). On a busy
stream a frame that would be written alone waits up to `d` for the frames about to follow
and they leave in one write: fewer system calls and thread wake-ups on both ends, paid for
with latency. Each end sets its own direction and needs nothing from the peer.

- **Adaptive, per stream.** The wait applies only while frames follow each other on that
  stream at an average interval of at most `d/2` (a moving average over the last few dozen
  frames), so that two or more further frames are expected within it: from about 10,000
  frames/s per stream for 200 us, 4,000 for 500 us. Below that rate every frame is written
  at once, exactly as without the option, and no timer is armed; a single pause of a few
  `d` switches the wait off for the frames that follow it, and a handful of frames in
  quick succession does not switch it on. The rate that counts is that of one stream: a
  gateway that spreads 30k msg/s over ten backends never waits at 200 us.
- **Bounded.** A frame waits at most `d` (`d/2` on average), plus the lateness of the
  timer: the Go runtime fires a sub-millisecond timer up to about 1 ms late when the
  process has nothing else to do, as after the last frames of a burst. A frame that
  arrives after `d` has passed ends the wait itself, and so does a full batch (128 frames
  or 1 MiB encoded). `d` is at most 5 ms (`linkgrpc.MaxWriteLinger`; the options clamp,
  the flag refuses more).
- **Errors.** A frame accepted into a wait is in the position of a frame queued behind a
  write in progress: its sender is told nothing until the answer arrives, and it is never
  reported as "not written" after the fact (which would let a delivered request be sent
  twice). If the write at the end of the wait fails the stream is broken and every request
  waiting on it fails as "stream broken"; only the goroutine that writes a batch its own
  frame is part of sees that write's error. The cost: a request that sat in a wait when
  the stream broke is answered with an error instead of being redispatched to another
  backend, as a request queued behind a write always was.
- **No fixed delay.** A linger that always waits was measured and dropped: at 260 msg/s a
  fixed 100 us raised the Heartbeat p50 from 0.8 to 2.1 ms (the timer lateness above, once
  per direction) and total CPU by a quarter (a timer per frame).
- The NATS link has no such option: the NATS client buffers publishes and flushes them
  itself.

What it buys and what it costs is in "Performance" below. In short: leave it off unless a
gateway-backend pair carries more than about 10,000 msg/s and CPU matters more than a
millisecond of latency; then start with 200 us on both ends.

`cmd/gocpp-gateway` (own module) is the reference gateway: WebSocket listener
(TLS, optional client-certificate verification, shared TLS ticket keys, accept
rate limit), the link listener (mutual TLS between tiers, required unless
`--link-insecure`), the Redis session registry (optionally over TLS), `/metrics`
(labels `gateway_id`, `gateway_version`, `pool`, `backend_version`), `/healthz`,
`/readyz` on `--metrics-listen` (default `:9100`; not draining, under
`--soft-max-sessions`, and a worker attached to the default pool), the admin API and
pprof on their own `--admin` listener (default `127.0.0.1:9101`), SIGTERM drain
(`--drain-window`), the link write linger (`--link-write-linger`, see above), and `gocpp-gateway probe` (upgrade + BootNotification +
Heartbeat with the reserved identity `$probe-<id>`; the backend must answer it
without side effects) for use as a startup probe. Flags have `GOCPP_GATEWAY_*`
environment equivalents; secrets are not flags (`GOCPP_GATEWAY_ADMIN_TOKEN`,
`GOCPP_GATEWAY_REDIS_PASSWORD`, `GOCPP_GATEWAY_ATTR_SOURCE_TOKEN`, or the matching
`--...-file` flag). Example manifests: `examples/gke-gateway`. See "Security" below.

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

## Security

The gateway sits between the internet and your processing tier, and the link
carries every message, including the `Authorization` header of each connect. Treat
the link, the admin API and Redis as part of the trust boundary.

**The gRPC link requires mutual TLS.** `gocpp-gateway` refuses to start the link
unless `--link-tls-cert`, `--link-tls-key` and `--link-client-ca` are given: a
backend must present a certificate signed by that CA to attach. An attacker that
can attach as a backend sees every `ConnectRequest` (Basic credentials included)
and every CALL and can send `Reset`, `RemoteStart` or `UpdateFirmware` to any
charger, so there is no unauthenticated default. `--link-insecure`
(`GOCPP_GATEWAY_LINK_INSECURE=true`) turns the check off for local development
and logs a warning at startup. Backends use `linkgrpc.WithDialOptions` with
`grpc.WithTransportCredentials(credentials.NewTLS(...))` (client certificate,
the CA, and the server name the gateway certificate carries). A backend with an
authenticated identity is trusted to send control operations for any session, so
keep the CA private to the processing tier; `examples/gke-gateway/tls.yaml` issues
both sides with cert-manager.

**Admin API and pprof** (`/admin/status|shed|soft-max`, `/debug/pprof/*`) listen on
`--admin`, separate from `/metrics`, `/healthz` and `/readyz` (`--metrics-listen`).
The default is `127.0.0.1:9101`. Binding it to a non-loopback address requires a
token (`GOCPP_GATEWAY_ADMIN_TOKEN` or `--admin-token-file`); the process refuses to
start otherwise. The token guards pprof as well and is sent as
`Authorization: Bearer <token>`. Use `--admin-tls-cert/--admin-tls-key` (and
`--admin-tls-client-ca` for mutual TLS) when the listener is reachable over the
network: the token is otherwise readable on the wire. `POST /admin/shed` can
disconnect every charger of a gateway; keep the port behind a NetworkPolicy.

**gocppctl** reads the gateways' admin addresses from lease labels in Redis, so
anyone who can write to Redis can point it elsewhere. It therefore refuses to send
the token over plain HTTP to a host that is not loopback (`--admin-ca` or
`--admin-tls` make lease addresses `https://`; `--admin-insecure-token` overrides).
Secrets are never flags (`ps` shows them): `GOCPPCTL_ADMIN_TOKEN` or
`--admin-token-file`, `GOCPPCTL_REDIS_PASSWORD` or `--redis-password-file`. Redis
over TLS: `--redis-tls`, `--redis-ca`, `--redis-cert/--redis-key`; the gateway has
the same as `--redis-tls`, `--redis-tls-ca`, `--redis-tls-cert/--redis-tls-key`.
Restrict Redis with ACLs: the registry needs keys under its prefix
(`gocpp:sess:*`), `EVALSHA`/`EVAL`, and `PUBLISH`/`SUBSCRIBE` on its channels.

**The NATS link** has no gateway-side listener to secure; the broker is the trust
boundary, so configure it:

- Use TLS (`nats://` with `--nats-tls-ca`, or `tls://`) and authenticate every
  client (`--nats-creds`, or client certificates `--nats-tls-cert/--nats-tls-key`).
  The gateway logs a warning when it connects without either.
- Give gateways and backends separate NATS users with subject permissions (the
  subjects are listed above; `P` is the prefix, default `gocpp`):
  - a **gateway** publishes `P.in.>`, `P.connect.>`, `P.notify.>`, `P.who`,
    `P.cancel`, replies on `P.r.>` and `P.ack.>`/`_INBOX.>` as needed, and
    subscribes `P.hello`, `P.drain`, `P.gw.>` and its reply subjects `P.r.>`;
  - a **backend** subscribes `P.in.<pool>.>` (or the per-action subjects),
    `P.connect.<pool>`, `P.notify.<pool>`, `P.who`, `P.cancel`, `P.ack.>` and its
    reply subjects `P.r.>`; it publishes `P.hello`, `P.drain`, `P.gw.>` (its CSMS
    calls and controls) and replies on `P.r.>`.
  Anyone who may publish `P.gw.*.control` can control any session, and anyone who
  may subscribe `P.in.>` reads every message: grant backends only their own pool
  (a canary user gets `P.in.canary.>`, `P.connect.canary`, `P.notify.canary`) and
  do not give backends `P.hello` subscribe rights. Verify the set against your
  build with `nats sub 'gocpp.>'` on a test server; a denied subject shows up in
  the server log as a permissions violation.
- The tap (`--tap-nats`) publishes `gocpp.tap.>`; give only the consumers
  `subscribe` there, since frames carry meter values and identity tags.

**Chargers**: terminate TLS in the gateway (`--tls-cert/--tls-key`); with
`--tls-client-ca` a presented client certificate is verified (Security Profile 3,
the backend decides whether one is required). `--max-connections`, `--accept-rate`
and the per-tenant limits bound what an unauthenticated peer can use before the
backend authenticates it.

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
and `/admin/soft-max` on its admin listener (`gateway/admin`, `--admin-token-file`,
`--advertise-admin`), driven by `gocppctl gateway status|shed|cap|rebalance`.
`probe --interval` is a sidecar that exercises an idle gateway continuously, and
`--attr-source-file` / `--attr-source-url` (`csms.WithAttrSource`) add labels from
outside the backend as `ext.*` session attributes. The runbook is
[rollout.md](rollout.md).

## Performance

Measured with the load-test harness (`interop/loadtest`,
run `gateway-2026-11-21`): 4,000 chargers, loopback, gateway on 2 pinned cores,
2 backends on 2 cores each, gRPC link, schema off, compared with one embedded server
on 6 cores (throughput and memory rows), and the final acceptance run below. Targets are those of design section 2.7.

| Target | Result |
|---|---|
| Gateway >= 20,000 inbound CALLs/s per core | 22,700 msg/s per core at 36k msg/s (1.6 cores); the same gateway at 7.7k msg/s used 0.77 cores |
| Added latency, p50 <= 0.5 ms, p99 <= 2 ms at 5k msg/s | +0.15 ms p50, +0.5 ms p99 at 7.7k msg/s (Heartbeat p50 0.20 -> 0.35 ms, p99 0.49 -> 0.96 ms) |
| Gateway memory <= 16 KB per idle session | **met for live memory**: 11 KB heap+stack per idle session (`BenchmarkGatewayIdleFootprint`, 2,000 sessions; 14 KB at 10,000 sessions with real clients in the harness: 9.5 KB heap from a post-GC heap profile plus about 4.8 KB stacks). Process RSS is 24-26 KB/session at 10,000 sessions for the gateway and for the embedded server alike (main and integ/gateway measured the same, no regression): about 1.7x the live memory is GC headroom and unreturned idle spans left by the connect storm, see docs/performance.md, "RSS versus live memory" |
| Backend rollout: 0 disconnects, 0 attributable CALLERRORs | met, in the final acceptance run (4,000 chargers, 25 min, 40% mute, 3 flappers, 2 gateways, 3 backends): 5 backend restarts (3 SIGTERM, 2 SIGKILL) and 2 gateway drains in one run; chargers up never dropped below 3,987 during backend restarts; Heartbeat p99 in restart windows 1.1 ms (embedded server restarts: all 4,000 reconnect, 6-27 s to 99%) |
| Gateway rollout, reconnects within the pace | met, same run: SIGTERM drain of one of two gateways over 20 s dipped to 3,625 and 3,270 of 4,000 chargers up and was back to 99% within 1.8 s of the replacement being ready, no freeze. 10,000 chargers over 3 gateways, three drains in turn: dips to 9,565 / 9,364 / 9,015, all back within 3.3 s, no freeze (`results/gateway-final-2026-11-21/report.md`) |
| Gateway crash, accept capacity (500 handshakes/s per core) | not measured here (no TLS in the harness) |

Final acceptance run (run `gateway-final-2026-11-21`, split and embedded concurrently on pinned cores): Heartbeat steady p50/p99 0.5/1.1 ms against 0.3/0.5 ms embedded (+0.2 ms p50, +0.6 ms p99); at about 300 msg/s the whole split deployment used 0.32 cores against 0.12 embedded, dominated by fixed per-session costs (that run is not CPU bound; the throughput comparison is below).

Cost of the split: total CPU is about 1.4x the embedded server (3.3 vs 2.4 cores at 37k
msg/s); the gRPC hop is about 25 us of CPU per message over both ends. The profile
(the run's CPU profiles) shows the gateway at 27% in `write(2)`: the WebSocket reply write
and the link stream write, in roughly equal parts. Link write coalescing, a second stream
per backend and stack reservation in the backend's request goroutines followed (see
below): in the same harness at 35k msg/s total CPU went from 1.35x to 1.29x of embedded
(79.8 to 76.2 us per message against 59.1); a second stream did not change it. When the gateway is the bottleneck (it
ran at 80% of its 2 cores in the two top steps) p99 grows to tens of milliseconds, which
is queueing, not hop cost: size gateways below about 70% CPU.

### Write linger and stack growth: CPU against latency

A second round on the cost of the split (same harness and machine, 2026-11-21 evening;
one gateway, two backends, 4,000 chargers; the machine was shared, so compare the rows of
a table with each other, not with the tables above). Two changes came out of it.

**Stack growth, always on.** Each inbound CALL runs on a fresh goroutine, and the
gateway's forward path is about 10 KB deep, so the runtime copied the stack three or four
times per message: `runtime.newstack` was 9.6% of the gateway's CPU at 35k msg/s. A
gateway server now grows the handler's stack to 16 KB once, at the start of the goroutine
(`dispatcher.Config.DeepHandlerStack`, set by `csms.NewGatewayServer`), and the link-grpc
backend does the same for its request goroutines: `runtime.newstack` is down to 2.3% of the
gateway's CPU and total CPU per message is 3 to 4% lower (rows "before" and "linger off"
below). It costs no latency.

**Write linger, opt in** (see "Write linger" above for the semantics). Total CPU of the
split per message (gateway plus both backends) and Heartbeat latency at the chargers, with
the linger set on both ends:

Gateway with headroom (4 CPUs, two thirds busy without a linger; backends 4 CPUs; the
streams carry 250, 2,900 and 18,000 frames/s each way at the three rates). CPU is us per
message, the median of the undisturbed runs (range in brackets where it matters);
latency is Heartbeat p50 / p99 in ms:

| | 490 msg/s | 5,900 msg/s | 36,500 msg/s |
|---|---|---|---|
| embedded server, for scale | 280 us, 0.3 / 0.5 | 90 us, 0.2 / 0.5 | 57 us, 0.2 / 1.7 |
| split before this round | 1,210 us, 0.6 / 1.2 | 318 us, 0.5 / 1.1 | 137 us, 0.6 / 21 |
| split, linger off (the default) | 1,180 us, 0.6 / 1.1 | 306 us, 0.4 / 1.0 | 131 us (128-135), 0.5 / 15-36 |
| linger 200 us on both ends | 1,150 us, 0.6 / 1.2 | 288 us, 0.4 / 1.0 | 113 us (99-115): -13%, 1.2 / 10-21 |
| linger 500 us on both ends | 1,170 us, 0.5 / 1.1 | 306 us, 0.4 / 2.4 | 86 us (83-90): -34%, 2.4 / 15-16 |

The p99 of the last column is the harness at its limit on this machine in every row
(histogram buckets, a shared host); the linger does not show in it. At 5,900 msg/s the
500 us linger is armed now and then (the streams run at 2,900 frames/s against the 4,000
at which it applies steadily; arrivals cluster) and the p99 shows it; the p50 and the CPU
do not.

Gateway on 2 CPUs (the pinning of the measurements further up; 75% busy without a
linger), 36,000 msg/s, two passes each:

| | CPU us/msg | Heartbeat p50 / p99 ms |
|---|---|---|
| embedded server, for scale | 52-60 | 0.2 / 1.1 |
| split before this round | 89 (88, 90) | 0.9-1.0 / 41-46 |
| split, linger off (the default) | 87 (86, 88) | 0.6-0.7 / 29-32 |
| linger 200 us on both ends | 76 (75, 77): -12% | 1.1-1.2 / 21 |
| linger 500 us on both ends | 60 (58, 62): -31% | 2.4 / 23-26 |

Against the embedded server that is about 1.5-1.6x without a linger, 1.3-1.4x at 200 us and
1.05-1.15x at 500 us. Ratios to embedded do not carry over between sessions on this
shared machine: the binaries behind the 1.29x further up, run again in this session with
the same pinning, measured 91 us per message against 60 embedded (1.5x). The rows of one
table were measured together and compare.

Reading:

- Below the rate at which it applies the linger changes nothing, CPU or latency, because
  it is not armed: the first two columns at 200 us, the first at 500 us (the second is
  close enough to its threshold to show in the p99, as said above).
- Where it applies it trades latency for CPU, and more of both with a longer linger. The
  latency it adds to a round trip is not 2 x d but about 0.6 ms at 200 us and 1.8 ms at
  500 us: both directions wait, and the timers fire late whenever the process has an idle
  moment (the Go runtime's sub-millisecond timers, see above).
- The saving comes from fewer write and read system calls and, above all, fewer thread
  wake-ups on both ends (at 500 us in the first table the two backends together went
  from 2.1 to 1.2 cores and the gateway from 2.7 to 1.9). It shrinks as a gateway nears saturation, because a saturated gateway batches
  anyway (frames queue behind the write in progress) and pays for it in queueing: one
  pass with the gateway at 82% of its 2 CPUs showed -4% at 200 us instead of -12%.
- It depends on the rate of each stream, not of the gateway: with ten backends behind a
  gateway instead of two, the same traffic is 3,700 frames/s per stream and a 200 us
  linger never applies.

`BenchmarkGRPCHopRate` (`addons/link-grpc`) isolates one stream over loopback, both ends
and 1,000 open-loop senders in one process on 4 cores, and reports CPU per round trip and
round-trip latency:

| msg/s on the stream | linger | CPU us/msg | p50 ms | p99 ms |
|---|---|---|---|---|
| 1,000 | off / 200 us / 500 us | 278 / 293 / 332 (not armed; noise) | 0.17 / 0.19 / 0.22 | 1.1 / 1.5 / 1.2 |
| 5,000 | off / 200 us | 118 / 114 (not armed) | 0.13 / 0.13 | 0.36 / 0.36 |
| 5,000 | 500 us | 85 (-28%) | 2.5 | 4.4 |
| 20,000 | off / 200 us / 500 us | 96 / 50 (-48%) / 37 (-62%) | 0.20 / 1.5 / 2.8 | 2.1 / 3.1 / 3.7 |
| 40,000 | off / 200 us / 500 us | 66 / 38 (-42%) / 31 (-53%) | 0.23 / 0.87 / 2.5 | 4.1 / 3.7 / 4.7 |

The 5,000 msg/s row at 500 us shows the worst case for latency: just above the rate at
which the linger applies the process is mostly idle, every wait ends a full timer tick
late, and a round trip takes 2.5 ms instead of 0.13.

Guidance: leave the linger off (the default) unless one gateway-backend stream carries more
than about 10,000 msg/s and a millisecond of latency is a fair price for the CPU; then set
200 us on both ends (`--link-write-linger 200us` on the gateway,
`linkgrpc.WithBackendWriteLinger` in the backend) and look at 500 us only if the latency
budget allows 2 ms more. OCPP itself does not care (call timeouts are tens of seconds).
The latency targets above are stated at 5k msg/s: spread over two or more backends that
is below the rate of a linger of up to 400 us, which is then not armed and changes
nothing; a single stream at 5k msg/s with a 500 us linger is the worst case of the
benchmark table.

Considered in the same round and not done:

- A fixed linger (every frame waits): +1.3 ms on the Heartbeat p50 and a quarter more CPU
  at 260 msg/s, for the same saving at high rate. Dropped in favour of the adaptive one.
- Answering the charger from the link's read loop instead of waking the handler goroutine
  (no goroutine per request on the reply path): the scheduler work around that goroutine
  is the largest remaining block of the gateway profile after system calls (`runtime.mcall`
  and `schedule` 13-15%), of which this would remove perhaps a third, 5% of the gateway.
  It needs an asynchronous handler contract in the dispatcher and puts every charger's
  socket write on the one goroutine that reads the stream (a slow socket would stall the
  backend's whole stream), so it is a redesign, not a tuning.
- Sharing the WebSocket write with the link flush: the reply write is one `write(2)` per
  charger socket (14% of the gateway) and sockets cannot share a system call; nothing to
  gain without `io_uring`-style submission, which the Go runtime does not offer.
- A second stream per backend: measured before, no change.
- A precise timer for the wait (a thread sleeping in `nanosleep`): would bring the added
  latency close to 2 x d. Not done; the runtime's timers are good enough for an opt-in
  throughput setting. Revisit if someone needs the linger with a tight latency budget.

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
`Server.InvalidateAuthCache(cpID)` drops the entries of one gateway after a
credential change; on the processing tier `Server.InvalidateGatewayAuthCache(ctx,
cpID)` sends the request to every gateway with a lease in the session registry
(without a registry: to the in-process link's only gateway), so a revoked
credential stops being admitted at once instead of after `TTL` (and, with the
backend unreachable, `StaleIfError`). A gateway that is down misses the request.
Leave the cache off when the backend decides on other request headers.

### Reconnect through another gateway

When a charger reconnects through gateway B while its old socket is still open on
gateway A, the processing tier that sees the newer session (registry epochs order
the two) asks A to close the old socket at once (`ControlReplaced`, close code
1008); A's reconcile pass (every 30 s) is the fallback. The sender is the backend,
not B: gateways have no channel to each other.

### Deadlines across hosts

Deadlines are never compared across hosts. An inbound message carries its
`ReceivedAt` and `Deadline` from the gateway's clock and the backend uses the
difference as the handler's budget; a CSMS call carries `CallRequest.Budget`, the
time left, which the gateway applies from its own clock.

## Testing

`GOCPP_TEST_SPLIT=1 go test ./internal/...` runs the conformance and e2e suites
with every server split through the in-process link (CI does this on every PR).
Tests create servers with `internal/splitmode.NewServer`, which is
`csms.NewServer` plus `WithSplitInProcess` when the variable is set. 
`scripts/split-nats-suite.sh [go test args]` runs the same suites (about 3,100
tests, ten seconds) with every server split over the NATS link and a real
`nats-server` on loopback; CI runs it in the `gateway-modules` job. The main
module must not depend on `addons/link-nats`, so the script builds a throwaway
`go.work` and an overlay that registers the transport
(`addons/link-nats/splitharness`, `splitmode.Register` and
`GOCPP_TEST_SPLIT=nats`); nothing in the tree changes. It skips the e2e goleak
check, because the suites do not close every server they start (the in-process
split run keeps that check). Another transport can be run the same way by
implementing `csms.SplitLink` and registering it. The NATS link
has its own tests (`addons/link-nats`: the link contract over a real
`nats-server`, and a wire-identity test that diffs the frames of an embedded
server against a gateway with a NATS backend) and runs the whole chaos suite.

## Not yet

Without a registry a gateway numbers its sessions with a local counter, so
duplicate connections across two gateways are not detected yet.
