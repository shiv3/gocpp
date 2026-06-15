# NATS MessageRouter

`github.com/shiv3/gocpp/addons/router-nats` implements
`storage.MessageRouter` using NATS request/reply.

```go
nc, err := nats.Connect(nats.DefaultURL)
if err != nil {
	return err
}

router := routernats.New(nc, "csms-a", registry)
server := csms.NewServer(
	csms.WithInstanceID("csms-a"),
	csms.WithConnectionRegistry(registry),
	csms.WithMessageRouter(router),
)
```

Each instance serves remote calls on `gocpp.route.<instanceID>` by default.
`CallRemote` resolves the charge point owner with
`ConnectionRegistry.LookupGlobal`, sends a request to the target instance
subject, and returns the remote handler response.

Use `WithSubjectPrefix` when several deployments share the same NATS account:

```go
router := routernats.New(nc, "csms-a", registry, routernats.WithSubjectPrefix("prod.gocpp.route"))
```

## Integration Test

Default tests use a fake transport and do not open sockets. To run the live
NATS test:

```sh
nats-server -p 4222
NATS_URL=nats://127.0.0.1:4222 go test -tags=integration ./...
```
