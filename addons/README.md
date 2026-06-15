# gocpp addons

Optional, dependency-heavy extensions to gocpp. Each addon is a **separate nested Go
module** (its own `go.mod`) so that heavy dependencies (Redis, NATS, Temporal, an FSM
library) stay out of the core `github.com/shiv3/gocpp` module's dependency tree — you only
pull in what you import.

| Addon | Module path | Purpose | External dep |
|---|---|---|---|
| [router-redis](router-redis/) | `github.com/shiv3/gocpp/addons/router-redis` | `storage.MessageRouter` over Redis Pub/Sub for multi-instance CSMS call fan-out | `redis/go-redis/v9` |
| [router-nats](router-nats/) | `github.com/shiv3/gocpp/addons/router-nats` | `storage.MessageRouter` over NATS request/reply | `nats-io/nats.go` |
| [router-temporal](router-temporal/) | `github.com/shiv3/gocpp/addons/router-temporal` | Durable, retryable `storage.MessageRouter` backed by Temporal (experimental) | `go.temporal.io/sdk` |
| [statefsm](statefsm/) | `github.com/shiv3/gocpp/addons/statefsm` | OCPP 1.6 connector state machine helper over the pluggable stores | `looplab/fsm` |
| [tenant](tenant/) | `github.com/shiv3/gocpp/addons/tenant` | Multi-tenant partitioning of `ConnectionRegistry`/`TransactionStore`/`ConfigStore` | none |

## Installing

Each addon is versioned and fetched independently:

```sh
go get github.com/shiv3/gocpp/addons/router-redis
```

For local development against an unreleased core, the addon `go.mod` files use
`replace github.com/shiv3/gocpp => ../..`.

## Routers (`router-redis`, `router-nats`, `router-temporal`)

All three implement `storage.MessageRouter` (`core/storage/router.go`):
`CallRemote` forwards a CSMS→CP call to whichever CSMS instance holds that charge point's
connection (resolved via `ConnectionRegistry.LookupGlobal`); `ServeRemote` runs the
receiving side that invokes your `RemoteHandler`. Wire one in with
`csms.WithMessageRouter(...)` plus a shared `ConnectionRegistry`. Live-service tests are
build-tagged `integration` and require a running Redis/NATS/Temporal.

## `statefsm`

A `Connector` state machine modeling OCPP 1.6 §4.9 states
(Available/Preparing/Charging/SuspendedEV/SuspendedEVSE/Finishing/Reserved/Unavailable/
Faulted) with event-driven transitions and a pluggable `StateStore` for persistence.

## `tenant`

A `Resolver` derives a tenant id from the upgrade request, and tenant-scoped store wrappers
isolate state so one CSMS can serve multiple tenants (the same cpID under two tenants does
not collide). Pure Go, no external dependency.

See each addon's own README for the full API and examples.
