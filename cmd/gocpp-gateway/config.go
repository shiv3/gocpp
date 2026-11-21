package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
)

// config is the whole configuration of the gateway: flags, each with an
// environment default (GOCPP_GATEWAY_<NAME>), so a Kubernetes manifest can use
// either.
type config struct {
	ID       string
	Version  string
	Listen   string // WebSocket listener
	Metrics  string // /metrics /healthz /readyz (reachable by Prometheus and the kubelet)
	Admin    string // admin API + pprof (loopback by default)
	LinkAddr string // gRPC listener backends dial

	Link       string // grpc | nats
	NATSURL    string // NATS server URL(s) of the NATS link
	NATSPrefix string // subject prefix of the NATS link
	NATSCreds  string // optional NATS credentials file
	NATSCA     string // CA file verifying the NATS server
	NATSCert   string // client certificate for NATS mTLS
	NATSKey    string
	NATSAccept time.Duration

	TLSCert, TLSKey, TLSClientCA string
	TicketSecret                 string
	LinkTLSCert, LinkTLSKey      string
	LinkClientCA                 string
	LinkInsecure                 bool // serve the gRPC link without TLS and client authentication

	AdminTLSCert, AdminTLSKey, AdminTLSClientCA string

	Path          string
	Subprotocols  []string
	RedisAddr     string
	RedisPassword string
	RedisTLS      bool
	RedisCA       string
	RedisCert     string
	RedisKey      string
	RedisSNI      string

	RoutePolicyFile     string
	RoutePolicyInterval time.Duration

	MaxConnections  int
	SoftMaxSessions int

	AdminToken                           string
	AdvertiseAdmin                       string
	AttrSourceFile                       string
	AttrSourceURL                        string
	AttrSourceToken                      string
	AttrSourceTimeout, AttrSourceRefresh time.Duration
	AcceptRate                           float64
	AcceptBurst                          int

	UpstreamWait time.Duration
	HandlerTime  time.Duration
	CallTimeout  time.Duration
	DrainWindow  time.Duration
	LogLevel     string

	EdgeHeartbeat bool
	TapJSONL      string // file receiving frame/session events as JSON lines
	TapNATS       bool   // publish frame/session events on NATS
	TapSample     int    // export 1 frame in N
	TapBuffer     int    // events buffered before dropping
	TapMaxFrame   int    // bytes of each frame kept (negative: metadata only)
	TapNoSessions bool
	TapNoFrames   bool
	TapSkipHB     bool

	TenantHeader, TenantAttr string
	TenantMaxSessions        int
	TenantMessageRate        float64
	TenantMessageBurst       int
	TenantInFlightPool       int
	TenantMaxInFlight        int
	TenantWeights            string

	LeaseTTL, HeartbeatEvery, ReconcileEvery, ClaimTimeout time.Duration
	LinkKeepalive                                          time.Duration
	LinkWriteLinger                                        time.Duration
}

const envPrefix = "GOCPP_GATEWAY_"

// secret reads a secret from file when given, else from the environment. Secrets
// are never flags: a flag value is visible to every user of the host in ps and
// in /proc.
func secret(file, envName string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading secret file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return env(envName, ""), nil
}

// checkSecurity enforces the secure defaults: the gRPC link needs mutual TLS
// unless --link-insecure was given explicitly, and an admin listener beyond
// loopback needs a token.
func checkSecurity(c config) error {
	if c.Link == "grpc" && !c.LinkInsecure {
		if c.LinkTLSCert == "" || c.LinkClientCA == "" {
			return errors.New("refusing to start the gRPC link without mutual TLS: give --link-tls-cert, --link-tls-key and --link-client-ca " +
				"(backends then need a client certificate signed by that CA), or --link-insecure to run it open (development only)")
		}
	}
	if c.AdminToken == "" && !loopbackAddr(c.Admin) {
		return fmt.Errorf("refusing to serve the admin API and pprof on %q without a token: bind --admin to loopback (the default) or set --admin-token-file / %sADMIN_TOKEN", c.Admin, envPrefix)
	}
	return nil
}

