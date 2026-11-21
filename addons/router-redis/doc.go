// Package routerredis provides a Redis Pub/Sub backed storage.MessageRouter.
//
// It is intended for multi-instance CSMS deployments where a charge point may
// be connected to a different process than the caller. The router uses a
// storage.ConnectionRegistry to find the target instance, publishes a JSON
// request envelope to that instance's channel, and waits for a JSON response on
// a per-request reply channel.
//
// Deprecated: storage.MessageRouter is superseded by gateway.Downstream; for new
// deployments use the gateway link (addons/link-grpc or addons/link-nats) instead
// of WithInstanceID plus a router. This package keeps working and is removed
// no earlier than v0.4. csms.RouterDownstream and csms.DownstreamRouter convert
// between the two while migrating (docs/clustering.md, "Moving to the gateway
// link").
package routerredis
