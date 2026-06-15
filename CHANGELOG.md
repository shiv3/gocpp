# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/) and this project adheres to
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://github.com/shiv3/gocpp/commits/master
