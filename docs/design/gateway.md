# gocpp gateway mode: splitting the connection tier from the processing tier

Status: design proposal, 2026-11-21. Based on gocpp `origin/main` at `e26604c` (v0.2.0+).
Artifacts: `/work/scratch/gocpp-gateway/hop-proto.go.txt` (hop-cost prototype),
`/work/scratch/gocpp-gateway/hop-proto-results.txt` (its output).

## 0. Summary

- **Split at the RPC level, not the frame level.** The gateway runs the whole of
  today's `dispatcher.Conn` (framing, CALL/CALLRESULT correlation, callSlot,
  keepalive, timeouts, frame observer). The only thing that moves out is the
  handler: on the gateway the handler registry has one catch-all entry that
  forwards `(cpID, action, payload)` upstream and waits for the reply. The
  backend holds no per-charger state at all.
- **The gateway is payload-opaque.** It parses the OCPP-J array and reads the
  action string, but never looks inside a payload: no schema validation, no
  lenient coercion, no typed structs. New actions, vendor DataTransfer, quirk
  fixes and schema-mode changes never require a gateway deploy. That is what
  makes "rarely redeployed" true in practice.
- **Per-charger ordering comes from OCPP itself, enforced at the edge**, not from
  the transport. A charger has one outstanding CALL; the gateway forwards the
  next only after replying to the previous one. So the inter-tier transport
  needs no partitioning and any backend can serve any message.
- **Default transport: gRPC bidirectional streams, backends dial gateways.**
  No broker on the hot path, real flow control, explicit drain. NATS is the
  second supported option. Durable logs (Kafka, Redis Streams, JetStream) are
  rejected for the request path and used only for one-way taps.
- **Session registry with registry-assigned epochs** replaces `PutGlobal`.
  Every write is conditional on the epoch, every inter-tier message carries it,
  gateways hold a lease instead of per-charger TTLs, and a reconcile loop makes
  the registry converge. This fixes the no-CAS weakness for existing clustering
  users as well.
- **A gateway deploy still reconnects its chargers** (a TLS socket cannot be
  moved between pods on GKE). The design makes that rare, paced and cheap:
  rate-limited idle-first close with 1012, accept-rate admission, TLS
  resumption. A backend deploy disconnects nobody.
- Embedded users change nothing. `csms.On` / `csms.Call` / generated
  `handlers` run unchanged in both modes; the conformance suite must pass in
  both.

## 1. Survey: how production systems split connection handling from processing

Source note: sections 1.1-1.3 are written from my knowledge of these systems'
public documentation and engineering posts; I did not re-fetch them today.
Numeric limits (AWS timeouts, Phoenix drainer defaults) should be re-checked
before being quoted externally. The OCPP-specific entries (1.2) are the ones I
am least certain about in detail.

### 1.1 General-purpose systems

| System | Edge → backend | Backend → client | Session state / presence | What to take from it |
|---|---|---|---|---|
| **AWS API Gateway WebSocket** | Each frame is one synchronous integration invoke (Lambda/HTTP), picked by a route selection expression; `$connect` (blocking, can reject, does auth), `$disconnect` (best effort, not guaranteed), `$default`. Integration timeout about 29 s. | `POST /@connections/{connectionId}`; `410 Gone` when the connection no longer exists; also GET (info) and DELETE (force close). | None for you: the backend stores `connectionId` itself (DynamoDB). Idle timeout 10 min, max connection lifetime 2 h. | The cleanest statement of the contract: connect / disconnect / message upstream as request-reply, plus an addressable callback API with a "gone" answer. `$disconnect` being best effort means presence needs a second mechanism (lease). The 2 h lifetime and per-frame invoke cost make it a poor fit for OCPP itself. |
| **AWS IoT Core** | MQTT broker; rules engine fans messages out to SQS/Lambda/Kinesis (asynchronous, at-least-once). | Publish to the device topic. | Client-id takeover (a new connection with the same client id closes the old one); lifecycle events on `$aws/events/presence/...` that are documented as possibly duplicated and out of order. | Takeover-by-id is the same rule as OCPP's "new connection replaces old". Out-of-order presence events are the norm: consumers need a version to order them (our epoch). |
| **Azure Web PubSub** | CloudEvents over HTTP webhooks: `connect` (blocking; can reject, set user id, pick subprotocol), `connected`, `disconnected`, user `message` events (synchronous, response body goes back to the client). `ce-connectionState` header lets the backend park small per-connection state at the edge and get it back on every event. | REST / server SDK: send to connection, user, group; close connection. | Service-side; backend is stateless. | Edge-held opaque connection state returned with every event (our "session attributes") removes a DB read per message from a stateless backend. Blocking connect hook = auth at the edge decided by the backend. |
| **Centrifugo / centrifuge** | Proxy hooks to the backend over HTTP or gRPC with a per-hook timeout: connect, refresh, subscribe, publish, rpc. | Server API (HTTP/gRPC): publish, disconnect, presence. Nodes share a broker/presence engine (Redis, NATS). | Presence in Redis with TTL; node-level registry. | Disconnect codes carry reconnect advice (one range means "reconnect", another "do not"). Proxy timeout is a first-class per-hook setting. gRPC is offered next to HTTP for the proxy because of connection reuse and deadlines. |
| **Discord gateway** | Gateway session processes talk to guild processes over the cluster's internal RPC. | Same path. | Session is resumable: `session_id` + sequence number + `resume_gateway_url`; the server replays missed events. | Reconnect-storm control is explicit: IDENTIFY is rate-limited in buckets (`max_concurrency`), opcode 7 asks a client to reconnect, heartbeats start with a random jitter. OCPP has no resume, so only the admission-control half transfers. |
| **Slack** | Gateway servers hold sockets; channel servers (stateful, placed by consistent hashing) do fan-out; an edge cache (Flannel) answers connect-time queries without the backend. | Channel server → gateway server → client. | Gateway servers subscribe per connection. | Answering hot, cheap queries at the edge (Flannel) is what kept reconnect storms from melting the backend; our analogue is the auth cache and optional edge Heartbeat. Draining gateway servers is a routine, paced operation. |
| **Pusher / Ably** | Webhooks / queue integrations for presence and client events. | HTTP publish API. | Ably: connection state kept about 2 min for recovery by key + serial. | At-least-once webhooks with event ids; backend must dedupe. |
| **Phoenix Channels** | In-process (process per channel), PubSub over distributed Erlang. | Same. | Presence is a CRDT replicated between nodes, no external store. | The socket drainer closes sockets in batches with an interval on shutdown: the standard shape for staggered close. Presence-by-replication is the alternative to a registry (considered in 2.5, not chosen). |
| **NATS-based gateways** | Gateway publishes a request on a subject, a queue group of workers answers on the reply inbox. | Worker publishes to a per-gateway or per-client subject. | Usually NATS KV or Redis. | Core NATS is at-most-once and has no backpressure: a slow subscriber is cut off, and "no responders" is reported immediately. JetStream adds at-least-once and a dedupe window, which is exactly what a request path with client-side timeouts does not want. |
| **Envoy / L7 WebSocket proxies** | After the upgrade the proxy is a byte pipe; it has no message awareness. | n/a | None. | An L7 proxy cannot be the OCPP gateway (no correlation, no per-message routing). Its drain closes the sockets, so every proxy deploy is a reconnect. On GKE, HTTP(S) load balancers also cap WebSocket lifetime at the backend-service timeout. Conclusion: L4 passthrough load balancer straight to the gateway pods, TLS terminated in the gateway (also required to read client certificates for Security Profile 3). |

### 1.2 OCPP-specific

| System | Mechanism (as I recall it) | Take-away |
|---|---|---|
| **AWS reference "OCPP gateway" sample** | NLB → Fargate containers hold the WebSockets; each charge point is an IoT Thing; the container bridges frames to IoT Core MQTT topics per charge point (in/out); a rule sends inbound messages to SQS → Lambda, which publishes the response to the out topic. | Frame-level relay over a broker. The backend must do CALL/CALLRESULT correlation and timeouts itself, and the path is asynchronous at-least-once. Simple to build, weak semantics. |
| **MaEVe (Thoughtworks)** | A Go `gateway` holds sockets; a stateless `manager` handles messages; MQTT between them with per-charge-station in/out topics and shared subscriptions. | Same shape as above; shows that the gateway/manager split is the accepted architecture for an open-source CSMS. |
| **CitrineOS** | A router module holds sockets, validates against schemas, gates on boot status using a cache, and exchanges messages with functional modules over a pluggable broker (RabbitMQ, Kafka, cloud pub/sub); pending calls are tracked in a cache with TTL. | The router there is not thin: validation and boot gating at the edge mean it changes with business rules. We deliberately go the other way. |
| **SteVe, the ocpp-go based monolith** | Single process. | The starting point. |
| **Commercial "OCPP proxy/broker" products** | Mostly multiplex one charger to several CSMSs or translate versions. | Different problem (fan-out), not tier splitting. I know of no established open pattern named "ocpp-broker"; EVerest/libocpp is charger-side. |

### 1.3 Design choices extracted

1. **Transport between tiers.** Three families: synchronous request-reply
   (API Gateway, Web PubSub, Centrifugo proxy), broker request-reply (NATS),
   and asynchronous logs/queues (IoT Core → SQS, CitrineOS, MaEVe). OCPP's
   inbound path is a client-timed RPC, so the first two fit; a log does not
   (see 6).
2. **Routing and ordering.** Systems that need per-key ordering across a broker
   pay for it with partitions and head-of-line blocking. OCPP does not need it:
   the protocol allows one outstanding CALL per direction, so holding the
   charger's next message at the edge until the reply is written gives total
   per-charger order with any backend picking up any message.
3. **Backpressure.** Only stream transports (HTTP/2 windows) and bounded
   in-flight counters give real backpressure. Brokers either drop (core NATS)
   or queue (logs), and queueing behind a 30 s client timeout is harmful.
4. **Delivery semantics.** OCPP has no idempotency key and chargers retry
   transaction messages with a new message id. At-least-once between tiers
   therefore creates duplicates the business layer cannot recognise by id. The
   request path must be at-most-once after dispatch; retries are allowed only
   when the gateway can prove the handler did not run.
5. **Timeouts.** One budget, set by the charger (typically 30 s). Every tier
   gets an absolute deadline derived from it; the edge answers before the
   charger gives up so the charger's single call slot is freed.
6. **Session state and presence.** Either the edge service owns it (API
   Gateway, Web PubSub) or a registry with TTLs does. Disconnect notifications
   are best effort everywhere, and presence events arrive out of order, so
   every event needs a per-session version.
