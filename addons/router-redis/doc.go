// Package routerredis provides a Redis Pub/Sub backed storage.MessageRouter.
//
// It is intended for multi-instance CSMS deployments where a charge point may
// be connected to a different process than the caller. The router uses a
// storage.ConnectionRegistry to find the target instance, publishes a JSON
// request envelope to that instance's channel, and waits for a JSON response on
// a per-request reply channel.
package routerredis
