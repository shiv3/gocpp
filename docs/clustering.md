# Running several CSMS instances

A charge point holds one WebSocket to one CSMS instance. With several instances
behind a load balancer, a command for a charge point (Reset, RemoteStartTransaction,
...) may be issued on an instance that does not hold its socket. gocpp forwards
such calls to the owning instance.

For the split deployment (a connection tier that holds the sockets and a stateless processing tier) see [gateway.md](gateway.md).

## Setup

> `WithMessageRouter` and the router addons are deprecated in favour of the gateway link; see [Moving to the gateway link](#moving-to-the-gateway-link). This page describes the cluster mode that remains supported until then.

```go
srv := csms.NewServer(
    csms.WithInstanceID("csms-2"),            // unique per running instance; enables clustering
    csms.WithConnectionRegistry(registry),    // shared route table (global part)
    csms.WithMessageRouter(router),           // forwards calls between instances
)
```

* The **registry** (`storage.ConnectionRegistry`, or a `storage.SessionRegistry` via
  `csms.WithSessionRegistry`) must share its *global* table
  (`PutGlobal` / `LookupGlobal` / `DeleteGlobal`) across instances; the *local*
  table is per instance. `memory.NewInProcessHub()` gives registries and routers
  that do this inside one process (tests, dev). Production deployments use a
  shared-store registry together with a router such as `addons/router-redis`,
  `router-nats` or `router-temporal`; the router resolves the owner through the
  same registry.
* Clustering is on when `WithInstanceID` is set. Without it nothing is registered
  globally and no remote calls are served (the previous behaviour).
* `Server.Shutdown` / `Server.Close` stop serving remote calls.

## Calling a charge point from any instance

`Server.Get(id)` still returns only connections held by this instance. To address
a charge point wherever it is, use `Server.Lookup`:

```go
conn, err := srv.Lookup(ctx, "CP_1")      // ocppj.ErrNotConnected if no instance holds it
if err != nil { ... }
resp, err := csms.Call(ctx, conn, profiles.Reset, req)   // also CallRaw / CallAsync
```

`Lookup` returns the live `*Conn` for a local charge point; otherwise a handle
whose calls are forwarded through the router (`conn.Remote()` is true). A remote
handle supports `Call`, `CallRaw`, `CallAsync` and `CallRawAsync`; `Close` and
`Send` return `csms.ErrRemoteConn`, and connection details (`Version`,
`RemoteAddr`, ...) are empty.

## Guarantees

* **Registration (sessions)**: the instance claims the charge point in a
  `storage.SessionRegistry` *before* it accepts the WebSocket upgrade (after
  auth, admission and a subprotocol pre-check). `Claim` is atomic: it assigns
  the next epoch for that charge point and records the instance, its boot id and
  the displaced owner. A failed upgrade releases the claim. Because the epoch is
  assigned by the registry, not by the writer's clock, a delayed write can no
  longer overwrite a newer connection.
* **Removal**: when the connection ends the instance calls `Release(cpID, epoch)`,
  a compare-and-delete on the epoch: a newer connection of the same id, on this
  or another instance, keeps its record.
* **Displacement and convergence**: every instance compares its sockets with the
  registry (`SessionConfig.ReconcileInterval`, default 30 s, batched
  `LookupMany`). A socket whose epoch is no longer current is closed with
  1008 (`ocppj.ErrConnReplaced`); a socket whose record is missing is
  re-asserted; current records are refreshed. This repairs lost writes, a registry
  restart and the residual race of a claim for an abandoned upgrade: the worst
  case is one spurious reconnect, not a permanently wrong route.
* **Liveness**: each instance keeps a lease in the registry (`Heartbeat`, TTL 15 s,
  every 5 s). A session is routable only while its instance's lease exists with
  the same boot id, so a crashed or restarted instance invalidates all of its
  sessions at once. Per-charge-point records carry a long TTL, used only for
  garbage collection.
* **Registry outage**: sockets already established keep working (inbound traffic
  does not touch the registry). A new upgrade waits `ClaimTimeout` (2 s) for the
  claim, then is accepted unclaimed (`Conn.Session()` has epoch 0) and claimed in
  the background; the reconcile loop repairs the rest.
* **Which registry**: `csms.WithSessionRegistry(reg)` uses a
  `storage.SessionRegistry` directly (`memory.NewSessionRegistry()` for one
  process, `addons/registry-redis` across processes). `WithInstanceID` with a
  `ConnectionRegistry` that implements `SessionRegistry` (the in-memory registries
  and `InProcessHub` do) gets the same guarantees. A `ConnectionRegistry` that only has
  `PutGlobal` / `LookupGlobal` / `DeleteGlobal` still works through an adapter
  with the old guarantees: last write wins (no compare-and-set), removal is an
  atomic compare-and-delete only if the registry implements
  `storage.OwnedGlobalDeleter` (else a lookup followed by a delete), no leases, no
  reconcile. The `PutGlobal` API is deprecated in favour of `SessionRegistry`.
* **Remote result**: the owner's typed response, `*ocppj.CallError` (code,
  description, details), and the ocppj sentinels (`ErrCallTimeout`,
  `ErrConnClosed`, `ErrNotConnected`, ...) come back with the same type, so
  `errors.As` / `errors.Is` work as for a local call. Other failures (e.g. a
  broker error) are returned wrapped with the action and charge point id.