7. **Drain.** Nobody migrates sockets. Everybody paces closes (Phoenix
   batches, Discord opcode 7, Centrifugo reconnect codes) and rate-limits
   re-admission (Discord IDENTIFY buckets).
8. **Auth at the edge.** Decided by the backend through a blocking connect
   hook, cached at the edge.
9. **Observability.** Trace context travels in the envelope; the edge is the
   only place that sees wire truth, so frame logging belongs there.
10. **Fairness.** Per-connection in-flight limits at the edge, then per-tenant
    and global caps; shed at admission (HTTP 503 before the upgrade) rather
    than after.

## 2. gocpp design

### 2.1 What runs where

Rejected alternative, for the record: a frame-level relay (gateway forwards raw
frames, the backend runs `dispatcher.Conn` over a virtual transport fed through
`Conn.handleMessage`). It keeps pending calls and the callSlot in the backend,
so a backend deploy loses in-flight calls and only one backend can talk to a
charger. That is the AWS-sample/MaEVe shape and it does not meet the goal.

| Concern | Gateway (edge) | Backend (processing) | Code today |
|---|---|---|---|
| TLS, mTLS client cert, HTTP Basic extraction, origin check, subprotocol negotiation | yes | | `csms/server.go` `ServeHTTP` |
| Credential decision (Security Profile 1-3) | asks the backend via `Upstream.Connect`, caches positive answers (hash, TTL 5 min), serves from cache when the backend is down | decides; invalidates the cache after a password/cert change | `core/auth` (`Authenticator` stays the embedded API; the gateway uses an `Authenticator` that calls `Upstream.Connect`) |
| OCPP-J framing, message type check per version, message id policy, max message size | yes | | `core/dispatcher/conn.go` `handleMessage`, `checkMessageID` |
| CALL ↔ CALLRESULT/CALLERROR correlation, message id generation for CSMS calls | yes | | `core/dispatcher/pending.go`, `call.go` |
| One outstanding CSMS call per charger, FIFO wait, queue bound, call timeout | yes | supplies a deadline | `core/dispatcher/callslot.go` |
| Inbound serialization (one handler at a time per charger), inbound queue bound | yes (`MaxConcurrentHandlers`, `InboundQueueSize`) | | `core/dispatcher/inbound.go` |
| WebSocket ping/pong, read idle watchdog, write watchdog | yes | sets values per connection in the `Connect` reply (same fields as `ConnTimeouts`) | `core/dispatcher/keepalive.go`, `csms.WithConnConfig` |
| Frame observer (wire log) | yes; optional lossy tap to a bus | | `core/dispatcher/frame_observer.go` |
| Duplicate-id policy (new connection replaces old) | locally as today; across gateways through the registry epoch | | `csms/server.go` `addConn` |
| Session registry writes, lease, drain | yes | reads | new, replaces `csms/cluster.go` `publishGlobal`/`retractGlobal` |
| Signed-message unwrap/verify, schema validation (strict/lenient/tolerant), lenient coercion, time encoding, outbound validation | **no** | yes | `core/dispatcher/handler.go` `runHandler`, `signing.go`, `call.go` `validateOutbound` |
| Typed decode, handlers, error mapping from handler errors | | yes | `csms/api.go` `On`, `v16/handlers`, `v201/handlers` |
| BootNotification gating, authorization, transactions, configuration | | yes | application, `addons/statefsm` |
| Built-in CALLERRORs for malformed frames, bad message ids, unknown message types | yes | | `conn.go` |
| Built-in CALLERRORs for unknown action, schema, decode | | yes (returned in the reply, written by the gateway) | `handler.go` |

Semantics that must stay at the edge, and why:

- **Correlation and synchronicity.** They are properties of one socket. Keeping
  them at the edge is what allows any backend to issue a call and any backend
  to handle a message.
- **Timeout authority.** The gateway is the last hop that can still answer the
  charger; it enforces the deadline and writes the CALLERROR.
- **Security Profile transport auth.** The TLS state and the upgrade request
  exist only at the edge. The backend sees an `Identity` and a certificate
  summary, not the handshake.
- **Keepalive.** Server-side ping interval and read timeout are per-socket
  timers. When the backend changes the charger's `WebSocketPingInterval` or
  `HeartbeatInterval` (ChangeConfiguration / SetVariables), the edge timers for
  that session must follow; v1 does that through `Control(UpdateSession)`
  carrying new `ConnTimeouts`, applied through the existing timer code in
  `keepalive.go`.

Semantics that deliberately do not go to the edge:

- **BootNotification gating.** It depends on business state (is this charger
  known, accepted, pending). The backend decides. To avoid a DB read per
  message it may store a small attribute set on the session (2.2,
  `SetSessionAttrs`, for example `boot=accepted`, tenant, quirk profile) that
  the gateway echoes on every envelope (the Web PubSub `connectionState`
  idea). Attributes are a cache: a gateway crash loses them, and a reconnect
  starts empty.
- **Heartbeat.** Answering it at the edge would remove most idle traffic from
  the backend but makes the gateway action-aware. Offered later as an explicit
  opt-in "edge responder" for Heartbeat only (phase 6), default off.

### 2.2 Inter-tier contract

New package `github.com/shiv3/gocpp/gateway` in the main module (interfaces,
envelope, in-process link; no new dependencies).

```go
// SessionRef identifies one WebSocket session. Epoch is assigned by the session
// registry at connect and is strictly increasing per CPID.
type SessionRef struct {
    CPID      string
    GatewayID string // stable pod identity, e.g. "gw-3"
    Boot      string // gateway incarnation (random per process start)
    Epoch     uint64
}

type Envelope struct {
    Proto      uint32            // contract version, starts at 1
    Session    SessionRef
    Version    ocppj.Version     // negotiated OCPP version
    Kind       Kind              // Call | Send | SessionOpened | SessionClosed | OrphanResult
    DeliveryID string            // Boot + per-gateway sequence; unique per dispatch attempt chain
    Attempt    uint32            // >1 only for a redispatch after a provable non-execution
    MsgID      string            // the charger's OCPP message id (inbound Call/Send)
    Action     string
    Payload    []byte            // raw JSON exactly as received; never re-encoded
    ReceivedAt time.Time         // gateway clock when the frame was read
    Deadline   time.Time         // absolute; the gateway answers the charger at this time
    Trace      string            // W3C traceparent (+ tracestate)
    Attrs      map[string]string // session attributes set by the backend (<= 1 KiB total)
}

type Reply struct {
    Payload []byte            // CALLRESULT payload
    Error   *WireError        // CALLERROR (code, description, details), or a sentinel
    Nack    bool              // handler did not run (backend draining/overloaded): safe to redispatch
}

// Upstream is what a gateway needs from the processing tier.
type Upstream interface {
    Connect(ctx context.Context, req ConnectRequest) (ConnectReply, error) // auth + admission + ConnTimeouts + initial Attrs
    Handle(ctx context.Context, env Envelope) (Reply, error)               // inbound CALL
    Notify(ctx context.Context, env Envelope) error                        // SEND (2.1), SessionOpened/Closed, OrphanResult
}

// Downstream is what the processing tier (or any other service) needs from gateways.
type Downstream interface {
    Call(ctx context.Context, req CallRequest) (CallReply, error) // CSMS-initiated CALL
    Send(ctx context.Context, req CallRequest) error              // OCPP 2.1 SEND
    Control(ctx context.Context, req ControlRequest) error        // Disconnect | SetSessionAttrs | UpdateSession | InvalidateAuth
}

type CallRequest struct {
    Target   SessionRef // Epoch 0 = "whatever session the gateway holds now"
    CallID   string     // caller-chosen idempotency key; the gateway dedupes within the pending window
    Action   string
    Payload  []byte
    Deadline time.Time
    Trace    string
}

type CallReply struct {
    Payload []byte
    Error   *WireError
    Sent    Sent // NotSent | SentNoResponse | Answered: tells the caller whether a retry can duplicate
    Session SessionRef // the session that actually served it
}
```

`WireError` generalises `remoteResponse`/`remoteError` in `csms/cluster.go`
(call error, sentinel by name, other) and adds `ErrSessionStale`,
`ErrGatewayDraining` and `ErrNoBackend`; the sentinel table there is reused.

Flows:

- **Inbound CALL.** reader → `handleMessage` → `enqueueInbound` → `runHandler`
  → catch-all forwarder → `Upstream.Handle` → reply → `sendCallResult` /
  `sendCallError`. The forwarder sets `Deadline = now + HandlerTimeout`
  (gateway default 25 s, to stay under the usual 30 s charger timeout). The
  charger's message id is passed through for logging and for
  `ocppj.MessageIDFromContext`; the gateway still does the correlation.
- **CSMS-initiated CALL.** backend `csms.Call(ctx, conn, ...)` → validate
  outbound → `Downstream.Call` to the owning gateway → `dispatcher.DoCall` on
  the local `Conn` (callSlot wait, write, pending) → `CallReply` → backend
  validates/decodes the response. The gateway uses
  `min(req.Deadline, CallTimeout)`. This is `Server.handleRemote` in
  `csms/cluster.go` with the epoch check and `Sent` added.
- **Session events.** `SessionOpened` (with remote address, selected request
  headers, TLS peer summary, identity from `Connect`) and `SessionClosed`
  (cause as a sentinel name, close code) go through `Notify`, at-least-once,
  to any backend, and are ordered by epoch on the consumer side: a
  `SessionClosed{epoch 5}` that arrives after `SessionOpened{epoch 6}` is
  ignored. This is the distributed form of the ordering `addConn` already
  guarantees locally (old disconnect before new connect).
- **Orphan results.** If the caller's stream is gone when the charger answers
  a CSMS call, the result is not dropped silently: it is emitted as
  `OrphanResult{CallID, Action, payload}` to any backend (best effort) and
  counted.
- **Cancellation.** When a session ends while a `Handle` is outstanding the
  gateway sends a cancel hint for that `DeliveryID`; in the backend it cancels
  the context returned by `csms.ConnContext`-style accessors, not the handler
  context (same rule as today: `handlerContext` in `handler.go` is detached
  from the connection).

**Same handler code in both modes.** The typed API does not change:

```go
// processing tier
srv := csms.NewServer(
    csms.WithGatewayLink(link),            // instead of serving HTTP
    csms.WithSessionRegistry(reg),
    csms.WithLenientSchema(), ...)         // schema options keep their meaning; they now run here
handlers.RegisterCSMS(srv, myHandler)      // generated registration, unchanged
csms.On(srv, profiles.BootNotification, onBoot)
go srv.ServeGateway(ctx)                   // blocks like http.Serve

// anywhere in the backend
conn, err := srv.Lookup(ctx, "CP_1")       // existing API (csms/cluster.go)
resp, err := csms.Call(ctx, conn, profiles.Reset, req)
```

