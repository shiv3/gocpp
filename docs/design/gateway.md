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
