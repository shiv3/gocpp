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

## Partial drains for rollouts

Give `DrainOptions` a selection and `Drain` becomes a partial drain: only the
matching sessions are closed, at the given pace, idle first, with the same 1012
close, and the server keeps accepting upgrades and stays ready. A rollout
controller uses it to move sessions gradually, for example off a canary gateway or
onto a new release.

```go
srv := csms.NewServer(
    csms.WithSessionRegistry(reg, csms.SessionConfig{Version: "v2", Label: "canary"}),
    csms.WithSessionAttributes(func(r *http.Request, id string) map[string]string {
        return map[string]string{"site": siteOf(id), "vendor": vendorOf(r)}
    }))

// move a tenth of site "s2" at 5 sessions per second
err := srv.Drain(ctx, csms.DrainOptions{
    Attrs: map[string]string{"site": "s2"}, Fraction: 0.1, Rate: 5, Window: 10 * time.Minute})
// or: close at most 50 sessions of canary-labelled chargers
err = srv.Drain(ctx, csms.DrainOptions{Max: 50, Attrs: map[string]string{"label.canary": "1"}, Rate: 5})
st := srv.DrainStatus() // Total, Closed, Forced, Remaining, Rate, Elapsed
```

Selectors: `CPIDs`, `Attrs` (all pairs equal), `Select` (predicate on the
registry record), `Filter` (predicate on the `*Conn`), then `Max` / `Fraction` pick
a stable subset by id hash. During a partial drain the lease stays `serving` and
upgrades are accepted, but `ReadyHandler` reports not-ready so the load balancer
does not refill the process (`RefuseDuring` also answers upgrades with 503).

**Attributes.** Set at connect (`WithSessionAttributes`) and updated later with
`Conn.SetAttrs` / `Server.SetSessionAttrs` when they become known (firmware
from BootNotification). `SetAttrs` *merges*: a key replaces its value, an empty
value deletes the key, other keys stay; the total size is capped at 1 KiB
(`ErrAttrsTooLarge`). In the registry record they are `Session.Meta` keys with the
reserved prefix `attr.` (`Session.Attrs()` returns them without it); the prefix
`route.` is reserved for routing decisions written by a gateway (`route.pool`).
Attribute keys starting with `gocpp.` are interpreted by a gateway (`gocpp.busy`),
not by this library. The record also has `GatewayVersion`; the lease
(`SessionRegistry.Gateways`) carries `Labels`: `version` from
`SessionConfig.Version`, your own via `SessionConfig.Labels` / `Server.SetGatewayLabel`,
and `policy_rev` (reserved: the routing policy revision a gateway has applied).

## Soft connection cap

`WithSoftMaxConnections(n)` / `Server.SetSoftMaxConnections(n)`: at or above n
sessions `ReadyHandler` answers 503 so the load balancer stops sending more, but
upgrades are still accepted (up to `WithMaxConnections`) and existing sessions are
never touched. A canary process uses it to fill only its share of the fleet.

## Metrics

A `Metrics` implementation that also implements `observability.SessionMetrics`
(the Prometheus and OpenTelemetry adapters do) receives, for canary comparison:
`gocpp_session_duration_seconds{version}` (histogram at close),
`gocpp_closes_total{version,code,cause}` (code `1000`/`1008`/`1012`/`1006`; cause
`server_close`, `replaced`, `drain`, `drain_timeout`, `dropped`, `other`) and
`gocpp_accepts_total{result}` (`ok`, `rate_limited`, `draining`, `shutting_down`,
`full`, `auth_rejected`, `bad_request`, `subprotocol`). Add `gateway_id` /
`gateway_version` constant labels with `prometheus.WrapRegistererWith`. TLS
handshake failures happen before the handler and are not visible here.

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

## Measured

Rolling restart of a 4,000-charger fleet (simulated 1.6J chargers, jittered
back-off, balancer sending new connections to the ready process; no TLS, no auth;
harness in branch `test/loadtest-drain`, `interop/loadtest/drain`):

| | peak connect attempts at the new process | peak server CPU (250 ms) | fleet back to 99% |
|---|---|---|---|
| SIGTERM, exit at once | about 2,000 per second (all 4,000 within 3 s) | 0.3 to 0.4 cores | 3 s (all of them offline until then) |
| `Drain`, window 60 s (target 74 per second) | 90 per second | 0.12 cores | 56 s (about 3,850 of 4,000 connected throughout) |
| `Drain`, window 30 s (target 148 per second) | 161 per second | 0.12 cores | 29 s |
| SIGTERM + `WithAcceptRateLimit(200, 400)` on the new process | 984 attempts, 594 upgrades per second | 0.24 cores | 33 s |

The reconnect peak follows the configured pace (observed peaks are within 25% of
the target, the excess being the chargers' own retry jitter), and the fleet
stays about 96% connected during a 60 s drain instead of 0% for the restart. The
bare gocpp server handles 4,000 reconnects in 3 s without trouble, so the
absolute CPU saving here is small; the pace matters where each connect costs more
(TLS handshakes, authentication against a database, per-connect application work).
