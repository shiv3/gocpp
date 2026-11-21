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

## Links

| Link | Package | Use |
|---|---|---|
| in-process | `gateway/inproc` | tests, conformance, single-binary deployments |
| gRPC (backends dial gateways) | `addons/link-grpc` (own module) | the default for separate processes |

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

Session attributes are the routing labels: `ConnectReply.Attrs` at connect, and
`Conn.SetAttrs` (or `Server.SetSessionAttrs`) from the
backend afterwards (merge; an empty value deletes; 1 KiB cap); the
router sees them from the next message. A rollback is swapping the router; no
socket is touched. The declarative policy engine, percentage rules and tooling
are a later phase (design section 5).

## Testing

`GOCPP_TEST_SPLIT=1 go test ./internal/...` runs the conformance and e2e suites
with every server split through the in-process link (CI does this on every PR).
Tests create servers with `internal/splitmode.NewServer`, which is
`csms.NewServer` plus `WithSplitInProcess` when the variable is set.

## Not yet

Session registry with epochs across gateways, drain with paced close, accept
rate limiting (design phases 1 and 2), the gRPC link and the reference gateway
binary (phase 4), an edge Heartbeat responder, per-tenant
caps (phase 6). Without a registry a gateway numbers its sessions with a local
counter, so duplicate connections across two gateways are not detected yet.
