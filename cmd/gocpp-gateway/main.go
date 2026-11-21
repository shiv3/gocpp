// Command gocpp-gateway is the reference connection tier of a split OCPP CSMS:
// it terminates TLS and WebSocket sessions, runs no handlers, and forwards every
// message over the gRPC link to the processing tier (backends dial it). See
// docs/gateway.md.
//
//	gocpp-gateway [serve] [flags]   run the gateway (default)
//	gocpp-gateway probe [flags]     pre-flight: upgrade, BootNotification, Heartbeat
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/observability/metrics/prom"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "probe":
		err = probe(args)
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q (serve, probe, version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocpp-gateway:", err)
		os.Exit(1)
	}
}

func logLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}

func loadPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s: no certificates", path)
	}
	return p, nil
}

func serve(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	wrapped := prometheus.WrapRegistererWith(prometheus.Labels{"gateway_id": cfg.ID, "gateway_version": cfg.Version}, reg)

	// Routing policy (design 5.1): the engine routes every message to a backend
	// pool; without a policy everything goes to the default pool.
	var gw *csms.Server
	engine := routepolicy.NewEngine(routepolicy.WithMetrics(newRouteMetrics(wrapped)), routepolicy.WithLogger(log),
		routepolicy.WithOnApply(func(rev uint64) {
			if gw != nil {
				gw.SetGatewayLabel(ctx, "policy_rev", strconv.FormatUint(rev, 10))
			}
		}),
		routepolicy.WithOnSuspend(func(rules []string) {
			if gw != nil {
				gw.SetGatewayLabel(ctx, "suspended", joinRules(rules))
			}
		}))
	wrapped.MustRegister(heldCollector{engine})

	var link gatewayLink
	var grpcLink *linkgrpc.Gateway
	switch cfg.Link {
	case "nats":
		var nopts []nats.Option
		if cfg.NATSCreds != "" {
			nopts = append(nopts, nats.UserCredentials(cfg.NATSCreds))
		}
		nopts = append(nopts, nats.PingInterval(cfg.LinkKeepalive))
		nc, err := linknats.Connect(cfg.NATSURL, "gocpp-gateway-"+cfg.ID, nopts...)
		if err != nil {
			return err
		}
		defer nc.Close()
		nl, err := linknats.NewGateway(nc, linknats.WithPrefix(cfg.NATSPrefix),
			linknats.WithRouteObserver(engine), linknats.WithGatewayVersion(cfg.Version), linknats.WithGatewayMetrics(newLinkMetrics(wrapped)),
			linknats.WithUpstreamWait(cfg.UpstreamWait), linknats.WithAcceptTimeout(cfg.NATSAccept))
		if err != nil {
			return err
		}
		defer nl.Close()
		link = nl
	default:
		grpcLink = linkgrpc.NewGateway(linkgrpc.WithRouteObserver(engine),
			linkgrpc.WithGatewayVersion(cfg.Version), linkgrpc.WithGatewayMetrics(newLinkMetrics(wrapped)),
			linkgrpc.WithUpstreamWait(cfg.UpstreamWait))
		link = grpcLink
	}
	wrapped.MustRegister(poolCollector{link})

	opts := []csms.Option{
		csms.WithGatewayID(cfg.ID), csms.WithPath(cfg.Path), csms.WithSubProtocols(cfg.Subprotocols...),
		csms.WithHandlerTimeout(cfg.HandlerTime), csms.WithCallTimeout(cfg.CallTimeout),
		csms.WithMaxConnections(cfg.MaxConnections), csms.WithSoftMaxConnections(cfg.SoftMaxSessions),
		csms.WithAcceptRateLimit(cfg.AcceptRate, cfg.AcceptBurst),
		csms.WithMetrics(prom.New(wrapped)), csms.WithLogger(log), csms.WithRouter(engine),
	}
	if cfg.EdgeHeartbeat {
		opts = append(opts, csms.WithEdgeHeartbeat())
	}
	if cfg.TenantMaxSessions > 0 || cfg.TenantMessageRate > 0 || cfg.TenantInFlightPool > 0 || cfg.TenantMaxInFlight > 0 {
		tl := csms.TenantLimits{
			AttrKey:      cfg.TenantAttr,
			InFlightPool: cfg.TenantInFlightPool,
			Default: csms.TenantLimit{MaxSessions: cfg.TenantMaxSessions, MessageRate: cfg.TenantMessageRate,
				MessageBurst: cfg.TenantMessageBurst, MaxInFlight: cfg.TenantMaxInFlight},
		}
		if cfg.TenantWeights != "" {
			tl.Tenants = map[string]csms.TenantLimit{}
			for _, kv := range strings.Split(cfg.TenantWeights, ",") {
				name, ws, ok := strings.Cut(strings.TrimSpace(kv), "=")
				w, err := strconv.ParseFloat(ws, 64)
				if !ok || err != nil || w <= 0 {
					return fmt.Errorf("--tenant-weights: %q is not tenant=weight", kv)
				}
				l := tl.Default
				l.Weight = w
				tl.Tenants[name] = l
			}
		}
		if h := cfg.TenantHeader; h != "" {
			tl.Resolver = csms.TenantFromRequest(func(r *http.Request) (string, bool) { v := r.Header.Get(h); return v, v != "" })
		}
		opts = append(opts, csms.WithTenantLimits(tl))
	}
	var rdb *redis.Client
	var policyStore storage.PolicyStore
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
		defer func() { _ = rdb.Close() }()
		reg := registryredis.New(rdb)
		policyStore = reg
		opts = append(opts, csms.WithSessionRegistry(reg, csms.SessionConfig{GatewayID: cfg.ID, LeaseTTL: cfg.LeaseTTL, HeartbeatInterval: cfg.HeartbeatEvery, ReconcileInterval: cfg.ReconcileEvery, ClaimTimeout: cfg.ClaimTimeout}))
	}
	gw = csms.NewGatewayServer(link, opts...)
	gw.SetGatewayLabel(ctx, "version", cfg.Version)
	gw.SetGatewayLabel(ctx, "policy_rev", "0")
	gw.SetGatewayLabel(ctx, "caps", "route.v1,attrs.set,send.21,cancel,orphan")

	if cfg.RoutePolicyFile != "" {
		go func() {
			_ = engine.Watch(ctx, routepolicy.FileSource{Path: cfg.RoutePolicyFile, Interval: cfg.RoutePolicyInterval})
		}()
	}
	if policyStore != nil {
		go func() { _ = engine.Watch(ctx, routepolicy.StoreSource{Store: policyStore}) }()
	}

	// WebSocket listener.
	lis, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	lis = csms.RateLimitListener(lis, cfg.AcceptRate*2, cfg.AcceptBurst)
	wsSrv := &http.Server{Handler: gw.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if cfg.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return err
		}
		tc := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		if cfg.TLSClientCA != "" {
			pool, err := loadPool(cfg.TLSClientCA)
			if err != nil {
				return err
			}
			tc.ClientCAs, tc.ClientAuth = pool, tls.VerifyClientCertIfGiven
		}
		if cfg.TicketSecret != "" {
			defer transport.RotateSharedTicketKeys(tc, []byte(cfg.TicketSecret), 0)()
		}
		lis = tls.NewListener(lis, tc)
	}
	go func() {
		if err := wsSrv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("websocket listener stopped", "error", err)
			stop()
		}
	}()

	// Link listener (backends dial it); the NATS link needs none.
	var gs *grpc.Server
	if grpcLink != nil {
		var err error
		if gs, err = startLinkListener(cfg, grpcLink, log, stop); err != nil {
			return err
		}
	}

	// Admin: metrics and probes.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	// Profiles for the performance work (the admin listener is internal).
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	ready := gw.ReadyHandler()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Ready means: the server wants connections (not draining, under the soft
		// cap) and a worker is attached to the default pool, so an accepted charger
		// can be served.
		if !link.Available("default") {
			http.Error(w, "no backend attached to the default pool", http.StatusServiceUnavailable)
			return
		}
		ready.ServeHTTP(w, r)
	})
	adminSrv := &http.Server{Addr: cfg.Admin, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("admin listener stopped", "error", err)
		}
	}()

	log.Info("gateway started", "id", cfg.ID, "version", cfg.Version, "ws", cfg.Listen, "link", cfg.Link, "link_addr", cfg.LinkAddr, "nats", cfg.NATSURL,
		"admin", cfg.Admin, "registry", cfg.RedisAddr != "")
	<-ctx.Done()

	// SIGTERM: fail readiness, close sessions at a paced rate (1012, idle first),
	// then stop. A second signal ends the wait.
	log.Info("draining", "window", cfg.DrainWindow)
	dctx, dcancel := context.WithTimeout(context.Background(), cfg.DrainWindow+30*time.Second)
	defer dcancel()
	second := make(chan os.Signal, 1)
	signal.Notify(second, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-second:
			dcancel()
		case <-dctx.Done():
		}
	}()
	if err := gw.Drain(dctx, csms.DrainOptions{Window: cfg.DrainWindow}); err != nil {
		log.Warn("drain ended with error", "error", err)
	}
	_ = gw.Shutdown(dctx)
	_ = wsSrv.Shutdown(dctx)
	if gs != nil {
		gs.GracefulStop()
	}
	_ = adminSrv.Shutdown(dctx)
	return nil
}