What makes this work in the code:

1. `csms.On` (`csms/api.go`) today registers a `dispatcher.HandlerFunc` that
   wraps `&Conn{inner: dc}`. It will additionally keep the same closure in a
   server-side table keyed by action with the signature
   `func(ctx, *csms.Conn, payload) ([]byte, error)`. `ServeGateway` calls that
   table with a remote `*csms.Conn`.
2. `csms.Conn` already has a remote form (`inner == nil`, `remoteID`, `srv`).
   It gains a `SessionRef` and a metadata snapshot, so on a backend
   `ID()`, `Version()`, `Subprotocol()`, `RemoteAddr()` work from the envelope;
   `RequestHeader()` and `TLS()` return what `SessionOpened` carried (fetched
   lazily from the registry record when the backend did not see the open
   event). `Close(ctx)` and `Send` stop returning `ErrRemoteConn`: they map to
   `Control(Disconnect)` and `Downstream.Send`. `Stats()` stays zero and
   `Done()` nil for remote handles (documented).
3. The payload pipeline in `runHandler` (signed unwrap → schema validate /
   lenient coerce → handler → response validate → transform) and in
   `DoCall` (`validateOutbound`, response validation) is extracted into a
   `dispatcher.Pipeline` value that needs only `*Config`, version and action,
   not a `*Conn`. The embedded `Conn` and `ServeGateway` both call it, so the
   error codes (`SchemaErrorCode`, `DecodeErrorCode`, `ErrorCodeMapper`) are
   identical in both modes by construction.
4. `dispatcher.HandlerRegistry` gets `SetFallback(HandlerFunc)`; `runHandler`
   uses it when `Lookup` misses. The gateway is a `csms.Server` built with
   schema mode off, no handlers, and the forwarder as fallback. Everything
   else in `ServeHTTP` (admission, duplicate policy, `HoldInbound` until
   registered, `Shutdown`) is reused as is.
5. `WithOnConnect` / `WithOnDisconnect` are driven by `SessionOpened` /
   `SessionClosed` on backends, with at-least-once delivery and the epoch
   exposed on `Conn` (`conn.Session()`).
6. `Server.Get` and `Server.Connections` stay local-only (empty on a backend),
   as `docs/clustering.md` already states; `Lookup` is the portable call. A
   registry-backed `Server.Sessions(ctx)` iterator is added for listings.

Embedded mode is literally the same objects with the in-process link: nothing
is wired unless `WithGatewayLink` is given, and the current code path stays the
default.

### 2.3 Transports

`gateway.Link` is the pluggable pair (`Upstream` for gateways, `Downstream` for
backends) plus `Attach`/`Drain` lifecycle. Implementations:

| Transport | Where | Semantics | Verdict |
|---|---|---|---|
| **In-process** | `gateway/inproc` (main module) | Direct function calls, zero copy. | Tests, conformance in split mode, single-binary deployments. |
| **gRPC bidi streams** | `addons/link-grpc` | Backends dial every gateway and keep 1-2 `Attach` streams each. All message kinds are multiplexed on the stream with a correlation id. HTTP/2 flow control plus a per-stream in-flight cap give backpressure; `GOAWAY`-style drain message; keepalive detects dead peers in seconds. | **Default.** |
| **NATS core** | `addons/link-nats` | Inbound: `Request` on `gocpp.in.<version>.<action>` to a queue group. Downstream: `Request` on `gocpp.gw.<gatewayID>`. At-most-once; "no responders" gives fast failure; no backpressure beyond the gateway's own in-flight caps. | Supported second option, for deployments that already run NATS, need non-Go consumers, or want per-action worker pools by subject. |
| **JetStream / Kafka / Redis Streams / Pub/Sub** | tap exporters only | At-least-once, durable, replayable. | **Not for the request path.** Used for session events and the frame-observer tap, where consumers are idempotent by `(cpID, epoch, seq)`. |

Why backends dial gateways (and not the reverse):

- Gateways are few, long-lived and have stable names (StatefulSet + headless
  Service); backends are many, autoscaled and redeployed daily. Discovery of
  the stable side by the volatile side is trivial (DNS or the registry's
  gateway table); the reverse needs endpoint watching in the thin tier.
- A new backend is usable the moment it attaches; a leaving backend sends
  `Drain`, the gateway stops assigning new work to that stream, in-flight
  handlers finish, the stream closes. No charger notices.
- The same stream is the return path for CSMS calls to that gateway's
  chargers: one hop, no broker.
- Other services (Temporal workers, admin API) use the same client in
  caller-only mode (`Attach` with role `caller`: they can `Call`/`Control` but
  receive no inbound work). `addons/router-temporal` keeps working on top of
  `Downstream.Call` for durable, visible commands.

Backend selection per inbound CALL at the gateway: power-of-two-choices on
in-flight count across attached, non-draining streams. No stickiness by charge
point is needed (ordering comes from the edge) and none is wanted (stickiness
turns a backend deploy into a thundering re-hash).

Why not NATS as the default: it puts a clustered broker on the critical path of
every message for no ordering or durability benefit, drops instead of
backpressuring, and makes "is anyone handling this" a timeout question apart
from the no-responders case. Why not a log: see failure semantics; a message
answered after the charger's timeout is a duplicate generator.

Measured hop cost (prototype, loopback, 300-byte payloads, 4 cores shared by
both ends, `/work/scratch/gocpp-gateway/hop-proto-results.txt`):

| | 30k req/s open loop | saturation, 256 in flight |
|---|---|---|
| gRPC, 4 bidi streams | p99 1.17 ms, 1.51 cores both ends | 602k req/s, 6.3 us CPU/req both ends |
| NATS request-reply (embedded server, queue group) | p99 1.29 ms, 1.38 cores incl. broker | 408k req/s, 7.3 us CPU/req incl. broker |

Caveats: the open-loop latencies include the load generator's timer slack on
this WSL2 host (p50 of about 0.6 ms at low rates is the sleep granularity, not
the hop), and the open-loop CPU figures include one goroutine and one channel
per request in the generator. The useful conclusions are: (1) both transports
cost single-digit microseconds of CPU per round trip when busy and add well
under 1.5 ms p99 on loopback; (2) the choice between them is about semantics
and operations, not speed.

### 2.4 Session manager

New interface in `core/storage` (the global half of `ConnectionRegistry` is
kept only as a deprecated adapter):

```go
type Session struct {
    CPID, GatewayID, Boot string
    Epoch       uint64
    Version     string
    ConnectedAt time.Time
    Meta        map[string]string // remote addr, identity, cert subject, selected headers
}

type SessionRegistry interface {
    // Claim atomically assigns the next epoch for CPID and records the caller
    // as owner. It returns the displaced session, if any.
    Claim(ctx context.Context, s Session) (epoch uint64, prev *Session, err error)
    // Release removes the record only if it still has this epoch.
    Release(ctx context.Context, cpID string, epoch uint64) (released bool, err error)
    // Reassert restores a record that is absent or older than s.Epoch (reconcile).
    Reassert(ctx context.Context, s Session) (ok bool, err error)
    Lookup(ctx context.Context, cpID string) (Session, bool, error)
    LookupMany(ctx context.Context, cpIDs []string) ([]Session, error)

    // Gateway lease: one key per gateway, not per charger.
    Heartbeat(ctx context.Context, g GatewayInfo, ttl time.Duration) error
    Gateways(ctx context.Context) ([]GatewayInfo, error) // id, boot, address, state (serving|draining), session count
}
```

Rules:

1. **No unconditional write exists.** `Claim` is an atomic increment-and-set
   (Redis: one Lua script doing `INCR` on the epoch key and `HSET` on the
   record); `Release` is compare-and-delete on the epoch; `Reassert` is
   set-if-absent-or-older. This removes both weaknesses listed in
   `docs/clustering.md` (no CAS on `PutGlobal`; non-atomic lookup-then-delete
   without `OwnedGlobalDeleter`).
2. **Claim before accept.** In `ServeHTTP` the claim moves from after
   `dconn.Start` to before `websocket.Accept` (after auth, admission and the
   subprotocol pre-check). A charger does not have two admitted upgrades in
   flight unless it abandoned the first, so the registry's epoch order follows
   the order in which sockets were established. A failed accept releases its
   epoch.
3. **Fencing.** The epoch travels in every envelope and every `CallRequest`.
   A gateway rejects a `Call` whose epoch is non-zero and differs from its
   local session's (`ErrSessionStale`); the caller looks up again once.
   Backends ignore `SessionClosed` and late replies for epochs lower than the
   newest they have seen.
4. **Displacement.** When `Claim` returns `prev` on another gateway, the new
   owner sends `Control(Disconnect, prev)`; the old gateway closes that socket
   with `ErrConnReplaced` (1008, as `addConn` does locally today).
5. **Invariant and reconcile.** A gateway that holds a socket whose epoch is
   not the registry's current epoch for that id must close it; a gateway whose
   session has no registry record must `Reassert` it. Every gateway checks its
   sessions against the registry in batches (`LookupMany`, full pass every
   30 s). This is what makes the registry converge after lost notifications,
   registry failover or the residual race in which a claim for an abandoned
   upgrade lands late: the worst case is one spurious reconnect, never a
   permanently wrong route.
6. **Liveness by gateway lease.** A session is live only if its gateway's lease
   exists and the `Boot` matches. A crashed or restarted gateway invalidates
   all its sessions at once without touching per-charger keys (lease TTL 15 s,
   renewed every 5 s). Per-charger records carry a long TTL refreshed by the
   reconcile pass, only to garbage-collect.
7. **Lookup cost.** Backends keep a local cache `cpID → SessionRef` that is
   filled for free from inbound envelopes and session events and corrected on
   `ErrSessionStale` / `ErrNotConnected`. The registry is read on a miss.
   Inbound traffic never touches the registry.
8. **Registry outage.** Existing sessions keep working (inbound needs no
   registry). New upgrades wait up to 2 s for `Claim`, then are accepted
   unclaimed (epoch 0) and claimed in the background; reconcile repairs the
   rest. CSMS calls to a charger whose owner is not in a backend's cache fail
   with `ErrNotConnected` during the outage.

**Registry versus consistent hashing: registry.** Placement is decided by the
L4 load balancer and by which gateways have room; hashing charge point ids to
gateways would need L7 routing in front and would reshuffle sockets whenever
gateway membership changes, which is the storm we are removing. Presence
replication between backends without a store (the Phoenix approach, feasible
here because every backend is attached to every gateway) was considered; it
removes the store from the system but gives no authority for epochs and
nothing for services that are not attached. It remains possible later as a
cache-warming optimisation.

