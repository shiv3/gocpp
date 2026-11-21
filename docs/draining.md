# Draining and reconnect-storm control

A restart of a process that holds thousands of charge point sockets makes every
charger reconnect at the same moment. The new process (or its neighbours) then
has to terminate that many TLS handshakes, run that many authentications and
accept that many BootNotifications at once. `Server.Drain` and the admission
limiters turn that spike into a controlled ramp. They work in a single process;
no gateway or cluster is needed.

## Deploying with Drain

```go
srv := csms.NewServer(csms.WithAcceptRateLimit(200, 400))
httpSrv := &http.Server{Addr: ":8080", Handler: mux} // mux has srv.Handler() and "/ready": srv.ReadyHandler()

// on SIGTERM:
err := srv.Drain(ctx, csms.DrainOptions{
    Window:     5 * time.Minute, // all sessions leave within this time
    IdleGrace:  30 * time.Second,
    OnProgress: func(p csms.DrainProgress) { log.Printf("drain: %d/%d closed", p.Closed, p.Total) },
})
_ = srv.Shutdown(ctx)
_ = httpSrv.Shutdown(ctx)
```

What `Drain` does, in order:

1. **Refuses new upgrades** with `503 Service Unavailable` and a `Retry-After`
   spread over 0.5x to 1.5x of `DrainOptions.RetryAfter` (default 10 s).
   `Server.Draining()` becomes true and `Server.ReadyHandler()` answers 503, so
   the load balancer or Kubernetes readiness probe stops routing to the process.
   With a session registry (`WithSessionRegistry`) the gateway lease is marked
   `draining`.
2. **Closes sessions at a fixed rate**: `DrainOptions.Rate`, by default
   sessions / (0.9 x Window) (10,000 chargers and a 5 minute window leave at about
   37 per second; the last tenth of the window is slack). Idle sessions go first.
3. A session that has a CALL in flight or queued in either direction when its
   turn comes **stops accepting new outbound calls** (they fail at once with
   `ocppj.ErrGatewayDraining`, nothing written, safe to retry after the charger
   reconnects elsewhere) and is closed as soon as what is in flight finished, or
   when `IdleGrace` runs out.
4. The close frame has **code 1012 (service restart)**, reason `gateway draining`;
   the `WithOnDisconnect` callback receives `ocppj.ErrGatewayDraining`.
5. When `Window` ends, sessions still open are closed anyway; calls in flight
   fail with `ocppj.ErrDrainTimeout` and may have been executed by the charger.

`Drain` returns when every session is torn down, or with `ctx.Err()` if `ctx`
ends first. It does not stop the HTTP server; call `Shutdown` afterwards. A
frame a charger sends at the instant its socket is closed can be lost, as with
any disconnect.

WebSocket close frames cannot carry a retry delay, so the spread of reconnects is
the pace of the closes. If the charger firmware has a retry back-off setting (for
example `RetryBackOffWaitMinimum` / `RetryBackOffRandomRange` in OCPP 2.0.1),
set it as well.

Kubernetes: give the pod a `terminationGracePeriodSeconds` larger than the drain
window, run `Drain` from a `preStop` hook or the SIGTERM handler, roll one pod at
a time (`maxUnavailable: 1`) and keep capacity for `N-1` pods.

## Admission control

* `WithMaxConnections(n)`: capacity cap (exists).
* `WithAcceptRateLimit(rate, burst)`: token bucket on upgrades. Over the limit an
  upgrade gets `503` plus a jittered `Retry-After` (about `burst/rate` seconds) before
  authentication, so a storm after a crash cannot become a CPU or database storm.
  `Server.AdmissionStats()` counts the refusals.
* `RateLimitListener(l, rate, burst)`: closes connections over the rate at TCP
  accept, before the TLS handshake, for servers that terminate TLS themselves.
  Clients see a failed connect rather than a 503, so use it as a backstop.
* `transport.RotateSharedTicketKeys(tlsConfig, secret, rotate)`: derive TLS session
  ticket keys from a secret shared by all processes, so a charger that reconnects
  to a different pod resumes its TLS session instead of a full handshake.