* **Not found**: no route, a route to an instance that no longer holds the
  connection, or a router that cannot forward (the single-instance default) all
  give `ocppj.ErrNotConnected`.
* **Timeouts**: the owner applies its own call timeout; the caller's `ctx` bounds
  the wait for the router. Give `ctx` a deadline.

## Not guaranteed

* With a `SessionRegistry`, a crashed instance's routes stop resolving when its
  lease lapses (at most `LeaseTTL`, 15 s by default); until then calls to them
  fail with a router error or timeout. With the legacy `PutGlobal` adapter the
  route can outlive the instance until the charge point reconnects or the
  registry expires the entry.
* There is no ordering between calls issued from different instances, and a
  forwarded call has no outbound queue on the caller (call serialization
  applies on the owning instance).
* Inbound messages (BootNotification, ...) are handled on the instance holding the
  socket only; they are not forwarded.
* Remote serving starts when the server is created; calls forwarded during the
  first moments may fail with `ErrNotConnected`.

## Moving to the gateway link

`storage.MessageRouter` and `csms.WithMessageRouter` are deprecated in favour of
`gateway.Downstream`, the contract every CSMS-initiated call travels over in a
split deployment (`Call`, `Send` and `Control`, with session epochs, `Sent`
semantics, deadlines and typed errors; `docs/gateway.md`). Nothing breaks: the
router, its three addons and the `WithInstanceID` cluster mode keep working
exactly as documented above, and they are removed no earlier than v0.4. New
deployments should use `csms.NewGatewayServer` for the connection tier and
`csms.WithGatewayLink` for the processing tier over `addons/link-grpc` or
`addons/link-nats`; the registry (`WithSessionRegistry`) is shared as before.

| Today | Replacement |
|---|---|
| `WithInstanceID` + `WithConnectionRegistry` + `WithMessageRouter(router-*)` | gateway tier + processing tier over a link, `WithSessionRegistry` |
| `Server.Lookup(id)` then `Call` (forwarded by the router) | the same call; the link carries it as `Downstream.Call` to the gateway holding the session |
| `router.CallRemote(cpID, action, payload)` in your own code | `Downstream.Call(ctx, CallRequest{Target: SessionRef{CPID: cpID}, ...})` |
| `router-redis` / `router-nats` / `router-temporal` | `addons/link-grpc` (default) or `addons/link-nats` |

To migrate in steps, two adapters in `csms` speak the router's call envelope:

- `csms.RouterDownstream(router)` presents an existing router as a
  `gateway.Downstream` (calls only; `Send` and `Control` return
  `gateway.ErrUnsupported`; the target's epoch is ignored). A service written
  against `Downstream` can run on today's cluster and move to a link later.
- `csms.DownstreamRouter(downstream)` presents a `Downstream` as the router of a
  clustered server, so calls for chargers held elsewhere travel as
  `Downstream.Call` requests while everything else stays as it is.

What does not carry over is deliberate: a `Downstream` call names a session
epoch and reports `Sent`, which a router never could.