// poolCollector exports the attached backends per pool and version
// (gocpp_gateway_pool_members).
type poolCollector struct{ l gatewayLink }

var poolDesc = prometheus.NewDesc("gocpp_gateway_pool_members", "attached backends", []string{"pool", "backend_version"}, nil)

func (poolCollector) Describe(ch chan<- *prometheus.Desc) { ch <- poolDesc }

func (c poolCollector) Collect(ch chan<- prometheus.Metric) {
	for _, p := range c.l.Pools() {
		counts := map[string]int{}
		for _, m := range p.Members {
			counts[m.Version]++
		}
		if len(counts) == 0 {
			ch <- prometheus.MustNewConstMetric(poolDesc, prometheus.GaugeValue, 0, p.Name, "")
		}
		for v, n := range counts {
			ch <- prometheus.MustNewConstMetric(poolDesc, prometheus.GaugeValue, float64(n), p.Name, v)
		}
	}
}

// gatewayLink is what the binary needs from either link: the csms gateway
// server's link, plus the readiness and pool views.
type gatewayLink interface {
	gateway.GatewayLink
	Pools() []gateway.PoolStatus
	Available(pool string) bool
}

// startLinkListener serves the gRPC link on cfg.LinkAddr.
func startLinkListener(cfg config, link *linkgrpc.Gateway, log *slog.Logger, stop context.CancelFunc) (*grpc.Server, error) {
	llis, err := net.Listen("tcp", cfg.LinkAddr)
	if err != nil {
		return nil, err
	}
	gopts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: cfg.LinkKeepalive, Timeout: max(cfg.LinkKeepalive/2, time.Second)}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: time.Second, PermitWithoutStream: true}),
	}
	if cfg.LinkTLSCert != "" {
		cert, err := tls.LoadX509KeyPair(cfg.LinkTLSCert, cfg.LinkTLSKey)
		if err != nil {
			return nil, err
		}
		tc := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
		if cfg.LinkClientCA != "" {
			pool, err := loadPool(cfg.LinkClientCA)
			if err != nil {
				return nil, err
			}
			tc.ClientCAs, tc.ClientAuth = pool, tls.RequireAndVerifyClientCert
		}
		gopts = append(gopts, grpc.Creds(credentials.NewTLS(tc)))
	}
	gs := grpc.NewServer(gopts...)
	link.Register(gs)
	go func() {
		if err := gs.Serve(llis); err != nil {
			log.Error("link listener stopped", "error", err)
			stop()
		}
	}()
	return gs, nil
}
