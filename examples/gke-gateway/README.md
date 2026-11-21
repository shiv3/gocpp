# Gateway on GKE (example manifests)

A StatefulSet of `gocpp-gateway` pods behind an L4 (passthrough) load balancer,
a headless Service the backends use to dial every gateway pod, and a backend
Deployment that attaches to all of them. These manifests are a starting point,
not a tested deployment: the load balancer behaviours they rely on are listed
under "Verify on your cluster" and have not been measured.

Files

- `gateway.yaml`: StatefulSet (`partition` for canary rollouts, `minReadySeconds`,
  startup probe using `gocpp-gateway probe`), headless Service for the link, the
  L4 `LoadBalancer` Service for chargers, PodDisruptionBudget.
- `tls.yaml`: cert-manager Issuer/Certificates for mutual TLS on the gRPC link
  and TLS on the admin API (or create the Secrets yourself).
- `backend.yaml`: backend Deployment (pool `default`) and an optional canary
  Deployment (pool `canary`, receives no traffic until a router names the pool).

Security

- The gateway refuses to start the gRPC link without TLS and a client CA
  (`--link-tls-cert --link-tls-key --link-client-ca`); `--link-insecure` exists for
  development only and logs a warning. Backends need a client certificate from the
  same CA (`tls.yaml`).
- The admin API and pprof listen on a separate port (here `:9443`, with the token
  from the `gateway-admin` Secret and TLS); on the default `127.0.0.1:9101` no
  token is needed. A non-loopback bind without a token is refused. Metrics and
  probes stay on `:9100` and carry no secrets.
- gocppctl: `GOCPPCTL_ADMIN_TOKEN` (or `--admin-token-file`), `--admin-ca` for the
  TLS verification, `GOCPPCTL_REDIS_PASSWORD` and `--redis-tls --redis-ca` for Redis.
- Charger-facing TLS (`gateway-tls`), Redis TLS, and NATS permissions (if you use
  the NATS link) are described in `docs/gateway.md`, "Security".
- Add a NetworkPolicy that only lets backends reach 9090 and your operators reach
  9443.

Rollout notes

- Gateway deploy: `kubectl rollout` proceeds one pod at a time (highest ordinal
  first); each pod drains on SIGTERM (readiness fails, sessions are closed at a
  paced rate with 1012, idle first) and `terminationGracePeriodSeconds` must
  exceed `--drain-window`. Size the fleet so that `G-1` gateways hold all chargers.
- Gateway canary: set `updateStrategy.rollingUpdate.partition` to `G-1`; give the
  pod `--soft-max-sessions` (a share of the fleet) so it reports not-ready at its
  share and the load balancer stops filling it.
- Backend deploy: ordinary rolling update; a draining backend answers Nack for
  new work and finishes what it has, so no charger sees it.

Verify on your cluster (not measured here)

- A pod that turns not-ready keeps its established WebSocket connections
  (connection persistence on unhealthy backends of the L4 load balancer).
- How new connections are spread when every pod is not-ready (expected: fail
  open across all pods).
