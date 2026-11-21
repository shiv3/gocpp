// Package registryredis implements storage.SessionRegistry on Redis.
//
// Every operation that changes a charge point's record is one Lua script, so it
// is atomic on the Redis server: Claim increments the per-charge-point epoch and
// stores the new owner, Release deletes only a record that still carries the
// caller's epoch, and Reassert writes only over an absent or older record. A
// delayed or stale writer therefore cannot overwrite a newer connection. Gateway
// liveness is a lease: one key with a TTL per gateway, renewed by Heartbeat.
//
// Keys of one charge point share a hash tag ({cpID}) so the scripts also work
// on Redis Cluster. Lookups and Gateways read several keys without a script;
// Gateways scans for lease keys and is meant for a single Redis (or Memorystore)
// endpoint.
//
// Experimental: this API may change before v1.0. Every exported identifier of
// this package is part of the split-gateway API; see "Stability" in
// docs/gateway.md.
package registryredis
