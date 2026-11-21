// Package gateway defines the contract between the two tiers of a split OCPP
// CSMS: the gateway (connection tier), which owns the WebSocket sessions, and
// the backend (processing tier), which runs the application's handlers.
//
// The gateway runs the whole of a normal csms.Server (framing, CALL/CALLRESULT
// correlation, timeouts, keepalive) except for the handlers: it forwards every
// inbound CALL as an Envelope to an Upstream and waits for a Reply. It never
// looks inside a payload. The backend holds no per-charger state; it receives
// envelopes, runs the payload pipeline and the handler, and reaches a charger
// through a Downstream. A Link carries both directions between the tiers.
//
// The same typed handler code (csms.On, csms.Call, the generated handlers)
// runs in embedded mode and behind a gateway. See docs/gateway.md and
// docs/design/gateway.md.
//
// This package has no dependencies beyond the standard library and
// core/ocppj. In-process links live in package inproc; the gRPC link is the
// separate module addons/link-grpc.
package gateway
