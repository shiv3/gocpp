# OCPP 1.6 Connector State FSM

`statefsm` is a thin helper over `github.com/looplab/fsm` for modeling OCPP 1.6 connector statuses in charge-point code. It does not modify the core `github.com/shiv3/gocpp` module.

## States

The package models the OCPP 1.6 `StatusNotification.status` values:

| State |
| --- |
| `Available` |
| `Preparing` |
| `Charging` |
| `SuspendedEV` |
| `SuspendedEVSE` |
| `Finishing` |
| `Reserved` |
| `Unavailable` |
| `Faulted` |

## Events

| Event | Legal source states | Destination state |
| --- | --- | --- |
| `PlugIn` | `Available`, `Reserved` | `Preparing` |
| `PlugIn` | `Preparing` | `Preparing` |
| `Authorize` | `Available`, `Reserved` | `Preparing` |
| `Authorize` | `Preparing` | `Preparing` |
| `StartTransaction` | `Preparing` | `Charging` |
| `SuspendEV` | `Charging`, `SuspendedEVSE` | `SuspendedEV` |
| `SuspendEVSE` | `Charging`, `SuspendedEV` | `SuspendedEVSE` |
| `Resume` | `SuspendedEV`, `SuspendedEVSE` | `Charging` |
| `StopTransaction` | `Charging`, `SuspendedEV`, `SuspendedEVSE` | `Finishing` |
| `Unplug` | `Preparing`, `Finishing` | `Available` |
| `Reserve` | `Available` | `Reserved` |
| `CancelReservation` | `Reserved` | `Available` |
| `Fault` | any non-`Faulted` state | `Faulted` |
| `ClearFault` | `Faulted` | `Available` |
| `ChangeAvailability` | `Available` | `Unavailable` |
| `ChangeAvailability` | `Unavailable` | `Available` |

## Public API

Create a connector helper with `New`:

```go
connector := statefsm.New("CP-1", 1)

if err := connector.PlugIn(); err != nil {
    // illegal transition
}
if err := connector.StartTransaction(statefsm.TransactionBegin{ID: "tx-1"}); err != nil {
    // illegal transition or transaction-store error
}

status := connector.State()
```

Use `WithStateStore` to persist connector status. `NewMemoryStateStore` is suitable for tests and embedded use:

```go
store := statefsm.NewMemoryStateStore()
connector := statefsm.New("CP-1", 1, statefsm.WithStateStore(store))
```

Use `WithTransactionStore` to record transaction begin/end calls through the core `storage.TransactionStore` interface:

```go
connector := statefsm.New("CP-1", 1, statefsm.WithTransactionStore(txStore))
```

When a transaction store is configured, `StartTransaction` and `StopTransaction` require a transaction ID either from the event payload or the active transaction recorded by the connector.