**Drain protocol for a gateway rollout** (`Server.Drain(ctx, DrainOptions)`,
also valuable for embedded users):

1. Mark the lease `draining`; readiness fails, so the load balancer sends no
   new upgrades; new upgrades that still arrive get `503` + `Retry-After`
   (the code path `reserveSlot` already has).
2. Close sessions at a fixed rate (default `sessions / window`, window 5 min,
   so 10,000 chargers leave at about 33/s), idle first: a session with a CALL
   in flight in either direction or with queued CSMS calls is skipped and
   retried, up to a per-session grace (default 30 s, one call timeout).
3. Close code **1012 Service Restart** with reason `gateway draining` (add
   `StatusServiceRestart` to `core/transport/ws.go`; today's `closeForCause`
   only knows 1000 and 1008). Queued, unsent CSMS calls fail immediately with
   `ErrGatewayDraining`, `Sent=NotSent` (safe to retry after re-lookup).
4. After the window, remaining sessions are closed regardless; calls that were
   written but unanswered return `Sent=SentNoResponse`.
5. Kubernetes: StatefulSet, one pod at a time, `terminationGracePeriodSeconds`
   above the window, PodDisruptionBudget `maxUnavailable: 1`, and capacity
   planned so that `G-1` gateways hold the fleet.

**Reconnect-storm control** (applies after a crash as well):

- `WithMaxConnections` per gateway (exists) sized for `fleet / (G-1)` plus
  headroom.
- New: accept-rate limiter in front of the TLS handshake (token bucket,
  default 200/s per gateway); excess connections are refused at TCP accept or
  answered `503` + `Retry-After` with a jittered value before the upgrade.
- TLS session resumption with ticket keys shared by all gateways, so a
  reconnect to a different pod avoids a full handshake.
- WebSocket close frames carry no retry hint, so jitter must come from the
  chargers: the backend should set `RetryBackOffWaitMinimum` /
  `RetryBackOffRandomRange` (2.0.1) or the vendor equivalent at boot. The
  gateway cannot enforce this; the admission limiter is the backstop.
- Auth cache at the edge so a storm does not become a backend/DB storm.

### 2.5 Failure semantics

Principle: **at-most-once after dispatch; retry only on proof of
non-execution; fail fast to free the charger's call slot.**

| Event | Gateway behaviour | Charger sees | Backend/caller sees |
|---|---|---|---|
| No backend attached | Hold the inbound CALL up to `UpstreamWait` (default 20 s) for a backend to attach; then CALLERROR | Normal reply if a backend appears; otherwise `InternalError` within its own timeout | n/a |
| Backend answers `Nack` (draining, overloaded) or the write to the stream fails before anything was sent | Redispatch to another backend, `Attempt+1`, within the same deadline | Nothing unusual | The second backend sees `Attempt=2` |
| Backend stream breaks after dispatch, before a reply | **No redispatch.** CALLERROR `InternalError` immediately | `InternalError`; chargers retry transaction messages per their retry settings and drop or retry others per spec | Handler may or may not have run: business idempotency by content (transaction id + sequence number in 2.0.1; connector + timestamp + meter start in 1.6), exactly as required today when a socket drops mid-handler |
| Handler exceeds the deadline | CALLERROR `InternalError` "handler timed out" (same text and code as `runHandler` today); a later reply is dropped and counted (`late_replies_total`) | `InternalError` | Its work is not rolled back; same as embedded mode |
| Gateway overloaded (global in-flight cap reached) | Readers stop reading: TCP backpressure per charger (existing `GlobalHandlerLimiter` behaviour) | Slower replies | n/a |
| Gateway crash | n/a | TCP reset or ping timeout, reconnect through the load balancer, admission-limited | In-flight `Call`s fail with `ErrConnDropped`; `Sent` is `SentNoResponse` if the request had been written. Sessions become dead when the lease expires (<= 15 s); `SessionClosed` is synthesised by lease expiry for consumers that track presence |
| Backend crash during a CSMS call it issued | The charger's answer is forwarded as `OrphanResult` | Nothing | The command may have executed; reconcile through the charger's follow-up messages, or issue commands through `router-temporal` when durability matters |
| Session replaced on another gateway mid-call | Old socket closed with 1008; pending calls fail with `ErrConnReplaced` | Old socket closed | Retry after lookup only if `Sent=NotSent` |
| Gateway ↔ backends partition, registry reachable | As "no backend"; after `NoBackendGrace` (default 60 s) the gateway goes not-ready and, if configured, slow-drains so chargers move to healthy gateways | CALLERRORs, then a paced reconnect | n/a |
| Gateway ↔ registry partition | Serve existing sessions; accept new ones unclaimed after 2 s; reconcile on recovery | Nothing | Calls to chargers not in the local cache fail with `ErrNotConnected` |

Why CALLERROR rather than silence when the backend is unavailable: a charger
has one outstanding CALL. Silence blocks all of its messages for the full
timeout and ends in the same retry decision as an immediate error. Why not hold
longer than 20 s: the reply must be on the wire before the charger's timer
(typically 30 s) fires, or the late reply will be matched against nothing and
the charger will already have retried.

Duplicate windows, stated plainly: (a) charger retries after an
`InternalError` whose handler did in fact run; (b) a CSMS call retried by a
caller after `SentNoResponse`. Both exist today in embedded mode (crash or
socket loss mid-handler); the split widens them by one network hop and adds
none, because the gateway never retries after dispatch and reports `Sent`
explicitly.

### 2.6 Observability and fairness

- **Tracing.** The gateway starts the span for an inbound message
  (`ocpp.inbound <Action>`: queue wait, upstream time, write) and injects
  `traceparent` into the envelope; the backend's existing `ocpp.handler` span
  becomes its child. For CSMS calls the backend's context is carried in
  `CallRequest.Trace` and the gateway span covers callSlot wait and wire round
  trip.
- **Metrics at the gateway** (extending `core/dispatcher/metrics.go`):
  sessions, accepts/s and rejections by reason, upstream latency histogram and
  outcome by action, in-flight per backend stream, redispatches, late replies,
  orphan results, callSlot wait, drain progress, registry errors, reconcile
  corrections.
- **Wire log.** `FrameObserver` runs at the edge only. A bounded, lossy
  exporter (drop with a counter, never block the write lock) ships frames to a
  bus for audit.
- **Fairness.** Per charger: one inbound handler in flight and a bounded queue
  (exists), plus a new per-connection token bucket on inbound frames to
  contain firmware loops. Per tenant: weighted in-flight caps keyed by the
  tenant resolved at connect (`addons/tenant`), phase 6. Global: in-flight cap
  with read-side backpressure (exists).

### 2.7 Performance targets and measurement

