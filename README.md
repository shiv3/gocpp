# gocpp

A generics-first **OCPP** (Open Charge Point Protocol) implementation in Go,
supporting **OCPP 1.6, 2.0.1, and 2.1** from a single module.

> Status: pre-v1, under active development. Public API may still change before v1.0.

## Why gocpp

A from-scratch alternative to `lorenzodonini/ocpp-go` with:

- **All three versions** (1.6 / 2.0.1 / 2.1) generated from the official OCA JSON
  schemas — no hand-written, drift-prone message structs.
- **Generics-first API** — typed `On[Req,Resp]` handlers and `Call[Req,Resp]`, so
  request/response types are inferred from a single message value.
- **Context-native concurrency** — every connection owns reader/writer/dispatch
  goroutines bound to a `context`; teardown flows from one `cancel(cause)`. Race- and
  goroutine-leak-tested (`-race` + `goleak`).
- **Two-layer validation** — JSON-Schema validation against the embedded official
  schemas (source of truth), plus generated `validate` struct tags.
- **Pluggable** Storage / Auth / Observability with in-memory / NoOp defaults and
  Prometheus + OpenTelemetry adapters.

## Install

```sh
go get github.com/shiv3/gocpp
```

Requires Go 1.25+.

## Quick start

### CSMS (central system)

```go
srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1", "ocpp2.0.1", "ocpp1.6"))

csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
    return v16msg.BootNotificationResponse{
        Status:      v16msg.RegistrationStatusAccepted,
        CurrentTime: time.Now(),
        Interval:    300,
    }, nil
})

log.Fatal(srv.ListenAndServe(":8080")) // ws://host:8080/ocpp/{cpId}
```

### Charge point (client)

```go
client := cp.NewClient("CP_1", "ws://localhost:8080/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"))
if err := client.Connect(ctx); err != nil { log.Fatal(err) }
defer client.Close()

resp, err := cp.Call(ctx, client, v16p.BootNotification, v16msg.BootNotificationRequest{
    ChargePointVendor: "Acme", ChargePointModel: "Model-X",
})
```

Runnable examples: [`examples/csms-minimal`](examples/csms-minimal),
[`examples/cp-minimal`](examples/cp-minimal).

## Packages

| Import | Purpose |
|---|---|
| `csms` | CSMS server: `NewServer`, `On`, `Call`, `Get` |
| `cp` | Charge point client: `NewClient`, `Connect`, `On`, `Call`, `Run` |
| `v16`, `v201`, `v21` | Per-version metadata + `RegisterSchemas` |
| `v16/messages`, `…/profiles` | Generated message structs + `ocppj.Message` profile vars |
| `core/ocppj` | OCPP-J framing (Call/CallResult/CallError), errors |
| `core/dispatcher` | Version-agnostic connection lifecycle + pending-call tracking |
| `core/schema` | JSON-Schema validator + registry |
| `core/auth` | `Authenticator`: `None`, `BasicAuth`, `MTLSFromClientCert` |
| `core/storage` (+ `/memory`) | `ConnectionRegistry`, `MessageRouter`, `TransactionStore`, `ConfigStore` |
| `core/observability` (+ `/metrics/{prom,otel}`) | `Metrics` (NoOp/Prometheus/OpenTelemetry), OTel tracer |

## Addons

Optional extensions live under [`addons/`](addons/), each a **separate nested module** so
their heavy dependencies stay out of the core dependency tree:

| Addon | Purpose |
|---|---|
| [`addons/router-redis`](addons/router-redis/) | `storage.MessageRouter` over Redis Pub/Sub (multi-instance CSMS) |
| [`addons/router-nats`](addons/router-nats/) | `storage.MessageRouter` over NATS request/reply |
| [`addons/router-temporal`](addons/router-temporal/) | Durable Temporal-backed `MessageRouter` (experimental) |
| [`addons/statefsm`](addons/statefsm/) | OCPP 1.6 connector state-machine helper |
| [`addons/tenant`](addons/tenant/) | Multi-tenant partitioning of the pluggable stores |

```sh
go get github.com/shiv3/gocpp/addons/router-redis
```

See [addons/README.md](addons/README.md) for details.

## Validation

Layer 1 (wire): enable strict schema validation on the connection so malformed
inbound messages are rejected with a `CallError` before reaching your handler:

```go
reg := schema.NewRegistry()
v201.RegisterSchemas(reg)
srv := csms.NewServer(
    csms.WithSubProtocols("ocpp2.0.1"),
    csms.WithSchemaRegistry(reg),
    csms.WithStrictSchema(true),
)
```

Schema handling is three-state. With a registry set:

- `WithStrictSchema(true)` — reject invalid messages with `FormationViolation`.
- `WithTolerantSchema()` — log a warning and process anyway (for real chargers that send
  undefined enums or extra fields).
- `WithStrictSchema(false)` (default) — validation off.

`cp.WithTolerantSchema()` / `cp.WithStrictSchema()` do the same on the charge-point side.

## Production features

```go
srv := csms.NewServer(
    csms.WithAuthenticator(auth.BasicAuth(verify)),     // Security Profile 1/2 (verify(cpID, password))
    csms.WithMetrics(otelmetrics.New(meterProvider)),   // OpenTelemetry metrics (or prom.New for Prometheus)
    csms.WithTracerProvider(tp),                        // OpenTelemetry spans
    csms.WithConnectionRegistry(myRegistry),            // pluggable
    csms.WithCPIDExtractor(extractCPID),                // dynamic path routing, e.g. /{org}/{cpId}
    csms.WithDuplicatePolicy(csms.DuplicatePolicyRejectNew), // or CloseExisting (default)
)
```

Handlers can read connection metadata from the `*csms.Conn`:
`c.RemoteAddr()`, `c.RequestHeader()`, `c.TLS()`, `c.Subprotocol()`. The authenticator
receives the parsed charge point id: `Authenticate(r *http.Request, cpID string)`. To send
an untyped CSMS→CP operation, use `csms.CallRaw(ctx, conn, action, payloadJSON)`.

## Tooling

```sh
go install github.com/shiv3/gocpp/cmd/gocpp-validate@latest
go install github.com/shiv3/gocpp/cmd/gocpp-sim@latest
```

- `gocpp-validate --version 2.0.1 --action BootNotification msg.json` — validate a
  message against the official schema.
- `gocpp-sim run -s scenario.yaml` — drive a simulated charge point through a YAML
  scenario against a CSMS.

## Testing

```sh
make test          # go test ./...
make test-race     # go test -race ./...
make codegen       # regenerate v16/v201/v21 from schemas/
```

The `internal/conformance` suite ports lorenzodonini/ocpp-go's per-message test cases
(validation tables + direction enforcement) across all 188 messages of 1.6/2.0.1/2.1.

## Documentation

- [docs/usage.md](docs/usage.md) — usage guide
- [docs/architecture.md](docs/architecture.md) — design & concurrency model

## License

MIT
