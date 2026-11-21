// Package linkgrpc is the gRPC link between gateways and backends
// (docs/design/gateway.md 2.3 and 5.3).
//
// Backends dial gateways. A gateway runs Gateway.Register on its gRPC server;
// a backend creates a Backend with the gateway addresses (or a resolver for a
// headless Service) and passes it to csms.WithGatewayLink. Each backend keeps
// one Attach stream per gateway; the stream carries every message kind in both
// directions with correlation ids, so HTTP/2 flow control is the backpressure
// and a broken stream is detected by the keepalive within seconds.
//
// The first frame of a stream is Hello (protocol range, capabilities, pool,
// version) and the answer HelloAck carries the negotiated protocol. A peer that
// receives a frame body it does not know answers Unsupported and keeps the
// stream.
//
// Experimental: this API may change before v1.0. Every exported identifier of
// this package is part of the split-gateway API; see "Stability" in
// docs/gateway.md.
package linkgrpc
