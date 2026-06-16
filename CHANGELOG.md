# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/) and this project adheres to
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Callback-style async calls: `csms.CallAsync` / `cp.CallAsync` (plus untyped
  `CallRawAsync`) send a request without blocking and deliver the typed response (or
  error) to a callback. With `WithSerializedCalls` they're queued on a per-connection
  FIFO and sent one outstanding at a time (callback in submission order); otherwise
  they run concurrently. Queue bound via `WithAsyncQueueSize` (default 64;
  `ocppj.ErrQueueFull` when full). Eases migration from ocpp-go's `SendRequestAsync`.

## [0.1.2] - 2026-06-16

All additions are opt-in and preserve prior default behavior.

### Added
- WebSocket ping/pong keepalive on both endpoints (`WithWebSocketPingInterval` +
  `WithWebSocketPongWait`); a missed pong tears down dead peers.
- Charge-point auto-Heartbeat via `cp.WithHeartbeatInterval`.
- Charge-point client authentication and transport options: `cp.WithBasicAuth`,
  `cp.WithHTTPHeader`, `cp.WithTLSConfig` (OCPP Security Profiles 1/2/3).
- `WithSerializedCalls()` (CSMS and CP): cap outbound CALLs to one outstanding per
  connection.
- `cp.WithOfflineQueue(capacity)`: bounded FIFO queue for CP-originated calls while
  disconnected, flushed FIFO on reconnect (`ocppj.ErrQueueFull` at capacity). In-flight
  CALLs fail with `ocppj.ErrConnClosed` on disconnect by default; `cp.WithRetryInFlightCalls()`
  opts into resend after reconnect.
- Connection lifecycle callbacks: `cp.WithOnConnect`/`WithOnDisconnect`/`WithOnReconnect`
  and `csms.WithOnConnect`/`WithOnDisconnect`.
- Schema validation extended to outbound requests, inbound responses, and handler
  responses (previously inbound requests only).

### Changed
- **BREAKING:** remove `csms.WithHeartbeatInterval` — in OCPP the Charge Point emits
  Heartbeat, never the CSMS. Use `cp.WithHeartbeatInterval`.

## [0.1.1] - 2026-06-16

### Changed
- **BREAKING:** rename the OCPP error-code constant `ErrorCodeRpcFrameworkError` to
  `ErrorCodeRPCFrameworkError` (Go naming; the on-the-wire value `"RpcFrameworkError"`
  is unchanged).
- Adopt golangci-lint v2 (built with the current Go toolchain so it lints a `go 1.26`
  module) and resolve all findings.

## [0.1.0] - 2026-06-16

### Added
- OCPP 1.6 (incl. Security Extensions), 2.0.1, and 2.1 message support from a single
  module — generated from the official OCA JSON schemas.
- Generics-first CSMS and Charge Point APIs (`csms.On`/`csms.Call`, `cp.On`/`cp.Call`)
  with compile-time direction enforcement.
- Context-driven dispatcher: one writer goroutine, context-bound lifecycle, panic-safe
  handlers, race- and goroutine-leak-tested.
- JSON-Schema-driven codegen with embedded runtime validation and a merge-patch override
  mechanism; deterministic output.
- Two-layer validation: JSON Schema (strict mode) + generated `validate` struct tags.
- Pluggable Storage (ConnectionRegistry, MessageRouter, TransactionStore, ConfigStore),
  Auth (None/BasicAuth/mTLS), and Observability (slog, OpenTelemetry, Prometheus).
- Subprotocol negotiation for multi-version CSMS; `Conn.Subprotocol()` /
  `Client.NegotiatedProtocol()`.
- Tooling: `gocpp-validate` (schema validation CLI) and `gocpp-sim` (scenario-driven
  charge-point simulator).
- 2.0.1→2.1 message-set diff/changelog generator.
- Conformance test suite (validation tables + direction) across all 188 messages of
  1.6/2.0.1/2.1, ported from lorenzodonini/ocpp-go and extended for 2.1.
- Benchmarks (codec/RTT/concurrent/memory), build-tagged soak test, and the
  `core/codec` JSON seam.
- Tolerant schema-validation mode (`csms.WithTolerantSchema()` / `cp.WithTolerantSchema()`):
  log a warning and process the message instead of rejecting, for vendor quirks (OQ-19).
- `csms.WithCPIDExtractor` for dynamic WebSocket path routing such as `/{org}/{cpId}` (#364).
- `csms.WithDuplicatePolicy` (`CloseExisting` default, or `RejectNew` → HTTP 409) (#376).
- Connection metadata accessors on `csms.Conn`: `RemoteAddr()`, `RequestHeader()`, `TLS()`,
  `Subprotocol()` (#315/#334/#343).
- `csms.CallRaw` for sending untyped CSMS→CP operations (symmetric with `cp.CallRaw`).
- OpenTelemetry metrics implementation (`core/observability/metrics/otel`).
- End-to-end OCPP 1.6 interop suite driving the gocpp CSMS against ocpp-cp-simulator
  (`examples/csms-full/interop/`).
- Addon modules under `addons/` (each a nested module so heavy deps stay out of the core
  tree): `router-redis` and `router-nats` (`storage.MessageRouter` for multi-instance CSMS),
  `router-temporal` (durable Temporal-backed routing, experimental), `statefsm` (OCPP 1.6
  connector state machine), and `tenant` (multi-tenant partitioning).

### Changed
- **BREAKING:** `auth.Authenticator.Authenticate` now takes the parsed charge point id —
  `Authenticate(r *http.Request, cpID string) (Identity, error)` (#352). Custom
  `Authenticator` implementations must add the `cpID` parameter. For `BasicAuth`, the
  `VerifyBasic` verifier now receives the path-parsed `cpID` as its first argument (was the
  HTTP Basic username); the username is preserved as `Identity.Credential`.
- `WithStrictSchema(false)` now explicitly selects "off" (no validation); use
  `WithTolerantSchema()` for the warn-and-pass behavior.

### Fixed
- csms data race: a connection was discoverable via `Get` before its context was
  initialized.
- Unrecovered handler panic crashed the process; handlers now reply `InternalError` and
  the connection survives.
- Call metrics (`gocpp_calls_total`, `gocpp_call_duration_seconds`) were never recorded —
  the dispatcher's `CallStarted`/`CallCompleted` hooks are now invoked on both the inbound
  handler and outbound `DoCall` paths.

### OCPP spec compatibility
- OCPP 1.6 (edition 2 + Security Whitepaper), 2.0.1, 2.1.

[0.1.1]: https://github.com/shiv3/gocpp/releases/tag/v0.1.1
[0.1.0]: https://github.com/shiv3/gocpp/releases/tag/v0.1.0
