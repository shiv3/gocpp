# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/) and this project adheres to
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Staged releases, phase 7 chaos leftovers: real-process tests of CSMS calls issued
  from the canary and the stable pool during a canary, the orphan result when the
  calling canary backend dies (gRPC link), a `hold: busy` session during promotion
  and rollback, and a "no NotSent call lost" check in the gateway canary fill.
- Staged releases, phase 7 leftovers: the gateway keeps the pool each session is
  routed to in the registry (`Session.Meta["route.pool"]`, new optional
  `storage.RouteWriter`, implemented by the memory registry and `registry-redis`),
  the gateway exports `gocpp_gateway_late_replies_total{pool}` and
  `gocpp_gateway_orphan_results_total{pool,outcome}` (link-grpc `RoutingMetrics`),
  an orphan result goes to the pool of the calling backend (`CallRequest.CallerPool`),
  and `addons/rollout` has the optional `late_orphan` gates. link-grpc metrics now
  label the default pool `default` instead of an empty string.
- Staged releases, phase 7c (docs/rollout.md part 4): `addons/rollout` (own module),
  the optional, default-off automatic promotion controller: stage plan, gates
  (failed-outcome ratio, CALLERROR code shares, p99 latency, minimum samples, your
  own Prometheus checks), auto-rollback in one revision, yield on manual changes,
  dry-run mode, `PromSource` and `ScrapeSource` (the gateways' own `/metrics`),
  log and Kubernetes event notifiers; `gocppctl rollout validate|run`.
- Staged releases, phase 7b (docs/rollout.md): `gateway/admin` (per-gateway admin
  API: status, paced `shed` by count / min age / attributes, soft cap; client and
  rebalance planner), `gocppctl gateway status|shed|cap|rebalance`,
  `Server.SoftMaxConnections`, `gateway.AttrSource` + `csms.WithAttrSource` +
  `gateway/attrsource` (file and HTTP; keys arrive as `ext.*`), gateway flags
  `--admin-token`, `--advertise-admin`, `--attr-source-*`, `probe --interval` (a
  continuous probe sidecar with `gocpp_probe_*` metrics), manifests with the probe
  sidecar and the admin port, and real-process tests of a gateway canary
  (soft cap, fill, rebalance, kill -9 of the canary, rollback during a shed,
  registry outage during a shed).
- `BenchmarkGatewayIdleFootprint` and `TestGatewayIdleFootprintBounds` (16 KB heap+stack per idle gateway session); `TestIdleFootprintBounds` bounds tightened to 9 KB heap / 6 KB stack so a regression of a few hundred bytes per connection fails. docs/performance.md: "RSS versus live memory".
- Gateway options (phase 6): `csms.WithEdgeHeartbeat` / `WithEdgeResponder` /
  `WithOnEdgeResponse` (a gateway answers Heartbeat, or any configured action,
  itself without a backend hop; opt-in) and `csms.WithTenantLimits` (per-tenant
  session cap checked before the upgrade and a fairly shared per-tenant message
  rate budget; tenant from a request resolver, a cp id mapping or the session
  attributes). `dispatcher.Conn.SetInboundGate`, the optional metrics
  extensions `observability.TenantMetrics` and `EdgeMetrics` (Prometheus and
  OpenTelemetry), `gocpp-gateway --edge-heartbeat --tenant-*` flags, benchmark
  `BenchmarkEdgeAndTenant` and gate `TestGateEdgeAndTenantOverhead`.
- Weighted in-flight caps per tenant (`csms.TenantLimits.InFlightPool`,
  `TenantLimit.Weight` / `MaxInFlight`): the pool is split by weight among the
  tenants with sessions, a tenant is always admitted up to its share and may
  borrow only what other active tenants cannot claim, refusals answer with
  `RejectCode` and count as `kind="inflight"`. `dispatcher.Conn.SetInboundDone`,
  optional `observability.TenantInflightMetrics` (`gocpp_tenant_inflight`),
  `gocpp-gateway --tenant-inflight-pool --tenant-max-inflight --tenant-weights`.
- `storage.MessageRouter`, `csms.WithMessageRouter` and the router addons
  (`router-redis`, `router-nats`, `router-temporal`) are **deprecated** in favour
  of `gateway.Downstream` over a gateway link (design: docs/design/gateway.md).
  Nothing changes at runtime and nothing is removed before v0.4. Migration:
  `csms.RouterDownstream(router)` exposes an existing router as a
  `gateway.Downstream` and `csms.DownstreamRouter(downstream)` exposes a
  `Downstream` to `WithMessageRouter`; docs/clustering.md ("Moving to the gateway
  link") maps each old construct to its replacement. Linters (staticcheck SA1019)
  will flag the deprecated identifiers.
- Frame and session tap exporters (`core/observability/tap`): bounded, lossy,
  never blocking the hot path, with sampling and filters; `tap.JSONL` /
  `JSONLFile` / `Multi` sinks and `linknats.NewTapSink` (NATS subjects per charge
  point); `csms.WithSessionObserver` and `observability.SessionEvent` (opened /
  closed once, on embedded servers and gateways); `gocpp-gateway --tap-*` flags and
  `gocpp_tap_events_total`.
- Conformance and e2e suites in split mode over NATS (`scripts/split-nats-suite.sh`,
  CI job `gateway-modules`): `csms.WithSplitLink` / `csms.SplitLink` (the
  `WithSplitInProcess` topology over any link), `splitmode.Register`
  (`GOCPP_TEST_SPLIT=<name>`), and `addons/link-nats/splitharness`. The main
  module does not depend on `addons/link-nats`.
- Gateway correctness follow-ups: `Backend.Locate` for the gRPC link (`locate`
  capability, `LocateRequest`/`LocateReply` frames), `BackendInfo.Actions` filled
  from the registered handlers and honoured by `PoolSet` (unhandled actions fall
  back to the route's fallback pool and are counted), `csms.WithGatewayAuthCache`
  and `Server.InvalidateAuthCache`, and link protocol skew tests (v1/v2 ranges).
- Staged releases, phase 7a (docs/staged-releases.md): `gateway/routepolicy`
  (declarative routing policy: id / attribute / version / stable-hash percentage
  rules, action rules with a transaction-group lint, `on_unavailable`, `hold: busy`,
  suspend on fallback, error-rate guard; `Engine` implements `gateway.Router` and
  the new `gateway.RouteObserver`), `storage.PolicyStore` (CAS + watch) with memory and
  `addons/registry-redis` implementations, `gateway.PoolReject`, `PoolSet.SetObserver`,
  `linkgrpc.WithRouteObserver`, `inproc.WithRouteObserver`, `cmd/gocppctl`
  (`route validate|show|apply|rollback|status`), gateway flags `--route-policy-file`,
  lease labels `policy_rev` and `suspended`, and the routing metrics
  (`gocpp_gateway_route_*`).

- `addons/link-grpc`: write coalescing on the link stream (frames queued while a write is
  in progress go out together; as one `Batch` message when both ends advertise the new
  `batch` capability, `gateway.CapBatch`; no timer, a lone frame is written at once) and
  `WithStreams(n)` for N Attach streams per gateway. Link protocol stays v1: an older peer
  simply does not negotiate `batch`. Backend request goroutines reserve stack up front.
- Gateway performance gates and benchmarks (`benchmarks/gateway_hop_test.go`,
  `addons/link-grpc/hop_test.go`), pprof on the `gocpp-gateway` admin listener, and the
  measured results in docs/gateway.md.
- `addons/link-grpc` (own module): gRPC link between gateways and backends
  (protobuf contract `gocpp.link.v1`, backends dial gateways, Hello/HelloAck
  protocol and capability negotiation, Drain/DrainAck, Unsupported answers).
- `cmd/gocpp-gateway` (own module): reference gateway binary (`serve`, `probe`),
  Prometheus metrics, `/readyz`, soft session cap, SIGTERM drain; chaos tests with
  real processes; `examples/gke-gateway` manifests.
- Gateway mode (phase 3 of docs/design/gateway.md): `gateway` package (contract:
  `Envelope`, `Upstream`, `Downstream`, `WireError`, `Pool`), `gateway/inproc`
  (in-process link), `csms.NewGatewayServer` (connection tier),
  `csms.WithGatewayLink` + `Server.ServeGateway` / `Server.Upstream` (processing
  tier), `csms.WithGatewayID`, `csms.WithOnOrphanResult`, `Conn.GatewaySession`,
  `csms.WithSplitInProcess`. On a backend `Lookup` returns a session handle whose
  `Call`, `Send` and `Close` go through the link. See docs/gateway.md. The
  conformance and e2e suites run a second time through the in-process link
  (`GOCPP_TEST_SPLIT=1`).
- Staged-release seams: `gateway.BackendInfo`, `DefaultPool`, `PoolSet`,
  `Router` / `RouteInput` / `RouteDecision`, `Envelope.Route`,
  `ControlSetAttrs`, `csms.WithBackendInfo`, `csms.WithRouter`,
  `Conn.UpdateAttrs`. Without a router only the default backend pool gets work.
- `dispatcher.RawCallError`, `dispatcher.WithSentHook`, `HandlerRegistry` send
  fallback (`SetSendFallback`, `ResolveSend`), `ConnMetadata.Owner` /
  `Conn.Owner`.
- `dispatcher.Pipeline` (`NewPipeline`, `RunCall`, `RunSend`, `ValidateInbound`,
  `ValidateOutbound`, `TransformOutbound`, `DecodeSigned`, `MapBuiltinError`): the
  payload policy of a connection (signed-message unwrap, schema validation and
  lenient coercion, outbound transform, handler-error to CALLERROR mapping),
  extracted from `Conn` so it needs only a `Config`, a version and a charge point
  id. Embedded connections use it unchanged; it is the seam a processing tier
  behind a gateway uses to apply identical rules.
- `HandlerRegistry.SetFallback` / `Resolve`: a catch-all handler (receiving the
  action) for actions with no registered handler (`Lookup` is unchanged).
- `transport.StatusServiceRestart` (WebSocket close code 1012).
- `csms.On` / `csms.OnSend` also record the handler in a table typed on
  `*csms.Conn` (internal), used by the upcoming gateway mode.
- `storage.SessionRegistry` (Claim / CAS Release / Reassert with registry-assigned
  epochs, per-gateway lease, `LookupMany`), `memory.NewSessionRegistry`, and
  `csms.WithSessionRegistry` with `csms.SessionConfig`. The server claims a
  session before accepting the WebSocket upgrade, releases it by epoch when the
  connection ends, keeps a gateway lease alive and runs a reconcile loop that
  closes sockets superseded by a newer connection on another instance and
  re-asserts lost records. `csms.Conn.Session()` returns the record.
  `dispatcher.Conn.SetAttachment/Attachment`.
- `addons/registry-redis`: `storage.SessionRegistry` on Redis with Lua scripts.
- `csms.Server.Drain(ctx, DrainOptions)`: moves every charge point off a server
  without a reconnect storm. New upgrades get `503` with a jittered
  `Retry-After` (`Server.Draining`, `Server.ReadyHandler` for readiness), sessions
  are closed at a fixed rate (default sessions / (0.9 x window), window 5 min),
  idle first, with close code 1012 (service restart) and cause
  `ocppj.ErrGatewayDraining`; a busy session stops taking new calls (they fail
  unsent with `ErrGatewayDraining`) and is closed when idle or after its grace;
  at the end of the window the rest are closed with `ocppj.ErrDrainTimeout`.
  `dispatcher.Conn.TryDrain` / `Quiesce` / `ForceDrain` / `Draining` are the
  per-connection primitives.
- Session attributes and gateway labels (aligned with docs/design/gateway.md section 5):
  attributes live in `storage.Session.Meta` under the reserved prefix `attr.`
  (`Session.Attrs()`, `storage.MergeAttrs`; `route.` is reserved for gateway routing
  decisions), `SessionRegistry.SetAttrs` merges (empty value deletes) with a compare
  on the epoch, `Session.GatewayVersion`, `GatewayInfo.Labels` stored in the lease
  (`version`, `policy_rev`, ...; memory and Redis), `csms.SessionConfig.Version/Labels`,
  `Server.SetGatewayLabel`, `csms.WithSessionAttributes`, `Conn.Attrs/SetAttrs`
  (1 KiB cap, `ErrAttrsTooLarge`), `Server.SetSessionAttrs`.
- Targeted drain for rollouts: `DrainOptions.CPIDs/Attrs/Select/Filter/Max/Fraction/
  RefuseDuring` close only the selected sessions (idle first, paced, 1012) while the
  server keeps serving (lease stays `serving`, `ReadyHandler` not-ready meanwhile);
  `Server.DrainStatus` reports progress.
- `csms.WithSoftMaxConnections` / `Server.SetSoftMaxConnections`: `ReadyHandler`
  reports not-ready at or above the soft cap; upgrades and sessions are unaffected.
- `observability.SessionMetrics` (Prometheus and OTel): session duration, closes by
  code and cause, upgrade results.
- `csms.WithAcceptRateLimit(rate, burst)` (token bucket in front of the upgrade,
  `503` + jittered `Retry-After`), `csms.RateLimitListener` (shed connections at TCP
  accept, before TLS), `Server.AdmissionStats`.
- `transport.SharedTicketKeys` / `RotateSharedTicketKeys`: TLS session ticket keys
  derived from a shared secret so a charge point reconnecting to another pod
  resumes its TLS session.
- `observability.SchemaSoftViolationMetrics` (optional extension of `Metrics`).
  The Prometheus (`gocpp_schema_soft_violations_total{version,action,kind,keyword}`)
  and OpenTelemetry (`gocpp.schema_soft_violations`) adapters implement it, and the
  dispatcher's metrics bridge forwards it, so lenient-mode soft violations are now
  exported.

### Changed

- With `WithInstanceID`, the connection registry's global routes are now recorded
  as epoch-ordered sessions when the registry implements `storage.SessionRegistry`
  (the in-memory registries and `InProcessHub` do): a delayed write from an old
  connection can no longer overwrite a newer one, and a superseded socket is
  closed by the reconcile loop. Registries that only implement `ConnectionRegistry`
  keep working through an adapter with their previous guarantees.
- In a clustered server the route is claimed before the WebSocket upgrade is
  accepted (it was published right after), so a slow registry now delays the
  handshake; after `SessionConfig.ClaimTimeout` (2s) the connection is accepted
  unclaimed and claimed in the background.
- `Server.Lookup` reads `SessionRegistry.Lookup`, which honours gateway leases.

### Fixed

- Gateway pre-merge fixes: a `RoleCaller` backend sending `Drain` no longer crashes the
  gateway (nil pool member); a write error met while flushing another goroutine's
  frame on a link stream is no longer returned to the goroutine whose own frame was
  written (it caused a redispatch of a delivered CALL); link batches are capped at
  1 MiB; `registry-redis` never hands out an epoch at or below the surviving record's
  when the epoch counter expired or was evicted, and `Reassert` keeps the counter alive.

- Gateway mode: the processing tier runs the connect/disconnect hooks of one charge point's session events one at a time, so a replaced session's disconnect can no longer run after the new session's connect when `SessionClosed(old)` and `SessionOpened(new)` arrive concurrently. A session event still waiting for a backend when the gateway server closes now gives up after the closing bound instead of keeping a goroutine for the pool's hold time.
- The `ConnectionCount` gauge (`gocpp_connections`) drifted negative when a connection
  was closed twice (a replaced connection is closed by the replacement and by its own
  teardown); the close is now counted once.
- `gocpp_outbound_queue_depth` / `gocpp.outbound_queue.depth` help text and the
  `dispatcher.Conn` doc comment described the removed writer-goroutine queue. The
  metric samples senders waiting for or holding the per-connection write lock
  (1 = uncontended write).

### Docs

- The library default schema mode is off; lenient is opt-in (docs/performance.md
  called it the default; it is only the load-test harness default). README and
  docs/usage.md now say so explicitly.
- WebSocket keepalive is on by default (ping 54s, pong wait 60s, read idle 60s);
  the usage and performance tables said it was disabled.
- `csms.WithMaxConnections` exists (default unlimited); docs/performance.md claimed
  connections cannot be limited.
- `CallTimeout` starts once the call owns the connection's call slot, right before
  the CALL is written, not when `Call` is invoked (the 0.2.0 wording "when the CALL
  is written" was accurate in effect; usage.md is now precise).
- `WithTransactionStore` / `WithConfigStore` are application-facing extension points
  that csms itself does not use.
- README: Go 1.26+ (matches go.mod); 194 messages across 1.6/2.0.1/2.1 (39 + 64 + 91).
- `WithMaxMessageSize` overflow closes with 1009 (covered by `TestMaxMessageSize`).

## [0.2.0] - 2026-11-21

This release collects everything since v0.1.3 (v0.1.4–v0.1.7 were tagged
without changelog sections). It contains breaking changes; pre-v1 minor
versions may break APIs. Read the upgrade notes first.

### Upgrade notes

- **Generated types:** every JSON-Schema `integer` is `int64` (was `int32`);
  JSON-Schema `number` fields are `types.Decimal` (encoded as JSON numbers);
  untyped 2.0.1/2.1 properties such as `DataTransfer.data` are
  `json.RawMessage`; `CustomDataType` keeps unknown properties in `Extra`.
  `datatransfer.Marshal`/`Unmarshal` work on `json.RawMessage`; use
  `MarshalString`/`UnmarshalString` for 1.6.
- **Outbound CALLs are serialized per connection by default** (OCPP-J
  synchronicity). `WithConcurrentCalls()` restores the old, non-compliant
  behaviour.
- **Handler contexts are not cancelled on disconnect**; they carry a per-handler
  deadline (`WithHandlerTimeout`, default 60 s). Use `csms.ConnFromContext(ctx)` /
  `cp.ConnDone(ctx)` to observe a disconnect.
- **Connection model:** one resident goroutine per connection; there is no writer
  goroutine or outbound queue any more, so `WithOutboundQueueSize` is ignored
  (deprecated). Writers take a per-connection write lock in FIFO order.
- **Error codes:** schema/decode rejections use the specific OCPP codes
  (Occurrence/Property/TypeConstraintViolation); unknown message types get
  `MessageTypeNotSupported` on 2.0.1/2.1; types 5/6 are only valid on 2.1.
  `WithOCPPGoCompat()` reproduces ocpp-go's charger-visible behaviour for staged
  migrations (see docs/migration-from-ocpp-go.md).
- **Parsed frames** reference the read buffer (`Frame.Payload`/`ErrData` are
  sub-slices); `NewMsgID` values minted in the same millisecond are unique but not
  ordered.
- **Clustering** is wired: with `WithInstanceID` + a message router, calls reach
  charge points connected to other instances (`Server.Lookup`).

Performance and sizing guidance: docs/performance.md.


### Added
- `docs/performance.md`: runtime guidance (GOMAXPROCS, GOMEMLIMIT, GOGC with measured tradeoffs), schema-mode and handler-pool cost, current per-connection footprint. `docs/architecture.md` and the README describe the one-reader-goroutine connection model.
- `dispatcher.HandlerPool` / `NewHandlerPool(n)` with `Config.HandlerPool`, and
  `csms.WithHandlerPool` / `cp.WithHandlerPool` (off by default): inbound handlers
  run on resident workers with warm stacks when one is idle, and on a new goroutine
  otherwise. It is a non-blocking hand-off, never a queue, so a slow handler cannot
  delay another connection. It removes the stack growth (`runtime.copystack`) of a
  fresh goroutine per message; see docs/performance.md for when it pays.
- `csms.WithOutboundValidation(bool)` / `cp.WithOutboundValidation(bool)` (default
  true): with false, payloads this side produces (handler replies, outbound Calls and
  Sends) skip schema validation and lenient repair; inbound messages are still
  validated as configured. Backed by `dispatcher.Config.SkipOutboundValidation`.
- `BenchmarkDispatchRoundTrip` (benchmarks/, in-memory transport, schema off/lenient/strict)
  and `BenchmarkValidateLenient` / `BenchmarkValidateStrict` (core/schema); baselines in
  docs/performance.md, "Per-message baselines".
- `InboundQueueSize` in `dispatcher.Config` plus `WithInboundQueueSize` and
  `WithOutboundQueueSize` (csms and cp): the reader-to-dispatcher queue is now sized
  independently of the writer queue.
- Footprint benchmarks and guard (`benchmarks/footprint_test.go`):
  `BenchmarkIdleFootprint` (2,000 idle connections, heap/stack/goroutines per
  connection, idle and after one Heartbeat), `BenchmarkHeartbeatMessage[Parallel]`
  (per-message ns/op and allocs/op), and `TestIdleFootprintBounds` asserting upper
  bounds. See docs/performance.md, "Measuring footprint".
- `csms.WithOCPPGoCompat()` / `cp.WithOCPPGoCompat()`: one preset that reproduces
  ocpp-go's charger-visible behaviour for staged migrations (lenient schema,
  ocpp-go error codes, silent drop of bad message ids, UTC whole-second times, no
  server pings with a 60 s idle close). Shorthand for the granular options below;
  later options win. See docs/migration-from-ocpp-go.md for the full table of
  gocpp default vs compat vs ocpp-go.
- `WithErrorCodeMapper(dispatcher.ErrorCodeMapper)` (csms and cp): change the
  CALLERROR code, or suppress the reply, for errors gocpp generates itself;
  `dispatcher.OCPPGoErrorCodeMapper()` mirrors ocpp-go. `ocppj.ProtocolError`
  gains `Elements` and `BadField`; `dispatcher.NewDecodeError` builds the
  decode-failure error typed handlers return.
- `WithMessageIDPolicy` (csms and cp): `MessageIDAuto`, `Accept`, `Reject`, `Drop`
  for an inbound CALL with an empty or over-36-character message id.
- `WithTimeEncoding(schema.TimeEncoding{UTC, Precision})` (csms and cp): control
  how every outbound date-time is written (UTC / fixed fractional digits), guided
  by the schema registry; works on 1.6, 2.0.1 and 2.1. Default unchanged.
- Lenient mode now reads a `null` payload as `{}` (`[2,"id","Heartbeat",null]`)
  and coerces a non-string 1.6 `DataTransfer.data` (object, array, number,
  boolean) to its compact JSON text, as ocpp-go accepts both.
- `csms.WithMaxConnections(n)`: cap simultaneous connections; over the cap the
  upgrade is refused with HTTP 503 + `Retry-After` before the handshake. The
  check-and-reserve is atomic and a same-id reconnect (replacement) is never refused.
- `WithMaxMessageSize(n)` for csms and cp: configurable inbound message size limit
  (default 1 MiB, as before); `transport.NewCoderWSWithLimit`.
- `csms.Conn.Stats()` / `LastActivity()` / `Done()` (`ConnectedAt`, `LastReceivedAt`
  including ping/pong, `LastSentAt`, `PendingCalls`, `QueuedCalls`),
  `csms.Server.Connections()`, `csms.ConnFromContext(ctx)` and `cp.ConnDone(ctx)`.
- `WithHandlerTimeout(d)` for csms and cp (see Changed).
- `WithConcurrentCalls()` for csms and cp: explicit, non-compliant opt-out of call
  serialization.
- Frame observer: `csms.WithFrameObserver` / `cp.WithFrameObserver`
  (`dispatcher.Config.FrameObserver`) receive an `observability.FrameEvent`
  (cp id, version, direction, raw bytes, type, message id, action, time, write
  error) for every inbound frame (including unparsable ones) and every outbound
  frame (after the write attempt; includes signed messages, SEND, CALLERROR and
  CALLRESULTERROR). It runs inline and must be fast; panics are recovered.
  `observability.SlogFrameObserver(logger, level, maxRaw)` is a ready-made logger.
- Message ids: inbound handlers read the frame's id with
  `ocppj.MessageIDFromContext(ctx)`; an outbound `Call`/`CallAsync`/`Send` reports
  the id it was sent with through `ocppj.WithMessageIDCallback(ctx, fn)`, with no
  change to the typed call signatures.
- Optional `observability.ConnectionMetrics` (outbound queue depth, WebSocket
  ping round-trip latency), implemented by the prom (`gocpp_outbound_queue_depth`,
  `gocpp_ping_latency_seconds`) and otel adapters. Existing `Metrics`
  implementations keep working unchanged.
- `cp.WithReconnectBackoff(initial, max, maxElapsed)`: tune the reconnect backoff
  used by `Client.Run`.
- `csms.WithConnConfig` / `csms.ConnTimeouts`: per-charge-point override of call
  timeout, write timeout, ping interval, pong wait and read-idle timeout, evaluated
  at upgrade. Defaults are unchanged.
- Multi-instance routing in `csms`: with `WithInstanceID`, the server registers
  each charge point globally (`PutGlobal`) on connect and removes the route on
  disconnect only if it still names this instance, serves forwarded calls through
  `WithMessageRouter` (`ServeRemote`, stopped by `Shutdown` / `Close`), and the new
  `Server.Lookup(ctx, id)` returns a `*Conn` for a charge point held by another
  instance, so `Call` / `CallRaw` / `CallAsync` are forwarded (`CallRemote`) and
  return the remote typed result, `CallError` and ocppj sentinels transparently.
  New `Conn.Remote()` and `csms.ErrRemoteConn`. See docs/clustering.md.
- `storage.OwnedGlobalDeleter`: optional registry interface for an atomic
  compare-and-delete of a global route (`DeleteGlobalIf`). Registries without it
  get a non-atomic lookup-then-delete.
- `memory.InProcessHub.Registry()`: per-instance registries sharing one global
  route table, for in-process multi-instance tests.
- Lenient schema mode now repairs common charger drift before the typed decode, for
  inbound requests and responses (lorenzodonini/ocpp-go#34, #190, #237, #240, #407):
  near-RFC3339 `date-time` strings (zone-less, space separator, lowercase `t`/`z`,
  `+0900` offsets, any fraction length) are rewritten to RFC3339, and wrong-typed
  primitives (`"1"` for integers, `"32.5"` for numbers, `"true"` for booleans, numbers
  for strings) are converted when lossless. Rewrites are reported as soft violations
  with keywords `format` and `type`. New `csms.WithLenientTimeLocation` /
  `cp.WithLenientTimeLocation` (zone-less default: UTC) and
  `schema.Validator.ValidateLenientWith` / `schema.LenientOptions`. Strict, tolerant
  and off modes are unchanged.
- OCPP 2.x `CustomDataType` keeps unknown properties: the new `Extra
  map[string]json.RawMessage` field round-trips everything beyond `vendorId`
  (the schema leaves it open for vendor extensions). Backed by
  `types.MarshalWithExtra` / `types.UnmarshalWithExtra` in `core/types`. A defined
  field wins over an `Extra` key of the same name on encode.
- `datatransfer.MarshalString` / `datatransfer.UnmarshalString`: the former
  `Marshal` / `Unmarshal` behaviour for the OCPP 1.6 string `data`.
- `csms.Conn.Close(ctx)` and `csms.Server.Disconnect(ctx, id)`: close one charge
  point's connection from the server side (WebSocket close 1000, reason "closed by
  the server"), waiting for teardown up to ctx. The disconnect callback fires once
  with the new cause `ocppj.ErrConnClosedByServer`; pending calls fail with it.
- `schema.ViolationError` (carries the failing JSON-Schema keyword) and
  `dispatcher.SchemaErrorCode` / `dispatcher.DecodeErrorCode`.
- OCPP 2.0.1 / 2.1: a frame with an unknown MessageTypeId and a readable message
  id is answered with CALLERROR `MessageTypeNotSupported` (Part 4 §4.2.3).
  OCPP 1.6J keeps ignoring such frames (the code is not in its error table).
- `csms.Server.Shutdown(ctx)`: gracefully closes live charge-point connections and
  waits for them to drain, up to the ctx deadline (force-closes on timeout).
  `Close()` remains the immediate teardown.
- `csms.WithCheckOrigin(func(*http.Request) bool)`: custom WebSocket origin
  predicate for logic that `WithOriginPatterns` can't express.
- `core/datatransfer` package: generic `Marshal[T]` / `Unmarshal[T]` helpers for
  the free-form `DataTransfer` `Data` payload.
- Generated typed handler interfaces and one-call registrars per version
  (`<version>/handlers`): `CPHandler` / `CSMSHandler` interfaces, embeddable
  `UnimplementedCPHandler` / `UnimplementedCSMSHandler` defaults (return a
  `NotSupported` CallError), and `RegisterCP(*cp.Client, CPHandler)` /
  `RegisterCSMS(*csms.Server, CSMSHandler)`. Replaces the per-message `cp.On` /
  `csms.On` boilerplate with a single call; embed the Unimplemented type to
  implement only the messages you need (gRPC-style). Compile-time type safe.
- Generated typed send helpers per version (`<version>/calls`): `CP<Action>(ctx,
  *cp.Client, req)` for charge-point-sendable messages and `CSMS<Action>(ctx,
  *csms.Conn, req)` for CSMS-sendable messages (DataTransfer gets both). Thin,
  direction-correct, discoverable wrappers over `cp.Call` / `csms.Call`.
- WebSocket origin verification controls for the CSMS: `WithOriginPatterns(...)`
  (cross-origin allowlist) and `WithInsecureSkipVerifyOrigin()` (disable the
  origin check entirely). These map to coder/websocket's `AcceptOptions`. The
  default is unchanged (same-origin verification; requests without an Origin
  header — i.e. non-browser charge points — are always allowed).

### Fixed
- Test hygiene: `TestTimeEncoding_ClientOutbound` closes its client, so `go test -race -count=N ./csms` no longer trips the goroutine-leak check in `TestServerLifecycle*` on later passes.
- Message types 5 (CALLRESULTERROR) and 6 (SEND) are OCPP 2.1 only. On a 1.6 or
  2.0.1 connection they are now treated as unknown message types (2.0.1: CALLERROR
  `MessageTypeNotSupported`; 1.6: ignored, answered under the ocpp-go compat preset)
  instead of resolving a pending call or reaching a SEND handler.
- Flaky `TestCluster_StaleDisconnectKeepsNewerOwner`: the test helper accepted the previous owner's route as proof that the new instance had published. Test-only; docs/clustering.md now states the cross-instance publish-ordering guarantee precisely.
- `csms.CallAsync` / `CallRawAsync` on a local connection called themselves instead of
  the dispatcher (unbounded recursion); they now dispatch the call.
- `cp.Client.Run` no longer stops reconnecting after 15 minutes: the backoff
  library's default `MaxElapsedTime` made `Run` return once a long-lived charge
  point had been up that long and then dropped. It now retries until ctx is
  cancelled (unless `WithReconnectBackoff` sets a finite `maxElapsed`), and the
  schedule restarts from the initial interval after each successful connection.
- **Breaking type change:** JSON Schema `number` fields (for example v1.6
  `ChargingSchedulePeriod.Limit` / `MinChargingRate`, and the v2.0.1/v2.1 `value`,
  `totalCost`, `minChargingRate` fields) are now `types.Decimal`
  (`github.com/shiv3/gocpp/core/types`) instead of `decimal.Decimal`. The old type
  was marshalled as a quoted JSON string unless the application flipped the
  process-global `decimal.MarshalJSONWithoutQuotes`, so gocpp sent numbers as strings
  and its own strict outgoing schema validation rejected them. `types.Decimal` always
  encodes as an unquoted JSON number and decodes both numbers and quoted numeric
  strings; the global is no longer consulted. Migration: replace
  `decimal.NewFromFloat/NewFromInt/NewFromString(...)` with the same-named functions in
  `core/types`, or wrap an existing value with `types.New(d)`; the shopspring methods
  remain available through the embedded value and `d.Shopspring()` returns it.
- `csms.Server` refuses new upgrades with HTTP 503 once `Shutdown` has begun or
  `Close` was called, instead of accepting and immediately closing.
- `csms`: inbound CALL/SEND handling is held until the connection is registered
  (`Get(id)` works) and `OnConnect` has returned; previously a handler could run
  first (flaky `Get` right after the first reply).
- `CallError.WireCode` now spells FormationViolation per version: `FormationViolation`
  on 1.6, `FormatViolation` on 2.0.1/2.1. Handler decode/schema rejections on 2.x
  connections previously used the 1.6 name.
- Generated `validate` struct tags no longer emit `required` on boolean fields.
  go-playground's `required` rejects the zero value, which would have flagged a
  legitimate `false` (e.g. `ConfigurationKey.readonly: false`) for anyone running
  struct-tag validation. (gocpp's own validation is JSON-Schema based and was
  unaffected.)
- Bidirectional `DataTransfer` support. OCPP 1.6/2.0.1/2.1 allow `DataTransfer`
  to be initiated by either peer; a new `ocppj.SentByBoth` direction marks such
  messages, and `CheckDirection` accepts it for any role/op. The generated
  `DataTransfer` profile descriptor (`v16`/`v201`/`v21`) is now `SentByBoth`, so a
  single descriptor works for all of `csms.On`, `csms.Call`, `cp.On`, and
  `cp.Call`. Previously the CSMS-initiated direction (`csms.Call` / `cp.On`)
  returned `ocppj.ErrInvalidDirection`. Backward compatible: existing
  charge-point-initiated usage is unaffected.
- `WithLenientSchema()` (CSMS and CP): a fourth schema-validation mode between
  strict and tolerant. Structurally broken messages (wrong type, missing required)
  are still rejected, but benign non-compliance is logged and passed: unknown/extra
  fields, out-of-bound values (numeric/length/array), and enum case mismatches — the
  last normalized to the canonical value so typed handlers keep matching. Per-keyword
  soft-violation metrics (`SchemaSoftViolation`) surface what real fleets send, for
  data-driven tuning. Avoids dropping transactions when stations send slightly
  non-compliant messages that ocpp-go's looser unmarshal would have accepted.
- A replaced connection (`DuplicatePolicyCloseExisting`) now reports
  `onDisconnect(old, ocppj.ErrConnReplaced)` exactly once and before
  `onConnect(new)`; previously the disconnect arrived after the new connect with
  the generic `ErrConnClosed` cause. The replaced peer gets close code 1008.
- Locally initiated teardown (`Conn.Close`, `Server.Shutdown`, replacement, idle
  watchdog) now sends a WebSocket close frame (1000, or 1008 when replaced)
  instead of dropping the TCP connection (peer saw 1006). `Server.Shutdown`
  closes connections in parallel.
- Panics in `DoCallAsync` callbacks and in the `WithOnConnect` /
  `WithOnDisconnect` hooks are recovered and logged instead of crashing the
  process.
- A CALL with a readable message id but a malformed shape is answered with a
  `FormationViolation` (1.6) / `FormatViolation` (2.x) CallError. CALLRESULT
  accepts extra trailing elements; CALLERROR / CALLRESULTERROR accept a missing
  details element, so pending calls resolve immediately instead of timing out.

### Changed
- The lenient schema path is cheaper: a payload that is `{}` (modulo whitespace) and
  valid for the schema returns without decoding or validating, other payloads are
  decoded by a small reflection-free decoder (strings and numbers share one copy of
  the payload, schema property names and enum values are reused), and the coercion
  walk no longer allocates per element or runs for schemas with nothing coercible.
  Accept/reject decisions, soft findings and normalised output are unchanged
  (differential tests against the previous implementation, plus fuzzing).
- `ocppj.Parse` decodes well-formed frames with a hand-written scanner (2 allocations
  for a CALL instead of 11) and falls back to the previous reflective decoder for
  anything unusual, so frames and errors are identical (`FuzzParseEquivalence`).
  `Frame.Payload` and `Frame.ErrData` are now sub-slices of the input buffer instead
  of copies: do not modify the buffer passed to `Parse` while the Frame is in use.
  The `Encode*` functions build the frame in one allocation.
- `ocppj.NewMsgID` no longer takes a process-wide mutex (math/rand/v2 instead of a
  crypto/rand monotonic source). Ids stay unique and time-sortable, but two ids from
  the same millisecond are no longer ordered by creation.
- The read idle watchdog and the WebSocket ping loop are `time.AfterFunc` timers
  instead of two goroutines per connection (8 -> 6 resident goroutines, ~2 KB less
  stack and ~1.8 KB less heap per connection), and inbound frames no longer
  signal a channel (two atomic stores). The idle deadline is measured on the
  monotonic clock. Behaviour is unchanged, except that the ping interval now
  counts from the end of the previous ping instead of a fixed ticker.
- Connection teardown is one `context.AfterFunc` callback instead of a closer
  goroutine plus a goroutine that waited for the others (6 -> 4 resident
  goroutines per server-side connection, ~6 KB less stack). Behaviour is unchanged:
  the same close frame per cause, `Close` still returns only after everything of the
  connection has stopped. `Conn.StartKeepalive` is now tracked like async calls
  (call it after `Start`; on a connection that is already closing it does nothing).
- `csms`: the HTTP handler that accepted a WebSocket upgrade returns right after
  the connection is registered, and the disconnect cleanup (disconnect callback,
  `Close`, registry removal) runs from a `context.AfterFunc`. A server-side
  connection holds 3 goroutines instead of 4 and no longer keeps the upgrade
  `*http.Request` alive (about 8 KB of stack and 3.5 KB of heap less per
  connection). Callback order is unchanged.
- No dispatcher goroutine per connection: the reader starts a handler goroutine
  itself when a slot is free, and parks further CALLs/SENDs in a bounded queue
  (`InboundQueueSize`, allocated only while frames wait) that finishing handlers
  drain. A server-side connection holds 2 goroutines (reader, writer) and about
  4 KB less stack and 0.8 KB less heap. Ordering and limits are unchanged
  (arrival-order starts, at most `MaxConcurrentHandlers` per connection,
  `HoldInbound` holds CALL/SEND only), and the reader still only stops when the
  queue is full. Differences: a handler waiting for `GlobalHandlerLimiter` now
  waits on its own goroutine (at most `MaxConcurrentHandlers` of them per
  connection) rather than on the dispatcher, and the queue now holds
  `InboundQueueSize` frames in total (it used to hold that many plus one in the
  dispatcher). `Conn` gains no public API; the reader body is split out as
  `handleMessage`, the entry point a push-style transport can drive.
- No writer goroutine per connection: whoever produced a frame (a handler, a
  caller of `DoCall`/`DoSend`, the reader for protocol-error replies) writes it
  under a per-connection write lock (a capacity-1 channel, FIFO, with
  context-aware waiting). A server-side connection now holds exactly one
  resident goroutine (the reader), about 2 KB less stack and 0.8 KB less heap, and a frame no
  longer crosses two goroutine handoffs. Behaviour changes to be aware of:
  1. A sender that already holds the write lock is not released by connection
     cancel until its write returns, which is bounded by `WriteTimeout` or by
     the transport closing during teardown (before, `send` returned as soon as the
     connection context ended). Senders still waiting for the lock leave
     immediately on their own context or the connection's.
  2. The `OutboundQueueDepth` metric now samples the number of senders waiting
     for or holding the write lock (1 = uncontended), not the length of a queue.
  3. `OutboundQueueSize`, `WithOutboundQueueSize` (csms, cp) have no effect any
     more and are deprecated; remove them.
  4. The frame observer's outbound calls run on the sending goroutine while it
     holds the write lock (still in wire order); an observer must not send on the
     same connection synchronously (it could not before either), and a slow
     observer delays that connection's writes only.
  Also: protocol-error replies from the reader are sent from at most 4 tracked
  background goroutines per connection and dropped beyond that (before: dropped
  when the 4-slot queue was full). `transport.WS` documents that `Ping` and
  `Close` may run concurrently with `Write`, that `Write` calls are serialized
  by the dispatcher, and that the slice returned by `Read` belongs to the caller.
- Dispatcher allocation diet (per message and per connection): the inbound
  handler context is built from one per-connection base plus the message id (and
  the `HandlerTimeout` deadline) instead of five nested contexts; the outbound
  call timeout is one `time.AfterFunc` instead of a context plus a context
  callback; the write timeout of a transport that implements the new optional
  `transport.Aborter` (the built-in coder transport and `FakeWS` do) is one lazily
  armed timer per connection instead of a context per write (other transports
  keep a per-write context); trace span attributes are set only for recording
  spans. Observable differences: a `DoCall` whose own context ends first now
  always returns that context's error (before, it could randomly return
  `ErrCallTimeout`), and a write that exceeds `WriteTimeout` aborts the socket
  without a close frame, as before. `transport.StatusAbnormalClosure` (1006) is
  added.
- Small per-connection savings (about 0.3 KB heap each): `dispatcher.NewConnShared`
  takes a `*Config` shared by many connections, and `csms` builds one config per
  negotiated version instead of copying it for every connection (a server with
  `WithConnConfig` still builds one per connection); the pending-call table keeps
  its first entry inline and allocates a map only for a second concurrent call.
- Socket buffers per connection shrink from net/http's 4 KB + 4 KB to 1 KB + 1 KB
  (about 6 KB less heap per connection, 14.0 -> 8.2 KB), via a `Hijack` wrapper
  around the ResponseWriter handed to the WebSocket library. New
  `csms.WithIOBufferSizes(read, write)` changes it (0 keeps net/http's 4096 for
  that side). Frames larger than the buffer bypass it; a frame over the write
  buffer size costs one extra write syscall. Bytes the client sends in the same
  segment as the handshake are preserved.
- Per-connection queue defaults shrink: `OutboundQueueSize` 64 -> 4 and the new
  `InboundQueueSize` defaults to 4 (the inbound queue used to share
  `OutboundQueueSize`). Idle heap per connection drops from ~34 KB to ~23 KB.
  The inbound queue only fills while all `MaxConcurrentHandlers` slots are busy,
  and then stops the reader (CALLRESULTs included); handlers that block for long
  on one connection may want `WithInboundQueueSize` raised. Migration: set the
  two options to 64 to restore the old depths.
- Strict schema mode now enforces the OCPP-J 36-character message id limit on
  inbound CALLs: an over-long id is answered with CALLERROR FormationViolation
  (FormatViolation on 2.x, id echoed) and an empty id is dropped. Lenient and
  tolerant modes accept both and log a warning. Restore the old behaviour with
  `WithMessageIDPolicy(dispatcher.MessageIDAccept)`.
- **Behaviour change: outbound CALLs are serialized by default** (OCPP 1.6J §4.1.1,
  2.0.1 Part 4 §4.1: no new CALL while one is outstanding), on csms and cp, all
  versions. Calls wait in one bounded per-connection FIFO shared by `Call` and
  `CallAsync` (`WithAsyncQueueSize`, default 64; beyond it `ocppj.ErrQueueFull`);
  `CallTimeout` starts when the CALL is written; a call cancelled while queued is
  never sent; replies and SEND bypass the queue; connections do not affect each
  other. Migration: code that relied on several in-flight Calls per charger now sees
  them sent one after another (latency adds up; a slow charger delays its own later
  calls, not other chargers'). `WithSerializedCalls()` is now the default and a
  no-op; `WithConcurrentCalls()` restores the old behaviour. `dispatcher.DefaultConfig()`
  now sets `SerializeOutboundCalls: true`.
- **Behaviour change: inbound handler contexts are no longer cancelled when the
  connection drops.** They keep the connection context's values but are cancelled only
  by a per-handler deadline (`WithHandlerTimeout`, default 60s, `0` = none). A handler
  that exceeds it and returns the context error is answered with `InternalError`; the
  CALLRESULT of a handler finishing after a disconnect is dropped silently. Migration:
  handlers that relied on `ctx.Done()` to notice a disconnect must select on
  `csms.ConnFromContext(ctx)` -> `Done()` (CSMS) or `cp.ConnDone(ctx)` (charge point).

- `memory.NewConnectionRegistry` now stores global routes (`PutGlobal` /
  `LookupGlobal` / `DeleteGlobal` were no-ops). Code relying on `LookupGlobal`
  always reporting not-found must not rely on it any more.
- **Breaking (generated types):** every OCPP JSON-Schema `integer` is now `int64`
  (was `int32`) in v16, v201 and v21, so vendor values such as a 1.6
  `transactionId` of 9223372036854775807 decode instead of overflowing
  (lorenzodonini/ocpp-go#188). Migration: change `int32` variables, pointers and
  slices that feed message fields to `int64` (`*int32` -> `*int64`, `[]int32` ->
  `[]int64`).
- **Breaking (OCPP 2.0.1 / 2.1):** `DataTransferRequest.Data` and
  `DataTransferResponse.Data` are `json.RawMessage` (was `*string`). The OCA
  schemas declare `data` without a type, so any JSON value is valid on the wire;
  a JSON object used to pass schema validation and then fail typed decoding
  (lorenzodonini/ocpp-go#257). Generally, any schema property with no `type` is
  now generated as `json.RawMessage`; DataTransfer `data` is the only one in the
  v201 and v21 schemas. OCPP 1.6 keeps `*string` (its schema says string).
  Migration: wrap the payload in `json.RawMessage` (or call `datatransfer.Marshal`);
  read it with `datatransfer.Unmarshal` or `json.Unmarshal`. A plain string must now be
  a JSON string literal (quotes included).
- **Breaking:** `datatransfer.Marshal` returns `json.RawMessage` and
  `datatransfer.Unmarshal` takes `json.RawMessage` (OCPP 2.x). For OCPP 1.6 use
  `MarshalString` / `UnmarshalString`.
- `CustomDataType` (2.x) now has custom `MarshalJSON` / `UnmarshalJSON` and an
  `Extra` field (lorenzodonini/ocpp-go#384); composite literals using field names
  are unaffected.
- Behaviour change: schema rejections of inbound CALLs are no longer always
  `FormationViolation` / `FormatViolation`. The first violated keyword selects the
  code: missing required fields / occurrence limits -> `OccurenceConstraintViolation`
  (1.6 wire spelling) / `OccurrenceConstraintViolation`; enum, length, range,
  pattern, format -> `PropertyConstraintViolation`; wrong JSON type of a property
  (also typed-decode failures in `csms.On` / `cp.On`) -> `TypeConstraintViolation`.
  A payload that is not an object stays `FormationViolation` / `FormatViolation`.

## [0.1.3] - 2026-06-16

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
- Behaviour change: schema rejections of inbound CALLs are no longer always
  `FormationViolation` / `FormatViolation`. The first violated keyword selects the
  code: missing required fields / occurrence limits -> `OccurenceConstraintViolation`
  (1.6 wire spelling) / `OccurrenceConstraintViolation`; enum, length, range,
  pattern, format -> `PropertyConstraintViolation`; wrong JSON type of a property
  (also typed-decode failures in `csms.On` / `cp.On`) -> `TypeConstraintViolation`.
  A payload that is not an object stays `FormationViolation` / `FormatViolation`.
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

[Unreleased]: https://github.com/shiv3/gocpp/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/shiv3/gocpp/releases/tag/v0.2.0
[0.1.3]: https://github.com/shiv3/gocpp/releases/tag/v0.1.3
[0.1.2]: https://github.com/shiv3/gocpp/releases/tag/v0.1.2
[0.1.1]: https://github.com/shiv3/gocpp/releases/tag/v0.1.1
[0.1.0]: https://github.com/shiv3/gocpp/releases/tag/v0.1.0
