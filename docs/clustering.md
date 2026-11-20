# Running several CSMS instances

A charge point holds one WebSocket to one CSMS instance. With several instances
behind a load balancer, a command for a charge point (Reset, RemoteStartTransaction,
...) may be issued on an instance that does not hold its socket. gocpp forwards
such calls to the owning instance.

## Setup

```go
srv := csms.NewServer(
    csms.WithInstanceID("csms-2"),            // unique per running instance; enables clustering
    csms.WithConnectionRegistry(registry),    // shared route table (global part)
    csms.WithMessageRouter(router),           // forwards calls between instances
)
```

* The **registry** (`storage.ConnectionRegistry`) must share its *global* table
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

* **Registration**: on connect the instance writes `cpID -> instanceID` to the
  global table (after the local table). **Last registration wins**: when a charge
  point reconnects to another instance, that instance's entry replaces the old one.
* **Removal**: on disconnect an instance removes the route only if it still names
  that instance. A registry implementing `storage.OwnedGlobalDeleter` does this as
  one atomic compare-and-delete (the in-memory registries do). For other
  registries the server does a lookup followed by a delete, which is not atomic: a
  registration landing in between can be lost, so shared-store registries should
  implement the interface.
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

* A route can outlive a crashed instance (nothing runs its disconnect cleanup);
  calls to it fail with a router error or timeout until the charge point
  reconnects or the registry expires the entry. Use a registry with TTLs or
  heartbeats for crash recovery.
* There is no ordering between calls issued from different instances, and a
  forwarded call has no outbound queue on the caller (call serialization
  applies on the owning instance).
* Inbound messages (BootNotification, ...) are handled on the instance holding the
  socket only; they are not forwarded.
* Remote serving starts when the server is created; calls forwarded during the
  first moments may fail with `ErrNotConnected`.
