# 0006. Optional split into a connection tier and a processing tier

Status: Accepted (public API experimental before v1.0)
Date: 2026-11-25

## Context

Deploying a CSMS disconnects every charge point on that process, and a reconnect
storm after a rollout is costly. Most deployments change handler code often and
connection handling rarely. Full design and rationale:
[design/gateway.md](../design/gateway.md); user guide: [gateway.md](../gateway.md).

## Decision

A CSMS can run as two parts joined by a link, or as one process as before:

- The gateway owns the TLS/WebSocket sessions and runs the whole connection
  layer (framing, CALL/CALLRESULT correlation, call slot, timeouts, keepalive,
  frame observer). It has no handlers; it forwards each inbound CALL, payload
  untouched, to the processing tier and writes the answer back. It never reads a
  payload, so new actions and schema changes do not require a gateway deploy.
- The backend holds no per-charger state. It runs the payload pipeline
  (`dispatcher.Pipeline`: signature unwrap, schema validation, lenient coercion,
  time encoding, error mapping) and the handlers, and reaches a charger through
  the gateway that holds it. Deploying a backend disconnects nobody.
- Per-charger ordering comes from OCPP (one outstanding CALL per side), enforced at
  the gateway, so the link needs no partitioning and any backend can serve any
  message.
- Links are pluggable: in-process, gRPC streams (the default, backends dial
  gateways) and NATS. Sessions are tracked in a `SessionRegistry` whose epochs make
  every write conditional.
- The typed API (`csms.On`, `csms.Call`, generated handlers) is the same in both
  modes. The pipeline is the single place payload policy lives, so error codes
  are identical embedded and split.

## Consequences

- A gateway deploy still reconnects its chargers (a TLS socket cannot move between
  processes); rate-limited drain and accept limiting make that gradual.
- An extra hop costs latency: CI gates the in-process split at 0.5 ms mean and
  2 ms p99 per message, and runs the conformance and e2e suites in split mode
  (in-process and over NATS) as well as embedded.
- The split API, the link protocol and the registry layout are marked
  experimental in [gateway.md](../gateway.md); mixed-version fleets are supported
  only between adjacent releases.

## Alternatives considered

- Splitting at the frame level: the gateway would need to understand payloads, and
  would be redeployed as often as the backend.
- A broker or durable log on the request path: adds latency and ordering problems
  that OCPP's one-outstanding-CALL rule already solves; kept only for one-way taps.