Baselines already measured for embedded gocpp (`docs/performance.md`): 13.6 KB
RSS per idle connection; 30k msg/s on 0.81 cores with schema off (the
gateway's configuration) and 1.16 cores lenient; Heartbeat p99 0.8 ms with
schema off.

| Target | Value | Rationale |
|---|---|---|
| Gateway memory | <= 16 KB RSS per idle session; 50,000 sessions in a 1 GiB pod | 13.6 KB today plus the session record and upstream bookkeeping |
| Gateway throughput | >= 20,000 inbound CALLs/s per core (round trips, schema off) | about 27 us/msg today at the edge plus a hop measured at 3-4 us per side |
| Added latency, gateway + hop, in-cluster | p50 <= 0.5 ms, p99 <= 2 ms at 5k msg/s; Heartbeat end-to-end p99 <= 5 ms at 30k msg/s | prototype p99 about 1.2 ms on loopback including generator slack |
| Backend | stateless; msg/s per core within 10% of embedded lenient mode minus socket work | same pipeline code |
| Backend rollout | 0 disconnects, 0 CALLERRORs attributable to the rollout, p99 < 2x steady state | Nack + redispatch |
| Gateway rollout, 10,000 chargers on 3 gateways | reconnect rate <= configured pace (50/s); no CSMS call lost that was `NotSent`; no freeze (canary Heartbeat RTT < 10 s) | drain protocol |
| Gateway crash | all sessions back within 2 min at 200 accepts/s per gateway; registry correct within 15 s + one reconcile pass | lease + admission |
| Accept capacity | >= 500 full TLS handshakes/s per core (ECDSA P-256), more with resumption | to be measured; sets the admission default |

How to measure, using `/work/wt/gocpp-loadtest/interop/loadtest` (branch
`test/loadtest-4k`):

- Parity server `interop/parity/cs-gocpp` gets `-mode embedded|gateway|backend`
  (same handlers in all three, which is itself the "same code" proof).
- The harness, which today supervises one `-server-bin`, gets a topology:
  `-gateways G -backends B`, per-process `-taskset` and resource sampling
  (it already reads RSS/CPU/threads/fds from `/proc` and goroutines from
  pprof), and `-restart-target backend|gateway|registry` with
  `-restart-mode term|kill`. Chargers connect through a tiny round-robin TCP
  splitter or a list of addresses.
- Runs: (1) idle footprint at 10k/50k sessions; (2) the existing 4,000-charger
  soak (`-steps 4000:50:25m -mute 0.4 -flappers 3 -dups 20`) in embedded and
  split topologies, compared in `report.py`; (3) rate ladder 5k/30k/60k msg/s
  for CPU per tier and latency per hop (gateway histograms give the split);
  (4) rollout runs for each restart target; (5) `-dups` across two gateways to
  exercise displacement; (6) the freeze canaries stay on in every run.
- As with the 2026-11-15 dev test, anything above about 2,500 simulated
  chargers must be driven from a GCP VM, not from Cloudflare egress.

## 3. Implementation plan

Legend: **L** = library (main module), **A** = addon module, **B** = reference
binary (own module, so the main module gains no gRPC/Redis dependency).

| Phase | Content | Kind | Acceptance criteria and tests |
|---|---|---|---|
| **0. Seams** (2 PRs) | (a) Extract `dispatcher.Pipeline` from `runHandler`/`DoCall`; (b) `HandlerRegistry.SetFallback`; `csms.On` also records the `*csms.Conn`-typed handler; `StatusServiceRestart`. No behaviour change. | L | Existing unit, conformance (`internal/conformance`), e2e and soak suites unchanged and green; benchmark `core/dispatcher/call_bench_test.go` and allocation per request within 2% of v0.2.0. |
| **1. Session registry with epochs** (2 PRs) | `storage.SessionRegistry` + memory implementation; `csms.WithSessionRegistry`; claim-before-accept, CAS release, displacement, reconcile loop, gateway lease; `Conn.Session()`; adapter keeps `WithConnectionRegistry` + `WithInstanceID` working; `addons/registry-redis` (Lua scripts). | L + A | Property test (`pgregory.net/rapid`, already a dependency): arbitrary interleavings of claim/release/reassert/delayed writes from N instances never leave a live socket unrouted after one reconcile pass, and never route to a dead one after lease expiry. `csms/cluster_test.go` scenarios pass through the adapter. Redis integration test with injected latency reproduces the stale-writer case from `docs/clustering.md` and shows it fixed. |
| **2. Drain and admission** (1-2 PRs) | `Server.Drain(ctx, DrainOptions)`, idle-first paced close with 1012, accept-rate limiter, jittered `Retry-After`, shared TLS ticket key helper. | L | synctest-based tests for pacing and idle-first; e2e: 2,000 simulated chargers, drain window 60 s, observed close rate within 10% of target, zero calls lost with `NotSent`. Loadtest: restart with drain versus today's SIGTERM, reconnect peak reduced to the configured pace. **Shippable on its own to embedded users and directly addresses the deploy storms.** |
| **3. Gateway contract, in-process** (3 PRs) | Package `gateway`: `Envelope`, `Upstream`, `Downstream`, `WireError`, in-process link; `gateway.NewServer` (a `csms.Server` with the forwarder as fallback, schema off); `csms.WithGatewayLink` + `Server.ServeGateway`; remote `csms.Conn` with session snapshot, `Close`/`Send` via `Control`; session events driving `OnConnect`/`OnDisconnect`; deadlines, `Sent`, orphan results. | L | The whole conformance and e2e suite runs a second time in "split in-process" mode and passes with identical wire output (diff of frame-observer logs between modes is empty for the deterministic cases). Example `examples/gateway-inproc`. No new dependency in `go.mod`. |
| **4. gRPC link and gateway binary** (3 PRs) | `addons/link-grpc` (proto, attach/drain/Nack, P2C selection, keepalive, mTLS between tiers); `cmd/gocpp-gateway` as its own module (flags/env only, health and metrics endpoints, Redis registry, graceful drain on SIGTERM); `docs/gateway.md`; GKE example manifests (StatefulSet, headless Service, L4 load balancer, PDB). | A + B | Integration tests with real processes: kill -9 a backend mid-handler (charger gets `InternalError`, no redispatch, no disconnect); SIGTERM a backend under load (zero CALLERRORs, zero disconnects); kill -9 a gateway (sessions re-established, registry correct within lease + reconcile, callers get `Sent` right); partition gateway↔backend and gateway↔registry with a TCP fault proxy (behaviour per table 2.5); duplicate id across two gateways. `goleak` clean. |
| **5. Load-test topology and gates** (2 PRs) | Harness topology flags, parity server modes, report comparison; record results for the targets in 2.7. | test tooling | Targets in 2.7 met or the gaps documented with profiles; 25 min soak with 4,000 chargers, 5 backend restarts and 2 gateway drains, no freeze. |
| **6. Options** (independent, as needed) | `addons/link-nats`; session attributes; edge Heartbeat responder; per-tenant caps; frame/session tap exporters; auth cache invalidation hooks; deprecate `storage.MessageRouter` in favour of `Downstream` (the three router addons become thin `Downstream` implementations or are retired). | L + A | Per feature. |

Backward compatibility for embedded users:

- No option changes meaning. Without `WithGatewayLink`, `WithSessionRegistry`
  or a `Drain` call the code path is today's.
- `WithConnectionRegistry` / `WithMessageRouter` / `WithInstanceID` keep
  working through an adapter (same guarantees as documented today); they are
  marked deprecated once phase 4 ships, and removed no earlier than v0.4.
- `Conn.Close` and `Send` on a remote handle change from `ErrRemoteConn` to
  working when the link supports control; that is a relaxation.
- Phases 0-2 are useful without ever running a gateway.

A point about the production migration: because the gateway is payload-opaque
and the contract is a small protobuf service, a backend does not have to be
written with gocpp's typed API to sit behind `cmd/gocpp-gateway`. The existing
monolith could implement `Upstream`/`Downstream` around its current handlers
and gain deploys without disconnects first, then move handlers to
`csms.On` at its own pace. Whether to do that is an owner decision (4.5).

## 4. Open decisions for the owner

1. **Default inter-tier transport.** Recommendation: gRPC bidirectional
   streams with backends dialling gateways; NATS as a supported addon later.
   Choose NATS first only if non-Go consumers or an existing NATS estate
   outweigh having a broker on the hot path.
2. **Registry store.** Recommendation: Redis (Memorystore) with Lua scripts
   for claim/release, since it is already the store behind `router-redis`; the
   interface is small enough for etcd or Postgres later. The inbound path does
   not depend on it, so a standard-tier instance is sufficient.
3. **Validation placement.** Recommendation: all payload validation and
   coercion in the backend, gateway payload-opaque. The cost is that a
   malformed payload travels one hop before being rejected; the benefit is
   that quirk fixes and schema-mode changes never touch the gateway. The
   alternative (strict validation at the edge, CitrineOS style) should be
   chosen only if protecting backends from garbage matters more than gateway
   stability.
4. **Behaviour when the backend is unavailable.** Recommendation: hold up to
   20 s, then CALLERROR `InternalError`; redispatch only on Nack; never retry
   after dispatch. The alternative, at-least-once with retries, needs
   content-based idempotency in every handler first.
5. **Order of adoption.** Recommendation: ship phases 1 and 2 (epoch registry,
   paced drain, admission) to embedded mode first because they address both
   past incident classes without any topology change, then phases 3-4. For the
   production CSMS, decide separately, with its maintainers, whether it
   attaches to the gateway through the raw contract before any handler port;
   this design does not presuppose a refactor of that service.

## 5. Staged releases: backend canary and gateway canary (addendum)

Status: addendum to sections 0-4, 2026-11-21. Written against `integ/gateway`
at `a75ff87` (phases 0 and 1 merged) and the in-flight phase 2 (`csms/drain.go`)
and phase 3 (`gateway/types.go`, `gateway/pool.go`) branches; type names below
are the ones those branches use.

Source note: the GKE and Cloud DNS behaviours in 5.2 (passthrough load balancer
keeping established connections to a not-ready pod, weighted DNS records) are
from memory of the public documentation and are listed as items to verify in
phase 5, not as measured facts.

### 5.0 Summary

- **Backend canary = a routing decision at the gateway.** Backends attach with
  a *pool* name and a version. For every inbound message the gateway asks a
  `Router` which pool serves it. The decision is a pure function of
  `(policy revision, charge point id, session attributes, action)`, so it is
  sticky per charger by construction, needs no shared state, and changes only
  when the policy or the attributes change. Promote and rollback are a policy
  revision swap: no socket is touched.
- **Routing attributes come from the processing tier.** `ConnectReply.Attrs`
  and `Control(SetAttrs)` put labels (site, tenant, vendor, model, firmware,
  any label the application has) on the session; rules match on them. A
  label-based staged release ("chargers/sites labelled X go first") is one
  rule on `label.X`, followed by percentage steps. An external label source is
  a pluggable `AttrSource`.
- **Gateway canary = the highest StatefulSet ordinal first, a capped share,
  and a partial drain to fill it.** An L4 load balancer cannot steer a
  charger to a pod, so the fraction is controlled with a soft session cap
  that drives readiness, and sessions are moved with the phase 2 drain
  (idle-first, paced, 1012) limited to a count.
- **Version skew is handled by a negotiated link protocol.** Every `Attach`
  starts with a hello carrying a protocol range and capability names, in both
  directions. Adjacent protocol versions must interoperate; new behaviour is
  gated on capabilities, never on build versions.
- **What the in-flight phases must include** is small and listed in 5.5: pool
  and version in the attach hello, protocol range and capabilities, the rule
  "no policy: only the default pool gets work", `SetAttrs`, partial drain, soft
  cap readiness, version labels on metrics and in the lease. The policy engine,
  tooling and automatic promotion are a new phase 7.

### 5.1 Backend canary

#### 5.1.1 Pools and backend identity

A backend declares who it is when it attaches. This is part of the link
contract from the first release, because gateways are the tier that is rarely
redeployed.

```go
package gateway

// DefaultPool is the pool of a backend that names none, and the only pool a
// gateway without a routing policy sends work to.
const DefaultPool = "default"

// BackendInfo is sent by a backend in the attach hello.
type BackendInfo struct {
    ID      string            // pod name; unique per process
    Pool    string            // "" = DefaultPool; e.g. "canary", "v0-9-3"
    Version string            // build version; a metric label, never used for routing
    Role    Role              // RoleWorker (takes inbound work) | RoleCaller (Call/Control only)
    Proto   ProtoRange        // link protocol versions it speaks (5.3)
    Caps    []string          // capability names (5.3)
    Actions []string          // optional: actions it has handlers for; empty = all
    Labels  map[string]string // free form, exported in status output only
}

// csms option on the processing tier:
//   csms.WithBackendInfo(gateway.BackendInfo{Pool: os.Getenv("GOCPP_POOL"), Version: buildVersion})
```

`gateway.Pool` (phase 3) stays the unit of selection: power-of-two-choices,
hold, Nack redispatch, drain, all *within one pool*. A new `PoolSet` holds one
`Pool` per pool name and creates/removes them as members attach and leave:

```go
type PoolSet struct{ /* map[string]*Pool, guarded; DefaultPool always exists */ }

func (s *PoolSet) Add(info BackendInfo, up Upstream) *Member // replaces Pool.Add(id, up) at link level
func (s *PoolSet) Pool(name string) *Pool                    // nil when no member has ever attached
func (s *PoolSet) Available(name string) bool                // >= 1 attached, non-draining member
func (s *PoolSet) Status() []PoolStatus                      // name, members (id, version, in-flight, draining)
```

Fail-safe rule, in force from phase 4: **a gateway sends inbound work only to
pools its routing policy names; without a policy that is `DefaultPool` only.**
A backend that attaches with `Pool: "canary"` to a gateway that has no policy
receives nothing (it can still issue CSMS calls). Starting a canary Deployment
is therefore never a traffic change by itself.

On GKE a canary is a second Deployment of the same image family
(`backend-canary`, 1-2 replicas, env `GOCPP_POOL=canary`); both Deployments
dial every gateway as today.

#### 5.1.2 Router and policy

```go
// RouteInput is what the gateway knows about a message without opening the payload.
type RouteInput struct {
    CPID   string
    Action string            // OCPP action, or "$connect", "$opened", "$closed" for session events
    Kind   Kind
    OCPP   ocppj.Version
    Attrs  map[string]string // session attributes
}

type RouteDecision struct {
    Pool     string // where to send
    Fallback string // pool to use when Pool is unavailable; "" = fail (hold, then CALLERROR)
    Rule     string // name of the rule that matched, "" = default; for metrics and traces
    Rev      uint64 // policy revision that produced the decision
}

// Router decides per message. Route must be pure, non-blocking and
// deterministic: same input and same policy give the same decision.
type Router interface {
    Route(in RouteInput) RouteDecision
}

// PolicySource delivers policy revisions. Implementations: static file,
// session registry, anything else (HTTP, CRD) outside the main module.
type PolicySource interface {
    // Watch calls apply with the current policy and then with every newer
    // revision until ctx ends. apply returns an error for an invalid policy;
    // the previous one stays in force.
    Watch(ctx context.Context, apply func(RoutePolicy) error) error
}

// csms.NewGatewayServer option:
//   csms.WithRouter(r gateway.Router)              // custom
//   csms.WithRoutePolicy(src gateway.PolicySource) // built-in policy router
```

The built-in router interprets a declarative policy (JSON or YAML):

```yaml
revision: 42            # strictly increasing; a gateway ignores revisions <= its current one
default: default        # pool for everything no rule matches
salt: rollout-0-9-3     # hash salt; change it per rollout to pick a different sample
rules:                  # first match wins
  - name: lab
    pool: canary
    match:
      attrs: { label.canary: ["1"] }           # AND across keys, OR across values, "*" suffix = prefix match
  - name: step
    pool: canary
    match:
      attrs: { tenant: ["t1"], vendor: ["Acme"], firmware: ["2.1.*"] }
      ocpp: ["ocpp1.6"]
      cp_ids: []                                # non-empty: only these ids (all matchers are AND-ed)
      percent: 5                                # of the chargers that pass the matchers above
    actions: []                                 # empty = all actions; else only these
    on_unavailable: fallback                    # fallback (to `default`) | error
    hold: busy                                  # none | busy (5.1.4)
```

Semantics, fixed now so that tools can reproduce them:

- **Percentage.** `bucket = uint64_be(SHA-256(salt || 0x00 || cpID)[0:8]) mod 10000`;
  the charger is in when `bucket < percent * 100`. Raising the percentage only
  adds chargers (a charger at 5% is still in at 10%); lowering only removes.
  The same salt across steps of one rollout keeps the sample stable; a new salt
  for the next rollout spreads the risk over different chargers.
- **Stickiness.** Rule membership (everything except `actions`) is evaluated
  once per session per policy revision and cached on the session
  (`sessionRoute{rev, matched bitset}`); per message the router only checks the
  action set. With an unchanged policy and unchanged attributes one charger's
  messages for one action always go to one pool. There is no random or
  load-based choice between pools, only within a pool.
- **By action.** A rule with `actions` routes only those actions to the pool;
  the charger's other actions fall through to later rules. The policy loader
  refuses, unless `allow_split: true` is set on the rule, an action set that
  separates the members of a transaction group
  (1.6: `Authorize`, `StartTransaction`, `StopTransaction`, `MeterValues`,
  `StatusNotification`; 2.x: `Authorize`, `TransactionEvent`, `MeterValues`,
  `StatusNotification`). The gateway stays payload-opaque: it compares action
  strings only.
- **Session events.** `Connect` is routed with action `$connect` and empty
  attributes, so id-list and percentage rules apply to it and attribute rules
  do not (attributes do not exist before the reply). `SessionOpened` /
  `SessionClosed` go to the pool of the session's `$opened` / `$closed`
  decision. `OrphanResult` goes to the pool of the caller that issued the
  call, falling back to the session's default decision.
- **Route in the envelope.** `Envelope.Route` (`Pool`, `Rule`, `Rev`) tells
  the backend why it received the message; it is copied into the handler span.

Policy sources shipped by gocpp:

| Source | Package | Propagation | Use |
|---|---|---|---|
| Static file (re-read on change and every 30 s) | `gateway/routepolicy` (main module) | ConfigMap sync time, about a minute on Kubernetes | Development, single gateway, GitOps-only setups |
| Session registry | `storage.PolicyStore`, implemented by `core/storage/memory` and `addons/registry-redis` | Push (Redis pub/sub) with a 5 s poll as backstop: under 1 s normally | Production: instant promote/rollback, compare-and-set writes, convergence visible in the gateway leases |

```go
package storage

type PolicyStore interface {
    // PutPolicy stores data as revision rev if the stored revision equals prevRev (CAS).
    PutPolicy(ctx context.Context, prevRev, rev uint64, data []byte) (ok bool, err error)
    Policy(ctx context.Context) (rev uint64, data []byte, err error)
    WatchPolicy(ctx context.Context, fn func(rev uint64, data []byte)) error
}
```

Every gateway publishes the revision it applied in its lease
(`GatewayInfo.Labels["policy_rev"]`), so a controller or `gocppctl route status`
can state "revision 42 is active on 4/4 gateways" instead of assuming it.
A gateway that cannot reach the source keeps its last policy; a gateway that
starts without any policy routes everything to `DefaultPool` (fail to stable)
and raises `gocpp_gateway_route_policy_missing`.

#### 5.1.3 Attributes and the label-based staged release

Sources of session attributes, merged in this order (later wins per key):

1. `ConnectReply.Attrs`: the backend already authenticates the charger at
   connect and knows its record; it returns `tenant`, `site`, and any release
   labels as `label.<name>=1`. This is the default and needs no gateway
   configuration.
2. `Control(SetAttrs)`: after BootNotification the backend adds `vendor`,
   `model`, `firmware`; an admin API (a `RoleCaller` link client) calls it
   with `Epoch: 0` when an operator changes a label of a connected charger.
   Keys with an empty value are deleted. The 1 KiB cap from 2.2 applies.
3. `gateway.AttrSource` (optional, phase 7):
   `Attrs(ctx, cpID) (map[string]string, error)`, resolved at connect with a
   timeout (default 200 ms, failure = no attributes, counted) and refreshed on
   an interval. For deployments where labels live in a system the backend does
   not own. Its keys are prefixed `ext.` and cannot override 1 and 2.

Reserved keys: the prefix `gocpp.` is interpreted by the gateway
(`gocpp.busy`, 5.1.4); everything else is opaque to it.

The registry keeps a copy for operators and tools, not for the hot path: on
claim and on each reconcile pass the gateway writes the attributes as
`Session.Meta["attr.<key>"]` and the current default decision as
`Session.Meta["route.pool"]`. `Server.Sessions(ctx)` can then list "which
chargers are on the canary" across gateways. Attributes are still a cache:
after a reconnect they are rebuilt from `ConnectReply` and the next
`SetAttrs`, so until the charger's BootNotification has been processed, rules
on `vendor`/`firmware` do not match it. Rules meant to hold across reconnects
should use connect-time attributes (labels, tenant, site) or the id.

Mapping of an existing "label-based staged release" in the application: the
labels on sites and chargers stay where they are (the application's database);
the backend's connect handler copies the release-relevant ones into
`Attrs`; the rollout is a sequence of policy revisions:
`label.canary` (internal and friendly sites) → `percent: 1` → `10` → `50` →
`100` → promote. gocpp only sees key/value attributes; what a label means
stays in the application.

#### 5.1.4 Promotion, rollback, transactions

- **Rollback** = a revision that removes the rule or sets `percent: 0`. Effect
  from the next message of each charger. Handlers already running on the
  canary finish and their replies are written; nothing is redispatched
  (2.5 is unchanged). Rollback ignores `hold`.
- **Promotion**, rolling form (default): raise to `percent: 100`, roll the
  stable Deployment to the new version (Nack + redispatch, zero disconnects,
  as for any backend deploy), then delete the rule and scale the canary to 0.
  Blue/green form: set `default: canary` in one revision, then retire the old
  pool. In both, chargers never notice.
- **Forward moves and `hold: busy`.** A percentage increase moves chargers
  from stable to canary. With `hold: busy`, a session whose attribute
  `gocpp.busy` is non-empty keeps its previous decision until the attribute is
  cleared, the session ends, or `max_hold` (default 4 h) passes. The backend
  sets `gocpp.busy=1` when a transaction starts and clears it when it ends,
  through `SetAttrs`. The gateway learns nothing about OCPP transactions.
  Default is `hold: none`: holding is a comfort, not the correctness
  mechanism.
- **The correctness mechanism is compatibility.** Backends are stateless and
  share the application's stores, so a transaction can always span versions:
  through a rollback, through a reconnect during a held move (the hold is
  lost at reconnect), through an action-scoped rule. Required of the
  application during a canary: version N and N+1 both read what the other
  writes (expand/contract migrations: add in N+1, stop using in N+2), and
  content-based idempotency for transaction messages as already required by
  2.5. The canary runbook states this as an entry criterion.
- **New message handling.** When only the canary understands an action (or a
  DataTransfer vendor id handled by a new code path keyed by action), use an
  action rule with `on_unavailable: error`, so the stable pool never answers
  `NotImplemented` for chargers in the rule. `BackendInfo.Actions` lets the
  gateway check this: a policy that routes an action to a pool where no
  member lists it is flagged in `route status` and counted
  (`gocpp_gateway_route_unhandled_total`).

#### 5.1.5 CSMS-initiated calls

Unchanged and pool-independent: every backend of every pool dials every
gateway, so `csms.Call` from stable or canary reaches the charger through the
owning gateway and the reply returns on the caller's stream. The charger's
follow-up messages (for example StatusNotification after a RemoteStart issued
by the canary) go to the pool the *charger* is routed to; that is the
compatibility requirement above again. A caller that wants to follow the
routing reads the charger's pool from `Envelope.Route` of its last message or
from `Session.Meta["route.pool"]`. The gateway records the caller's pool and
version with each pending call for metrics and for `OrphanResult` routing.

#### 5.1.6 When the canary pool is unavailable

| Situation | `on_unavailable: fallback` (default) | `on_unavailable: error` |
|---|---|---|
| Pool has no attached, non-draining member at dispatch | Send to `default` immediately (no hold) | Hold up to `UpstreamWait`, then CALLERROR `InternalError` (as 2.5) |
| Every member Nacks (proof of non-execution) | Redispatch to `default` within the deadline, `Attempt+1` | CALLERROR `InternalError` |
| Stream breaks after dispatch | CALLERROR `InternalError`, no redispatch (as 2.5) | same |
| Pool flaps (crash loop) | After a fallback the rule is *suspended* on that gateway for `suspend_for` (default 60 s): its chargers stay on `default` for that time, so a charger changes pool at most once per window instead of per message | n/a |
| Guard trips (below) | Rule suspended until the next policy revision | Rule suspended; affected actions get CALLERROR |

Gateway-local guard (automatic rollback without a controller): per rule,
`guard: {min_requests: 200, max_error_ratio: 0.05, window: 60s}`. If the
pool's ratio of failed outcomes (`timeout`, `stream_broken`, CALLERROR
`InternalError`) exceeds the limit in the window, the gateway suspends the
rule and reports `Labels["suspended"]` in its lease. It is deliberately
crude; the comparison against stable belongs to the controller (5.4).

### 5.2 Gateway canary

#### 5.2.1 Rollout shape on GKE

Constraints from 2.4: sockets cannot move between pods; the load balancer is
L4 passthrough, which spreads *new* connections over ready pods and offers no
per-pod weight; a freshly restarted gateway is empty.

Procedure (StatefulSet `gateway`, `G` replicas, `updateStrategy.rollingUpdate.partition`):

1. **Canary pod.** Set `partition: G-1`. Pod `gw-(G-1)` drains (phase 2:
   lease `draining`, not ready, idle-first paced 1012) and restarts on the new
   version. Its chargers reconnect to the other pods under the admission
   limiter.
2. **Pre-flight.** The new pod stays not-ready until `gocpp-gateway probe`
   (startup probe) has succeeded: against its own listener it performs, for
   each enabled OCPP version and security profile, upgrade → BootNotification
   → Heartbeat with a reserved probe identity (`$probe-<gateway id>`, which
   backends answer without business side effects), and attaches to at least
   one backend with a negotiated protocol. A gateway that cannot complete a
   handshake never receives a charger.
3. **Capped share.** The canary runs with `--soft-max-sessions = share x fleet`
   (for example 5%). At or above the soft cap the pod reports not-ready, so
   the load balancer stops sending it new connections; it keeps the sessions
   it has. (`WithMaxConnections` remains the hard cap that answers 503.)
4. **Fill.** `gocppctl gateway shed --count N` on the old gateways, one at a
   time: a partial drain (`DrainOptions.Max = N`, optionally
   `Filter` = sessions whose attributes match, so labelled lab chargers move
   first), during which the shedding gateway reports not-ready. Displaced
   chargers land on the remaining ready pods, the canary among them, until it
   reaches its cap. Churn to move `X` sessions to the canary is about
   `X x (G-1)` reconnects, all paced at the drain rate and admitted through
   the accept limiter; with `G = 4` and a 5% canary that is 15% of the fleet
   reconnecting once over the configured window.
5. **Observe** (5.2.4) for the soak time, then lower `partition` one ordinal at
   a time. Each step is an ordinary drain. Remove the soft cap.
6. **Rebalance.** After the last step, pods are uneven (the last one restarted
   empty). `gocppctl gateway rebalance` computes the fair share from the
   leases (`Gateways()` already reports session counts) and sheds the excess
   from pods above `fair x 1.1`, which are not-ready while shedding. This
   command is useful after any gateway rollout or crash, canary or not.

Variant for targeted testing or finer weights: a second StatefulSet
`gateway-canary` behind its own Service and load balancer address. Lab
chargers are pointed at its hostname; or the public hostname becomes a weighted
DNS record set (stable address, canary address), which sends a fraction of
*new resolutions* to the canary. It costs a second address and depends on
charger DNS caching, so it is the variant, not the default (open decision 2).

Unchanged requirements: capacity so that `G-1` gateways hold the fleet, PDB
`maxUnavailable: 1`, `terminationGracePeriodSeconds` above the drain window,
`minReadySeconds` so a crash-looping new pod stops the rollout.

Additions to `csms` for this (small, phase 2 scope):

```go
type DrainOptions struct {
    // ... existing fields ...
    // Max > 0 makes the drain partial: close at most Max sessions (idle first,
    // paced), then return. The server keeps serving, the lease state stays
    // "serving", and new upgrades are refused only while the partial drain runs
    // if RefuseDuring is set (default: not refused, only ReadyHandler reports
    // not-ready).
    Max    int
    // Filter restricts a drain to matching sessions (nil = all).
    Filter func(*Conn) bool
}

// WithSoftMaxConnections makes ReadyHandler report not-ready at or above n
// sessions while upgrades are still accepted up to WithMaxConnections.
func WithSoftMaxConnections(n int) Option
func (s *Server) SetSoftMaxConnections(n int) // runtime change (canary cap removal, rebalance)
```

To verify in phase 5 on the actual load balancer type: that a pod turning
not-ready keeps its established connections (connection persistence on
unhealthy backends), and how new connections are spread when every pod is
not-ready (expected: fail open across all pods).

#### 5.2.2 Sessions moving between gateway versions

A move is a reconnect: the new gateway `Claim`s, the registry assigns the next
epoch, callers with a stale `SessionRef` get `ErrSessionStale` and look up
again. None of this depends on the gateway build, because the registry, not
the gateway, assigns epochs. What mixed versions add:

- **Registry format.** Old and new gateways read and write the same records.
  Rule: record and lease encodings change additively only (the Redis addon
  stores JSON blobs, so unknown fields are ignored by old readers; writers
  must not drop fields they do not know when they rewrite a record: `Reassert`
  rewrites only its own record, which satisfies this). The Lua scripts are
  loaded per process by hash, so two script versions coexist; a script change
  must keep the key layout. A layout change is a two-release migration
  (release A reads both and writes old, release B writes new), gated on every
  lease reporting `Labels["reg"] >= n`. `GatewayInfo.Labels["version"]` makes
  the mix visible.
- **Attributes and holds** do not survive the move; they are rebuilt from
  `ConnectReply` (5.1.3). A charger moving between gateways therefore gets the
  same routing decision as before for connect-time attributes and ids, on any
  gateway version that has the same policy revision.
- **Policy support.** A gateway that does not implement routing (no `route.v1`
  capability, 5.3) sends everything to `DefaultPool`. During a gateway canary
  that introduces routing, do not start a backend canary; `gocppctl route put`
  refuses a policy with rules while any lease lacks the capability.
- **Ping-pong.** A charger that is shed from an old gateway, lands on the
  canary, and is drained back by a rollback has reconnected twice; epochs make
  both moves safe. The shed filter skips sessions younger than
  `MinSessionAge` (default 10 min) so one charger is not moved repeatedly.

#### 5.2.3 Rollback

Raise `partition` back (or roll the canary StatefulSet back). The canary pod
drains like any gateway; because its share was capped, the reconnect volume is
bounded by the cap. If the canary is harming sessions (corrupt frames, stuck
writes), drain with a short window (`--drain-window 30s`) or delete the pod:
the lease expires within 15 s and the accept limiter paces the return. If the
canary never passed pre-flight it holds no charger and rollback is free.

#### 5.2.4 Verifying a gateway canary

Every gateway metric carries the constant labels `gateway_id` and
`gateway_version`. Compared between canary and stable gateways:

| Signal | Metric | Gate (canary vs stable, same window) |
|---|---|---|
| Handshake errors | `gocpp_gateway_accepts_total{result}` with `result` = `ok`, `tls_error`, `auth_rejected`, `subprotocol`, `rate_limited`, `full`, `draining`, `upstream_error` | `tls_error + subprotocol + upstream_error` ratio not above stable + 0.5 pp |
| Connection churn | `gocpp_gateway_session_duration_seconds` (histogram at close), `gocpp_gateway_claims_total{prev="none\|dead\|live"}`, `gocpp_gateway_reclaim_interval_seconds` (time since the displaced session's `ConnectedAt`) | share of sessions closed within 10 min not above stable x 1.5; no charger reconnecting more than 3 times in 10 min |
| Close mix | `gocpp_gateway_closes_total{code,cause}` | abnormal (1006, ping timeout, write timeout) ratio not above stable x 1.5 |
| Liveness of sockets | ping RTT histogram, `pong_timeouts_total`, `write_errors_total` | p99 RTT within 2x |
| Inbound path | `gocpp_gateway_upstream_seconds{action}`, `gocpp_gateway_upstream_requests_total{outcome}` | CALLERROR mix equal within noise; p99 within 1.5x |
| CSMS calls | `gocpp_gateway_downstream_calls_total{outcome}`, callSlot wait | `timeout` ratio not above stable |
| Link | `gocpp_gateway_link_attach_total{proto,result}`, `link_unsupported_total{kind}` | every backend attached; zero rejects |
| Registry | `reconcile_corrections_total`, `registry_errors_total` | zero corrections after the fill settles |
| Resources | RSS per session, goroutines, fds | within the 2.7 targets |

Plus the synthetic probe from 5.2.1 running continuously against each gateway
pod through the headless Service, so an idle canary is still exercised.

### 5.3 Link protocol versioning and capability negotiation

Applies to every networked link (`addons/link-grpc` first). The in-process
link has one build on both sides and skips it.

```go
type ProtoRange struct{ Min, Max uint32 } // inclusive; phase 4 ships {1, 1}

// First message on every Attach stream, backend → gateway.
type Hello struct {
    Backend BackendInfo // includes Proto and Caps
}

// Answer, gateway → backend. Proto 0 means rejected (Reason set), stream closes.
type HelloAck struct {
    Proto   uint32      // min(backend.Max, gateway.Max), valid if >= both Min
    Caps    []string    // intersection of both sides' capabilities
    Gateway GatewayPeer // ID, Boot, Version, PolicyRev
    Reason  string
}
```

Rules:

1. **Negotiated per stream.** `Envelope.Proto` carries the negotiated value;
   a side never sends a message kind, control op or field semantics that the
   stream did not negotiate. Different streams of one gateway may run
   different protocols during a rollout.
2. **Support window.** Every release speaks its own protocol and the previous
   one (`Min = Max - 1` after the first bump). Skipping a release on one tier
   is therefore safe; skipping two is not, and attach fails loudly
   (`link_attach_total{result="proto_mismatch"}`, backend not ready) instead of
   degrading.
3. **Additive within a protocol version.** New protobuf fields are optional
   and ignored by old peers. A protocol bump is needed only when ignoring a
   field would be wrong.
4. **Capabilities gate behaviour.** Named, independent of the number:
   `attrs.set`, `route.v1`, `send.21`, `cancel`, `orphan`, `actions.list`.
   Callers ask `link.Has(cap)`; the typed API returns
   `gateway.ErrUnsupported` (a `WireError` kind) when the peer lacks one.
5. **Unknown is an answer, not a failure.** A peer that receives an unknown
   `Kind` or `ControlOp` replies `ErrUnsupported` and keeps the stream.
   Breaking the stream on unknown input would turn skew into an outage.
6. **Capability-aware selection.** `Pool.pick` skips members that lack the
   capability a message needs (for example `KindSend` requires `send.21`); if
   no member of the pool has it, the outcome is `no_backend` after the hold.
7. **Order of deployment for a feature needing both tiers:** gateways first
   (the capability is advertised but unused), then backends start using it;
   removal in the reverse order. Old backend ↔ new gateway always works by
   rule 1; new backend ↔ old gateway works by rules 4 and 5.

CI enforces the window: a compatibility job builds the gateway and the parity
backend from the previous release tag and runs the link integration suite in
both mixed combinations.

### 5.4 Observability per backend version, and promotion

Gateway metrics for routing (the gateway is the only place that sees both
versions on identical terms; labels are bounded: pools and attached versions
are few, actions are those of the OCPP version):

- `gocpp_gateway_upstream_requests_total{pool, backend_version, action, outcome}`
  with `outcome` = `result`, `callerror_<Code>` (the code is in
  `Reply.Error.Code`, no payload parsing), `timeout`, `nack`, `stream_broken`,
  `no_backend`
- `gocpp_gateway_upstream_seconds{pool, backend_version, action}` (histogram)
- `gocpp_gateway_route_decisions_total{rule, pool}`,
  `gocpp_gateway_route_fallback_total{rule, reason}`,
  `gocpp_gateway_route_suspended{rule}`, `gocpp_gateway_route_unhandled_total{pool, action}`
- `gocpp_gateway_route_sessions{pool}` (sessions whose default decision is the
  pool), `gocpp_gateway_route_held_sessions{rule}`
- `gocpp_gateway_route_policy_revision`, `gocpp_gateway_route_policy_missing`,
  `gocpp_gateway_route_policy_rejected_total`
- `gocpp_gateway_pool_members{pool, backend_version, state}`
- `gocpp_gateway_downstream_calls_total{caller_pool, caller_version, action, outcome}`
- `gocpp_gateway_late_replies_total{pool}`, `gocpp_gateway_orphan_results_total{pool}`

Backends add a constant `version` label and the `pool` to their existing
handler metrics and put `Envelope.Route` on the handler span, so traces can be
filtered by rule.

Promotion criteria (same actions, canary pool vs default pool, same window,
minimum 500 canary requests per evaluated action group):

- failed-outcome ratio (`timeout`, `stream_broken`, `callerror_InternalError`)
  not above stable + 0.5 pp;
- CALLERROR code distribution: no code whose share exceeds stable's by more
  than 1 pp (catches a new `FormationViolation` / `PropertyConstraintViolation`
  / `NotImplemented` pattern);
- p99 `upstream_seconds` not above 1.5x stable (and under the 2.7 budget);
- `late_replies` and `orphan_results` not above stable;
- application-level checks supplied by the user as extra queries (for example
  transactions started per connected charger).

Manual first: `gocppctl route status|put|set-percent|promote|rollback` writes
revisions through `PolicyStore` with compare-and-set and waits for all leases
to report the revision. Automatic promotion is a controller that runs a stage
list (`label → 1% → 10% → 50% → 100%`, soak time per stage), evaluates the
criteria from Prometheus, and calls the same store; it rolls back by writing
one revision. It lives outside the gateway (`addons/rollout`, also usable as a
library or driven by an existing progressive-delivery tool through the CLI).
The gateway-local guard (5.1.6) is the only automatic action inside the hot
path.

### 5.5 Phase plan update

Additions to the phases already planned. Phases 0 and 1 are merged, so their
additions go into the PRs named below; all of them are additive.

| Phase | Addition (exactly this, nothing more) | Why it cannot wait |
|---|---|---|
| **1** (follow-up, ride with the phase 2 PR) | `storage.GatewayInfo.Labels map[string]string`, stored in the lease by the memory registry and `registry-redis` (new JSON field `labels`); gateways set `version`. Document the reserved `Session.Meta` prefixes `attr.` and `route.`. | Lease format is shared by gateway versions; adding it later is a mixed-format rollout. |
| **2** | `DrainOptions.Max` and `DrainOptions.Filter` (partial drain: server keeps serving, lease stays `serving`); `WithSoftMaxConnections` / `SetSoftMaxConnections` feeding `ReadyHandler`; `ReadyHandler` not-ready while a partial drain runs; metrics `session_duration_seconds`, `closes_total{code,cause}`, `accepts_total{result}`. | Gateway canary fill and rebalance are built from these; they also serve embedded users. |
| **3** | `gateway.BackendInfo`, `DefaultPool`, `Role`; `PoolSet` wrapping `Pool`; `BackendLink.Serve` and the in-process hub take a `BackendInfo` (`csms.WithBackendInfo`); `gateway.Router`, `RouteInput`, `RouteDecision`, `csms.WithRouter`, with the built-in default router returning `DefaultPool`; the edge forwarder calls `Router.Route` and dispatches through `PoolSet` (including the fallback and the "only named pools get work" rule); `Envelope.Route`; `ControlSetAttrs` with `ControlRequest.Attrs` (merge, delete on empty value, 1 KiB cap, re-evaluate the session's cached route); `ErrUnsupported` in the `WireError` kinds. Test: split-mode conformance with two pools and a 50% test router gives identical wire output; a backend in a non-default pool receives nothing without a router. | These are the seams. The policy engine later plugs into `Router` without touching the forwarder or the links. |
| **4** | Proto: `Hello` / `HelloAck` as in 5.3 (protocol range, capabilities, `BackendInfo`, `GatewayPeer`), field numbers reserved for `Envelope.route`, unknown kind/op → `ErrUnsupported` without closing the stream, capability-aware `Pool.pick`; labels `pool`, `backend_version`, `gateway_version` on the gateway metrics of 5.4 that exist without a policy (`upstream_*`, `pool_members`, `downstream_calls`, `link_attach_total`); `cmd/gocpp-gateway`: `--soft-max-sessions`, `/readyz` from `ReadyHandler` plus "at least one worker attached to the default pool", lease labels `version`, `caps`; `probe` subcommand and the reserved `$probe-` identity; manifests with `partition`, `minReadySeconds`, startup probe. Chaos test added: a stub peer speaking an unknown kind and a peer with a disjoint protocol range. CI: the previous-release compatibility job (active from the second release; the job exists and is skipped until a tag has the binary). | The wire contract and the gateway binary are what get deployed rarely; they must be forward-compatible on day one. |
| **5** | Harness: `-backend-pools default:3,canary:1`, `-restart-target` accepts a pool; a scenario for partial drain + soft cap (gateway canary fill) recording reconnect rate and churn; verification on GKE of the two load balancer behaviours listed in 5.2.1. | Gates for phase 7 need the topology. |
| **6** | "Session attributes" leaves this phase (moved to 3 and 7). The rest is unchanged. | |

New phase:

| Phase | Content | Kind | Acceptance criteria and tests |
|---|---|---|---|
| **7a. Routing policy** (2 PRs) | `gateway/routepolicy`: `RoutePolicy` schema, validation (transaction-group lint, unknown pools, capability check), compiled router with per-session cache, percentage hash, `on_unavailable`, suspend, `hold: busy`, guard; `PolicySource` static file; `storage.PolicyStore` + memory + `registry-redis` (CAS, pub/sub watch); lease `policy_rev`; registry copy of attributes and route; routing metrics of 5.4; `gocppctl route ...`; `docs/staged-releases.md` (canary runbook, compatibility entry criteria). | L + A + B | Property tests (`rapid`): `Route` is deterministic; percentage is monotonic and within ±1 pp of nominal over 100k ids; with a fixed revision and arbitrary interleavings of messages, member attach/detach and Nacks, a `(charger, action)` pair changes pool only at a suspend edge and at most once per `suspend_for`. Split-mode conformance with a policy at 50% is wire-identical. Load test, 4,000 chargers: `label → 10% → 50% → 100% → rollback`, zero disconnects, zero CALLERRORs attributable to the changes, every gateway on the new revision within 2 s. Chaos during a canary: kill -9 all canary backends mid-handler (in-flight get `InternalError` once, following messages reach `default` without the 20 s hold); canary crash loop every 5 s (bounded pool changes per charger); guard trips on an error-injecting canary within its window; policy store down (last policy kept; a restarted gateway fails to `DefaultPool`); one gateway partitioned from the store while the revision advances (lag visible in leases, CLI refuses to report success); invalid policy pushed (rejected, previous kept); held session survives a percentage increase and moves after `gocpp.busy` clears; rollback ignores the hold; CSMS calls from each pool to chargers routed to the other; orphan result when the calling canary backend dies. |
| **7b. Gateway canary tooling** (1-2 PRs) | `gocppctl gateway status\|shed\|rebalance` (drives `Drain` with `Max`/`Filter` through an admin endpoint of `cmd/gocpp-gateway`), `MinSessionAge`, continuous probe, gateway comparison dashboards/queries for 5.2.4, runbook for the partition procedure and the separate-Service variant; `gateway.AttrSource`. | B + docs | Real-process test with 3 gateways (one built from the previous tag once it exists): canary pod, 5% soft cap, shed, observe, complete, rebalance: reconnect rate never above the configured pace, final session counts within 10% of fair share, epochs strictly increasing per charger, no `NotSent` call lost. Chaos: kill -9 the canary gateway during fill; rollback drain of the canary while another gateway sheds; registry outage during shed. |
| **7c. Automatic promotion** (1 PR, optional) | `addons/rollout`: stage list, Prometheus analysis with the 5.4 criteria, promote/rollback through `PolicyStore`, dry-run mode, Kubernetes events/log of every decision. Default off. | A | Simulation tests with recorded metric series (healthy, error spike, latency regression, too few samples → no decision); end to end on the load-test topology with an error-injecting canary: rolled back automatically within one evaluation interval + guard window. |

### 5.6 Open decisions

1. **Who promotes automatically.** Recommendation: the gateway only ever
   suspends (local guard); promotion steps come from outside it. Ship the CLI
   and metrics (7a) first and operate canaries manually for a few releases;
   add `addons/rollout` (7c) afterwards, and prefer driving the CLI from an
   existing progressive-delivery tool where one is already in use. The
   alternative, a controller inside the gateway binary, puts a Prometheus
   dependency and release logic into the tier that must stay thin.
2. **How a gateway canary gets its share.** Recommendation: same Service,
   highest ordinal first, soft cap + partial drain (5.2.1): no extra
   infrastructure, exact cap, and the same primitives rebalance after every
   rollout. The alternative, a separate Service with weighted DNS, gives
   finer steering of new connections and a dedicated hostname for lab
   chargers at the cost of a second load balancer address and reliance on
   charger DNS behaviour; keep it documented for the lab-charger case.
3. **Where the routing policy lives at runtime.** Recommendation: the session
   registry (`PolicyStore`, compare-and-set, sub-second watch, applied
   revision visible per gateway), with the file source for development and
   with Git as the declarative origin applied by `gocppctl route put` in the
   delivery pipeline. The alternative, a ConfigMap only, is simpler but makes
   rollback take as long as ConfigMap propagation (about a minute) and gives
   no convergence signal.
