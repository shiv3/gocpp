package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// config is the whole configuration of the gateway: flags, each with an
// environment default (GOCPP_GATEWAY_<NAME>), so a Kubernetes manifest can use
// either.
type config struct {
	ID       string
	Version  string
	Listen   string // WebSocket listener
	Admin    string // /metrics /healthz /readyz
	LinkAddr string // gRPC listener backends dial

	Link       string // grpc | nats
	NATSURL    string // NATS server URL(s) of the NATS link
	NATSPrefix string // subject prefix of the NATS link
	NATSCreds  string // optional NATS credentials file
	NATSAccept time.Duration

	TLSCert, TLSKey, TLSClientCA string
	TicketSecret                 string
	LinkTLSCert, LinkTLSKey      string
	LinkClientCA                 string

	Path          string
	Subprotocols  []string
	RedisAddr     string
	RedisPassword string

	RoutePolicyFile     string
	RoutePolicyInterval time.Duration

	MaxConnections  int
	SoftMaxSessions int
	AcceptRate      float64
	AcceptBurst     int

	UpstreamWait time.Duration
	HandlerTime  time.Duration
	CallTimeout  time.Duration
	DrainWindow  time.Duration
	LogLevel     string

	EdgeHeartbeat            bool
	TenantHeader, TenantAttr string
	TenantMaxSessions        int
	TenantMessageRate        float64
	TenantMessageBurst       int

	LeaseTTL, HeartbeatEvery, ReconcileEvery, ClaimTimeout time.Duration
	LinkKeepalive                                          time.Duration
}