// loopbackAddr reports whether a listen address binds only to loopback. An
// empty host (":9101") binds every interface.
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(envPrefix + name); ok {
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
	fs.StringVar(&c.Metrics, "metrics-listen", env("METRICS_LISTEN", ":9100"), "listen address of /metrics /healthz /readyz (no secrets, no control)")
	fs.StringVar(&c.Admin, "admin", env("ADMIN", "127.0.0.1:9101"), "listen address of the admin API and pprof; loopback unless --admin-token-file (or "+envPrefix+"ADMIN_TOKEN) is set")
	fs.StringVar(&c.LinkAddr, "link-listen", env("LINK_LISTEN", ":9090"), "gRPC link listen address (backends dial it)")
	fs.StringVar(&c.Link, "link", env("LINK", "grpc"), "link transport: grpc (backends dial the gateway) or nats (gateway and backends meet on a NATS server)")
	fs.StringVar(&c.NATSURL, "nats-url", env("NATS_URL", "nats://127.0.0.1:4222"), "NATS server URL(s), comma separated (--link nats)")
	fs.StringVar(&c.NATSPrefix, "nats-prefix", env("NATS_PREFIX", "gocpp"), "subject prefix of the NATS link; must match the backends'")
	fs.StringVar(&c.NATSCreds, "nats-creds", env("NATS_CREDS", ""), "NATS credentials file (--link nats)")
	fs.StringVar(&c.NATSCA, "nats-tls-ca", env("NATS_TLS_CA", ""), "CA file that verifies the NATS server (--link nats; use with a tls:// URL)")
	fs.StringVar(&c.NATSCert, "nats-tls-cert", env("NATS_TLS_CERT", ""), "client certificate for NATS mTLS")
	fs.StringVar(&c.NATSKey, "nats-tls-key", env("NATS_TLS_KEY", ""), "client key for NATS mTLS")
	fs.DurationVar(&c.NATSAccept, "nats-accept-timeout", envDur("NATS_ACCEPT_TIMEOUT", 5*time.Second), "fail a CALL no backend acknowledged within this long (--link nats)")
	fs.StringVar(&c.TLSCert, "tls-cert", env("TLS_CERT", ""), "TLS certificate for the WebSocket listener")
	fs.StringVar(&c.TLSKey, "tls-key", env("TLS_KEY", ""), "TLS key for the WebSocket listener")
	fs.StringVar(&c.TLSClientCA, "tls-client-ca", env("TLS_CLIENT_CA", ""), "CA that verifies charge point client certificates (Security Profile 3)")
	fs.StringVar(&c.TicketSecret, "tls-ticket-secret", env("TLS_TICKET_SECRET", ""), "secret shared by all gateways for TLS session resumption")
	fs.StringVar(&c.LinkTLSCert, "link-tls-cert", env("LINK_TLS_CERT", ""), "TLS certificate for the link listener")
	fs.StringVar(&c.LinkTLSKey, "link-tls-key", env("LINK_TLS_KEY", ""), "TLS key for the link listener")
	fs.StringVar(&c.LinkClientCA, "link-client-ca", env("LINK_CLIENT_CA", ""), "CA that verifies backend client certificates (mTLS between tiers)")
	fs.BoolVar(&c.LinkInsecure, "link-insecure", env("LINK_INSECURE", "") == "true", "serve the gRPC link WITHOUT mutual TLS (anyone who can reach it can attach as a backend, read every message and send commands); for local development only")
	fs.StringVar(&c.AdminTLSCert, "admin-tls-cert", env("ADMIN_TLS_CERT", ""), "TLS certificate of the admin listener")
	fs.StringVar(&c.AdminTLSKey, "admin-tls-key", env("ADMIN_TLS_KEY", ""), "TLS key of the admin listener")
	fs.StringVar(&c.AdminTLSClientCA, "admin-tls-client-ca", env("ADMIN_TLS_CLIENT_CA", ""), "CA that verifies admin clients' certificates (optional mTLS)")
	fs.StringVar(&c.Path, "path", env("PATH_PREFIX", "/ocpp/"), "WebSocket path prefix")
	subs := fs.String("subprotocols", env("SUBPROTOCOLS", "ocpp1.6,ocpp2.0.1"), "comma separated WebSocket subprotocols")
	fs.StringVar(&c.RedisAddr, "redis-addr", env("REDIS_ADDR", ""), "Redis address of the session registry (empty: no registry, sessions are numbered locally)")
	redisPassFile := fs.String("redis-password-file", env("REDIS_PASSWORD_FILE", ""), "file holding the Redis password (or set "+envPrefix+"REDIS_PASSWORD)")
	fs.BoolVar(&c.RedisTLS, "redis-tls", env("REDIS_TLS", "") == "true", "connect to Redis over TLS")
	fs.StringVar(&c.RedisCA, "redis-tls-ca", env("REDIS_TLS_CA", ""), "CA file that verifies the Redis server (implies --redis-tls)")
	fs.StringVar(&c.RedisCert, "redis-tls-cert", env("REDIS_TLS_CERT", ""), "client certificate for Redis mTLS")
	fs.StringVar(&c.RedisKey, "redis-tls-key", env("REDIS_TLS_KEY", ""), "client key for Redis mTLS")
	fs.StringVar(&c.RedisSNI, "redis-tls-server-name", env("REDIS_TLS_SERVER_NAME", ""), "server name to verify (default: the host of --redis-addr)")
	fs.StringVar(&c.RoutePolicyFile, "route-policy-file", env("ROUTE_POLICY_FILE", ""), "routing policy file (YAML or JSON), re-read on change; with a registry the policy stored there (gocppctl route apply) and this file both apply, the higher revision wins")
	fs.DurationVar(&c.RoutePolicyInterval, "route-policy-interval", envDur("ROUTE_POLICY_INTERVAL", 30*time.Second), "full re-read interval of the policy file")
	fs.IntVar(&c.MaxConnections, "max-connections", envInt("MAX_CONNECTIONS", 0), "hard cap on sessions (503 above it)")
	adminTokenFile := fs.String("admin-token-file", env("ADMIN_TOKEN_FILE", ""), "file holding the bearer token required by the admin API and pprof (or set "+envPrefix+"ADMIN_TOKEN); required to bind --admin beyond loopback")
	fs.StringVar(&c.AdvertiseAdmin, "advertise-admin", env("ADVERTISE_ADMIN", ""), "host:port at which gocppctl reaches this gateway's admin listener (published in the lease label \"admin\"; e.g. $(POD_NAME).gateway:9100)")
	fs.StringVar(&c.AttrSourceFile, "attr-source-file", env("ATTR_SOURCE_FILE", ""), "external session attributes from a YAML/JSON file (charger id -> labels), added as ext.<key>")
	fs.StringVar(&c.AttrSourceURL, "attr-source-url", env("ATTR_SOURCE_URL", ""), "external session attributes from an HTTP GET (use {cpid} in the URL); added as ext.<key>")
	attrTokenFile := fs.String("attr-source-token-file", env("ATTR_SOURCE_TOKEN_FILE", ""), "file holding the bearer token sent to --attr-source-url (or set "+envPrefix+"ATTR_SOURCE_TOKEN)")
	fs.DurationVar(&c.AttrSourceTimeout, "attr-source-timeout", envDur("ATTR_SOURCE_TIMEOUT", 200*time.Millisecond), "lookup timeout at connect (a failure means no external attributes)")
	fs.DurationVar(&c.AttrSourceRefresh, "attr-source-refresh", envDur("ATTR_SOURCE_REFRESH", 0), "refresh the external attributes of connected sessions this often (0 = connect only)")
	fs.IntVar(&c.SoftMaxSessions, "soft-max-sessions", envInt("SOFT_MAX_SESSIONS", 0), "sessions at which /readyz reports not-ready (the load balancer stops sending more); 0 = off")
	fs.Float64Var(&c.AcceptRate, "accept-rate", envFloat("ACCEPT_RATE", 200), "upgrades admitted per second (0 = unlimited)")
	fs.IntVar(&c.AcceptBurst, "accept-burst", envInt("ACCEPT_BURST", 400), "accept burst")
	fs.DurationVar(&c.UpstreamWait, "upstream-wait", envDur("UPSTREAM_WAIT", 20*time.Second), "hold an inbound CALL this long for a backend")
	fs.DurationVar(&c.HandlerTime, "handler-timeout", envDur("HANDLER_TIMEOUT", 25*time.Second), "deadline for the backend to answer a CALL")
	fs.DurationVar(&c.CallTimeout, "call-timeout", envDur("CALL_TIMEOUT", 30*time.Second), "timeout of CSMS calls to chargers")
	fs.DurationVar(&c.DrainWindow, "drain-window", envDur("DRAIN_WINDOW", 5*time.Minute), "window in which sessions are closed on SIGTERM")
	fs.BoolVar(&c.EdgeHeartbeat, "edge-heartbeat", env("EDGE_HEARTBEAT", "") == "true", "answer Heartbeat at the gateway without a backend hop (the backend no longer sees Heartbeats)")
	fs.StringVar(&c.TapJSONL, "tap-jsonl", env("TAP_JSONL", ""), "append frame and session events to this file as JSON lines (lossy, bounded; never slows connections)")
	fs.BoolVar(&c.TapNATS, "tap-nats", env("TAP_NATS", "") == "true", "publish frame and session events on NATS (<nats-prefix>.tap.frame.<cp>, .tap.session.<cp>); uses --nats-url")
	fs.IntVar(&c.TapSample, "tap-sample", envInt("TAP_SAMPLE", 1), "export one frame out of every N (sessions are never sampled)")
	fs.IntVar(&c.TapBuffer, "tap-buffer", envInt("TAP_BUFFER", 4096), "events buffered for the tap sinks before dropping (counted in gocpp_tap_events_total)")
	fs.IntVar(&c.TapMaxFrame, "tap-max-frame", envInt("TAP_MAX_FRAME", 2048), "bytes of each frame kept in the tap (negative: metadata only)")
	fs.BoolVar(&c.TapNoFrames, "tap-no-frames", env("TAP_NO_FRAMES", "") == "true", "tap session events only")
	fs.BoolVar(&c.TapNoSessions, "tap-no-sessions", env("TAP_NO_SESSIONS", "") == "true", "tap frames only")
	fs.BoolVar(&c.TapSkipHB, "tap-skip-heartbeat", env("TAP_SKIP_HEARTBEAT", "") == "true", "leave Heartbeat frames out of the tap")
	fs.StringVar(&c.TenantHeader, "tenant-header", env("TENANT_HEADER", ""), "request header naming the tenant of a charger (per-tenant limits)")
	fs.StringVar(&c.TenantAttr, "tenant-attr", env("TENANT_ATTR", "tenant"), "session attribute (set by the backend at connect) naming the tenant when the header is absent")
	fs.IntVar(&c.TenantMaxSessions, "tenant-max-sessions", envInt("TENANT_MAX_SESSIONS", 0), "sessions allowed per tenant (503 above it); 0 = unlimited")
	fs.Float64Var(&c.TenantMessageRate, "tenant-message-rate", envFloat("TENANT_MESSAGE_RATE", 0), "inbound messages per second allowed per tenant (CALLERROR above it); 0 = unlimited")
	fs.IntVar(&c.TenantInFlightPool, "tenant-inflight-pool", envInt("TENANT_INFLIGHT_POOL", 0), "inbound CALLs in flight across all tenants, split by weight (CALLERROR above a tenant's share when others need it); 0 = off")
	fs.IntVar(&c.TenantMaxInFlight, "tenant-max-inflight", envInt("TENANT_MAX_INFLIGHT", 0), "inbound CALLs in flight allowed per tenant; 0 = unlimited")
	fs.StringVar(&c.TenantWeights, "tenant-weights", env("TENANT_WEIGHTS", ""), "weights of the in-flight pool as tenant=weight,... (default weight 1)")
	fs.IntVar(&c.TenantMessageBurst, "tenant-message-burst", envInt("TENANT_MESSAGE_BURST", 0), "token bucket depth of the tenant message limit (default: one second of the rate)")
	fs.DurationVar(&c.LeaseTTL, "lease-ttl", envDur("LEASE_TTL", 15*time.Second), "gateway lease lifetime in the session registry")
	fs.DurationVar(&c.HeartbeatEvery, "lease-heartbeat", envDur("LEASE_HEARTBEAT", 5*time.Second), "lease renewal interval")
	fs.DurationVar(&c.ReconcileEvery, "reconcile-interval", envDur("RECONCILE_INTERVAL", 30*time.Second), "full registry reconcile pass interval")
	fs.DurationVar(&c.ClaimTimeout, "claim-timeout", envDur("CLAIM_TIMEOUT", 2*time.Second), "registry wait on connect before accepting unclaimed")
	fs.DurationVar(&c.LinkKeepalive, "link-keepalive", envDur("LINK_KEEPALIVE", 10*time.Second), "HTTP/2 keepalive ping interval on the link (dead backends are detected within about 1.5x)")
	fs.DurationVar(&c.LinkWriteLinger, "link-write-linger", envDur("LINK_WRITE_LINGER", 0), "let a busy gRPC link stream gather frames for up to this long before writing them to the backend as one batch (saves CPU at high message rates, adds up to this much latency while it applies; a stream whose frames are on average more than half of it apart is never delayed); 0 = off; at most 5ms")
	fs.StringVar(&c.LogLevel, "log-level", env("LOG_LEVEL", "info"), "debug|info|warn|error")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	c.Version = version
	var err error
	if c.RedisPassword, err = secret(*redisPassFile, "REDIS_PASSWORD"); err != nil {
		return c, err
	}
	if c.AdminToken, err = secret(*adminTokenFile, "ADMIN_TOKEN"); err != nil {
		return c, err
	}
	if c.AttrSourceToken, err = secret(*attrTokenFile, "ATTR_SOURCE_TOKEN"); err != nil {
		return c, err
	}
	c.Subprotocols = strings.Split(*subs, ",")
	if (c.TLSCert == "") != (c.TLSKey == "") || (c.LinkTLSCert == "") != (c.LinkTLSKey == "") {
		return c, fmt.Errorf("certificate and key must be given together")
	}
	if (c.RedisCert == "") != (c.RedisKey == "") || (c.NATSCert == "") != (c.NATSKey == "") || (c.AdminTLSCert == "") != (c.AdminTLSKey == "") {
		return c, fmt.Errorf("certificate and key must be given together")
	}
	if err := checkSecurity(c); err != nil {
		return c, err
	}
	if c.Link != "grpc" && c.Link != "nats" {
		return c, fmt.Errorf("--link must be grpc or nats, not %q", c.Link)
	}
	if c.ID == "" {
		return c, fmt.Errorf("--id is required (no hostname)")
	}
	if c.LinkWriteLinger < 0 || c.LinkWriteLinger > linkgrpc.MaxWriteLinger {
		return c, fmt.Errorf("--link-write-linger must be between 0 and %v, not %v", linkgrpc.MaxWriteLinger, c.LinkWriteLinger)
	}
	if c.LinkWriteLinger > 0 && c.Link != "grpc" {
		return c, fmt.Errorf("--link-write-linger applies to the gRPC link only (the NATS client batches its own writes)")
	}
	return c, nil
}
