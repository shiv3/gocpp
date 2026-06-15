# Temporal MessageRouter addon

Experimental `storage.MessageRouter` implementation for `github.com/shiv3/gocpp`
that uses Temporal workflows and activities to forward CSMS-to-charge-point calls
to the CSMS instance that owns the live connection.

## Design

- `New(client, taskQueue, registry, opts...)` returns a router for one CSMS
  instance. `taskQueue` is the local Temporal task queue polled by
  `ServeRemote`.
- `CallRemote` looks up `cpID` in `storage.ConnectionRegistry.LookupGlobal`,
  starts one short-lived routing workflow on the local task queue, waits for the
  workflow result with the caller's context, and returns the activity response.
- The workflow schedules the delivery activity on the target instance's task
  queue. By default the registry `instanceID` is used directly as the task queue;
  use `WithTaskQueueForInstance` if your deployment has a different mapping.
- `ServeRemote` starts a Temporal worker on the local task queue and registers
  both the routing workflow and an activity that invokes the supplied
  `storage.RemoteHandler`.
- `CallLocal` intentionally matches the core no-op router and returns
  `storage.ErrNotLocal`.

## Usage sketch

```go
router := routertemporal.New(temporalClient, "csms-instance-a", connRegistry)

go func() {
    _ = router.ServeRemote(ctx, func(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
        return dispatcher.Call(ctx, cpID, action, req)
    })
}()
```

If registry instance IDs are not task queue names:

```go
router := routertemporal.New(
    temporalClient,
    "queue-a",
    connRegistry,
    routertemporal.WithTaskQueueForInstance(func(instanceID string) (string, bool) {
        queue, ok := map[string]string{"instance-a": "queue-a", "instance-b": "queue-b"}[instanceID]
        return queue, ok
    }),
)
```

## Limitations

- This addon is experimental and the workflow/activity names are the only
  compatibility surface currently kept stable.
- Temporal retries improve durability for crashes and transient handler errors,
  but they do not make OCPP calls exactly once. Make routed commands idempotent
  where possible.
- `CallRemote` returns `storage.ErrNotLocal` when the registry has no owner or
  the owner cannot be mapped to a task queue.
- By default, if the caller context expires while waiting for the workflow
  result, the Temporal workflow may continue until its configured timeouts. Use
  `WithCancelWorkflowOnContextError(true)` when caller cancellation should also
  cancel the Temporal workflow.
- Unit tests use `go.temporal.io/sdk/testsuite` and do not require a Temporal
  server. The integration test is behind `//go:build integration` and requires
  `TEMPORAL_HOST_PORT`.

