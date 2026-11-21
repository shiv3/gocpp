# registry-redis

`storage.SessionRegistry` on Redis: the shared table that says which CSMS
instance holds which charge point's socket.

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
srv := csms.NewServer(
    csms.WithSessionRegistry(registryredis.New(rdb), csms.SessionConfig{GatewayID: "csms-2"}),
    csms.WithMessageRouter(router), // e.g. addons/router-redis
)
```

* `Claim`, `Release` and `Reassert` are single Lua scripts, atomic on the server.
  Epochs come from `INCR` on a per-charge-point key that outlives the record.
* Gateway liveness is one key with a TTL per gateway (`Heartbeat`).
* Keys of one charge point share a `{cpID}` hash tag, so the scripts run on Redis
  Cluster. `Gateways()` uses `SCAN` and expects a single endpoint (Memorystore
  standard tier is enough; inbound traffic never touches the registry).

## Tests

Unit tests run against an embedded [miniredis](https://github.com/alicebob/miniredis)
(it executes the Lua scripts). To run the same tests against a real server, set
`GOCPP_REDIS_ADDR=host:6379`; the tests use a unique key prefix and the
TTL-dependent cases are skipped there.
