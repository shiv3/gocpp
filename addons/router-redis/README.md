# Redis MessageRouter Addon

`github.com/shiv3/gocpp/addons/router-redis` provides a Redis Pub/Sub backed
implementation of `storage.MessageRouter` for multi-instance CSMS deployments.

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
router := routerredis.New(rdb, instanceID, connectionRegistry)
```

`CallRemote` resolves the target CSMS instance with
`ConnectionRegistry.LookupGlobal`, publishes a JSON request envelope to that
instance's request channel, and waits for a JSON response on a per-request reply
channel. `ServeRemote` subscribes to the local instance request channel and
invokes the supplied `storage.RemoteHandler` for each valid request.

## Options

- `WithChannelPrefix(prefix string)` changes the Redis channel namespace.
- `WithRequestTimeout(timeout time.Duration)` sets the maximum time for a remote
  call. Use zero or a negative duration to rely only on the caller's context.

## Redis Channels

The default channel prefix is `gocpp:router`.

- Requests: `gocpp:router:requests:<instanceID>`
- Replies: `gocpp:router:replies:<sourceInstanceID>:<requestID>`

The envelopes are JSON. Binary request and response payloads are encoded by
Go's JSON encoder as base64 strings.

## Testing

Default tests use an in-memory fake broker and do not require Redis:

```sh
cd addons/router-redis
go test ./...
```

Integration tests require a local Redis server. Start Redis on `localhost:6379`
or set `REDIS_ADDR`, then run:

```sh
cd addons/router-redis
go test -tags=integration ./...
```