func env(name, def string) string {
	if v, ok := os.LookupEnv("GOCPP_GATEWAY_" + name); ok {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(env(name, "")); err == nil {
		return v
	}
	return def
}

func envFloat(name string, def float64) float64 {
	if v, err := strconv.ParseFloat(env(name, ""), 64); err == nil {
		return v
	}
	return def
}

func envDur(name string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(env(name, "")); err == nil {
		return v
	}
	return def
}

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func parseConfig(args []string) (config, error) {
	var c config
	host, _ := os.Hostname()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&c.ID, "id", env("ID", host), "stable gateway id (the pod name)")
	fs.StringVar(&c.Listen, "listen", env("LISTEN", ":8080"), "WebSocket listen address")
	fs.StringVar(&c.Admin, "admin", env("ADMIN", ":9100"), "admin listen address (/metrics /healthz /readyz)")
	fs.StringVar(&c.LinkAddr, "link-listen", env("LINK_LISTEN", ":9090"), "gRPC link listen address (backends dial it)")
	fs.StringVar(&c.Link, "link", env("LINK", "grpc"), "link transport: grpc (backends dial the gateway) or nats (gateway and backends meet on a NATS server)")
	fs.StringVar(&c.NATSURL, "nats-url", env("NATS_URL", "nats://127.0.0.1:4222"), "NATS server URL(s), comma separated (--link nats)")
	fs.StringVar(&c.NATSPrefix, "nats-prefix", env("NATS_PREFIX", "gocpp"), "subject prefix of the NATS link; must match the backends'")
	fs.StringVar(&c.NATSCreds, "nats-creds", env("NATS_CREDS", ""), "NATS credentials file (--link nats)")
	fs.DurationVar(&c.NATSAccept, "nats-accept-timeout", envDur("NATS_ACCEPT_TIMEOUT", 5*time.Second), "fail a CALL no backend acknowledged within this long (--link nats)")
	fs.StringVar(&c.TLSCert, "tls-cert", env("TLS_CERT", ""), "TLS certificate for the WebSocket listener")
	fs.StringVar(&c.TLSKey, "tls-key", env("TLS_KEY", ""), "TLS key for the WebSocket listener")
	fs.StringVar(&c.TLSClientCA, "tls-client-ca", env("TLS_CLIENT_CA", ""), "CA that verifies charge point client certificates (Security Profile 3)")
	fs.StringVar(&c.TicketSecret, "tls-ticket-secret", env("TLS_TICKET_SECRET", ""), "secret shared by all gateways for TLS session resumption")
	fs.StringVar(&c.LinkTLSCert, "link-tls-cert", env("LINK_TLS_CERT", ""), "TLS certificate for the link listener")
	fs.StringVar(&c.LinkTLSKey, "link-tls-key", env("LINK_TLS_KEY", ""), "TLS key for the link listener")
	fs.StringVar(&c.LinkClientCA, "link-client-ca", env("LINK_CLIENT_CA", ""), "CA that verifies backend client certificates (mTLS between tiers)")
	fs.StringVar(&c.Path, "path", env("PATH_PREFIX", "/ocpp/"), "WebSocket path prefix")
	subs := fs.String("subprotocols", env("SUBPROTOCOLS", "ocpp1.6,ocpp2.0.1"), "comma separated WebSocket subprotocols")
	fs.StringVar(&c.RedisAddr, "redis-addr", env("REDIS_ADDR", ""), "Redis address of the session registry (empty: no registry, sessions are numbered locally)")
	fs.StringVar(&c.RedisPassword, "redis-password", env("REDIS_PASSWORD", ""), "Redis password")
	fs.StringVar(&c.RoutePolicyFile, "route-policy-file", env("ROUTE_POLICY_FILE", ""), "routing policy file (YAML or JSON), re-read on change; with a registry the policy stored there (gocppctl route apply) and this file both apply, the higher revision wins")
	fs.DurationVar(&c.RoutePolicyInterval, "route-policy-interval", envDur("ROUTE_POLICY_INTERVAL", 30*time.Second), "full re-read interval of the policy file")
	fs.IntVar(&c.MaxConnections, "max-connections", envInt("MAX_CONNECTIONS", 0), "hard cap on sessions (503 above it)")
	fs.IntVar(&c.SoftMaxSessions, "soft-max-sessions", envInt("SOFT_MAX_SESSIONS", 0), "sessions at which /readyz reports not-ready (the load balancer stops sending more); 0 = off")
	fs.Float64Var(&c.AcceptRate, "accept-rate", envFloat("ACCEPT_RATE", 200), "upgrades admitted per second (0 = unlimited)")
	fs.IntVar(&c.AcceptBurst, "accept-burst", envInt("ACCEPT_BURST", 400), "accept burst")
	fs.DurationVar(&c.UpstreamWait, "upstream-wait", envDur("UPSTREAM_WAIT", 20*time.Second), "hold an inbound CALL this long for a backend")
	fs.DurationVar(&c.HandlerTime, "handler-timeout", envDur("HANDLER_TIMEOUT", 25*time.Second), "deadline for the backend to answer a CALL")
	fs.DurationVar(&c.CallTimeout, "call-timeout", envDur("CALL_TIMEOUT", 30*time.Second), "timeout of CSMS calls to chargers")
	fs.DurationVar(&c.DrainWindow, "drain-window", envDur("DRAIN_WINDOW", 5*time.Minute), "window in which sessions are closed on SIGTERM")
	fs.BoolVar(&c.EdgeHeartbeat, "edge-heartbeat", env("EDGE_HEARTBEAT", "") == "true", "answer Heartbeat at the gateway without a backend hop (the backend no longer sees Heartbeats)")
	fs.StringVar(&c.TenantHeader, "tenant-header", env("TENANT_HEADER", ""), "request header naming the tenant of a charger (per-tenant limits)")
	fs.StringVar(&c.TenantAttr, "tenant-attr", env("TENANT_ATTR", "tenant"), "session attribute (set by the backend at connect) naming the tenant when the header is absent")
	fs.IntVar(&c.TenantMaxSessions, "tenant-max-sessions", envInt("TENANT_MAX_SESSIONS", 0), "sessions allowed per tenant (503 above it); 0 = unlimited")
	fs.Float64Var(&c.TenantMessageRate, "tenant-message-rate", envFloat("TENANT_MESSAGE_RATE", 0), "inbound messages per second allowed per tenant (CALLERROR above it); 0 = unlimited")
	fs.IntVar(&c.TenantMessageBurst, "tenant-message-burst", envInt("TENANT_MESSAGE_BURST", 0), "token bucket depth of the tenant message limit (default: one second of the rate)")
	fs.DurationVar(&c.LeaseTTL, "lease-ttl", envDur("LEASE_TTL", 15*time.Second), "gateway lease lifetime in the session registry")
	fs.DurationVar(&c.HeartbeatEvery, "lease-heartbeat", envDur("LEASE_HEARTBEAT", 5*time.Second), "lease renewal interval")
	fs.DurationVar(&c.ReconcileEvery, "reconcile-interval", envDur("RECONCILE_INTERVAL", 30*time.Second), "full registry reconcile pass interval")
	fs.DurationVar(&c.ClaimTimeout, "claim-timeout", envDur("CLAIM_TIMEOUT", 2*time.Second), "registry wait on connect before accepting unclaimed")
	fs.DurationVar(&c.LinkKeepalive, "link-keepalive", envDur("LINK_KEEPALIVE", 10*time.Second), "HTTP/2 keepalive ping interval on the link (dead backends are detected within about 1.5x)")
	fs.StringVar(&c.LogLevel, "log-level", env("LOG_LEVEL", "info"), "debug|info|warn|error")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	c.Version = version
	c.Subprotocols = strings.Split(*subs, ",")
	if (c.TLSCert == "") != (c.TLSKey == "") || (c.LinkTLSCert == "") != (c.LinkTLSKey == "") {
		return c, fmt.Errorf("certificate and key must be given together")
	}
	if c.Link != "grpc" && c.Link != "nats" {
		return c, fmt.Errorf("--link must be grpc or nats, not %q", c.Link)
	}
	if c.ID == "" {
		return c, fmt.Errorf("--id is required (no hostname)")
	}
	return c, nil
}
