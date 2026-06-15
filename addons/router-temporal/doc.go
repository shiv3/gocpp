// Package routertemporal provides an experimental Temporal-backed
// storage.MessageRouter implementation for gocpp CSMS deployments.
//
// The router uses Temporal to make CSMS-to-charge-point calls visible and
// durable across process crashes. CallRemote looks up the charge point owner in
// storage.ConnectionRegistry, starts a short-lived workflow on this instance's
// task queue, and that workflow schedules an activity on the owning instance's
// task queue. ServeRemote starts the Temporal worker for the local task queue
// and registers the workflow plus an activity that invokes the supplied
// storage.RemoteHandler.
//
// This package is experimental. It assumes that ConnectionRegistry's global
// instance IDs can be mapped to Temporal task queues, and it preserves only the
// MessageRouter request/response contract. It does not provide exactly-once OCPP
// delivery semantics; handler errors can be retried by Temporal and callers
// should make forwarded calls idempotent where possible.
package routertemporal
