// Package server is the serve mode of gocpp-csms: one OCPP listener for 1.6,
// 2.0.1 and 2.1 charge points (subprotocol negotiation), optional TLS, mTLS
// and Basic Auth, an HTTP control listener with /healthz, /readyz, /metrics,
// the generic /command and read-only /connections and /transactions, and a
// graceful drain on SIGTERM.
//
// Optional features plug in as Extensions (see extension.go).
package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/actions"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/authn"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/dispatch"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/recorder"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/txn"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/observability/metrics/prom"
	"github.com/shiv3/gocpp/csms"
)

// Server is a configured serve-mode CSMS.
type Server struct {
	cfg    *config.Config
	logger *slog.Logger
	rec    *recorder.Recorder
	out    *bufio.Writer
	hooks  *Hooks
	rt     *Runtime

	tlsConfig *tls.Config
	ocppLn    net.Listener
	ctlLn     net.Listener
	ocppHTTP  *http.Server
	ctlHTTP   *http.Server
	errCh     chan error

	runCtx    context.Context
	cancelRun context.CancelFunc
	shutdown  sync.Once
}

// New builds a server from cfg. stdout receives the recorder lines and the
// ports sentinel when enabled (nil = os.Stdout).
func New(cfg *config.Config, logger *slog.Logger, stdout io.Writer, exts ...Extension) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	s := &Server{cfg: cfg, logger: logger, errCh: make(chan error, 2)}
	if cfg.Record || cfg.Announce {
		s.out = bufio.NewWriter(stdout)
		s.rec = recorder.New(s.out)
	}

	schemas, err := actions.Schemas()
	if err != nil {
		return nil, fmt.Errorf("load schemas: %w", err)
	}
	promReg := prometheus.NewRegistry()
	promReg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	txns := txn.New()
	var rec *recorder.Recorder
	if cfg.Record {
		rec = s.rec
	}
	in := inbound.New(inbound.Options{Txns: txns, Recorder: rec, BootInterval: cfg.BootInterval, Logger: logger})

	h := &Hooks{Config: cfg, Logger: logger, Inbound: in, Txns: txns, Recorder: rec, Metrics: promReg}
	if h.authenticator, err = defaultAuthenticator(cfg); err != nil {
		return nil, err
	}
	for _, ext := range exts {
		if err := ext.Setup(h); err != nil {
			return nil, fmt.Errorf("extension %s: %w", ext.Name(), err)
		}
	}
	s.hooks = h

	if cfg.TLSCert != "" {
		if s.tlsConfig, err = loadTLS(cfg); err != nil {
			return nil, err
		}
	}

	opts := []csms.Option{
		csms.WithSubProtocols(cfg.Subprotocols()...),
		csms.WithPath(cfg.PathPrefix),
		csms.WithCPIDExtractor(lastSegmentExtractor(cfg.PathPrefix)),
		csms.WithLogger(logger),
		csms.WithAuthenticator(h.authenticator),
		csms.WithMetrics(prom.New(promReg)),
		csms.WithMaxConnections(cfg.MaxConnections),
		csms.WithCallTimeout(cfg.CommandTimeout),
		csms.WithSchemaRegistry(schemas),
		csms.WithOnConnect(s.onConnect),
		csms.WithOnDisconnect(s.onDisconnect),
	}
	opts = append(opts, schemaOption(cfg.Schema))
	if cfg.InboundConcurrency > 0 {
		opts = append(opts, csms.WithGlobalConcurrencyLimit(cfg.InboundConcurrency))
	}
	if h.cpidExtractor != nil {
		opts = append(opts, csms.WithCPIDExtractor(h.cpidExtractor))
	}
	if obs := h.frameObservers; len(obs) > 0 {
		opts = append(opts, csms.WithFrameObserver(func(ctx context.Context, ev observability.FrameEvent) {
			for _, o := range obs {
				o(ctx, ev)
			}
		}))
	}
	opts = append(opts, h.options...)
	srv := csms.NewServer(opts...)
	if err := in.Register(srv); err != nil {
		return nil, fmt.Errorf("register inbound handlers: %w", err)
	}

	s.rt = &Runtime{
		Config:     cfg,
		Logger:     logger,
		CSMS:       srv,
		Dispatcher: dispatch.New(srv.Get, schemas, cfg.CommandTimeout),
		Inbound:    in,
		Txns:       txns,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if httpapi.AllowMethods(w, r, http.MethodGet) {
			httpapi.WriteJSON(w, http.StatusOK, httpapi.Response{OK: true})
		}
	})
	mux.Handle("/readyz", srv.ReadyHandler())
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))
	mux.Handle("/command", s.rt.Dispatcher.Handler())
	mux.HandleFunc("/connections", s.connections)
	mux.HandleFunc("/transactions", s.transactions)
	for _, r := range h.routes {
		mux.Handle(r.pattern, r.handler)
	}

	s.ocppHTTP = &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn)}
	s.ctlHTTP = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn)}
	return s, nil
}

// Runtime returns the running parts (valid after New).
func (s *Server) Runtime() *Runtime { return s.rt }

// Start binds both listeners, starts serving and runs the OnStart hooks.
func (s *Server) Start() error {
	var err error
	if s.ocppLn, err = net.Listen("tcp", s.cfg.OCPPAddr); err != nil {
		return fmt.Errorf("bind OCPP listener: %w", err)
	}
	if s.ctlLn, err = net.Listen("tcp", s.cfg.ControlAddr); err != nil {
		_ = s.ocppLn.Close()
		return fmt.Errorf("bind control listener: %w", err)
	}
	ocppLn := s.ocppLn
	if s.tlsConfig != nil {
		ocppLn = tls.NewListener(s.ocppLn, s.tlsConfig)
	}
	s.runCtx, s.cancelRun = context.WithCancel(context.Background())
	go s.serve("ocpp", s.ocppHTTP, ocppLn)
	go s.serve("control", s.ctlHTTP, s.ctlLn)

	if s.cfg.Announce {
		if err := s.rec.Announce(port(s.ocppLn), port(s.ctlLn)); err != nil {
			return fmt.Errorf("write ports sentinel: %w", err)
		}
	}
	for _, fn := range s.hooks.onStart {
		if err := fn(s.runCtx, s.rt); err != nil {
			return err
		}
	}
	scheme := "ws"
	if s.tlsConfig != nil {
		scheme = "wss"
	}
	s.logger.Info("gocpp-csms serving",
		"ocpp_url", fmt.Sprintf("%s://%s%s<cpId>", scheme, s.ocppLn.Addr(), s.cfg.PathPrefix),
		"control_addr", s.ctlLn.Addr().String(),
		"versions", strings.Join(s.cfg.Versions, ","),
		"tls", s.tlsConfig != nil,
		"mtls", s.cfg.TLSClientCA != "",
		"basic_auth", s.cfg.BasicAuthFile != "",
	)
	return nil
}

// OCPPAddr is the bound OCPP listener address (after Start).
func (s *Server) OCPPAddr() net.Addr { return s.ocppLn.Addr() }

// ControlAddr is the bound control listener address (after Start).
func (s *Server) ControlAddr() net.Addr { return s.ctlLn.Addr() }

// Err delivers a listener that stopped unexpectedly.
func (s *Server) Err() <-chan error { return s.errCh }

func (s *Server) serve(name string, srv *http.Server, ln net.Listener) {
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.errCh <- fmt.Errorf("%s server: %w", name, err)
	}
}

// Shutdown drains the charge point connections (up to --drain-timeout:
// new upgrades are refused, /readyz fails, sessions are closed with 1012),
// then stops both listeners and flushes stdout. ctx bounds the whole.
func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error
	s.shutdown.Do(func() {
		srv := s.rt.CSMS
		if s.cfg.DrainTimeout > 0 && len(srv.Connections()) > 0 {
			dctx, cancel := context.WithTimeout(ctx, s.cfg.DrainTimeout+5*time.Second)
			if err := srv.Drain(dctx, csms.DrainOptions{Window: s.cfg.DrainTimeout}); err != nil {
				s.logger.Warn("drain", "error", err)
			}
			cancel()
		}
		if s.cancelRun != nil {
			s.cancelRun()
		}
		if err := s.ctlHTTP.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errs = append(errs, fmt.Errorf("shutdown control server: %w", err))
		}
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errs = append(errs, fmt.Errorf("shutdown CSMS: %w", err))
		}
		if err := s.ocppHTTP.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errs = append(errs, fmt.Errorf("shutdown OCPP server: %w", err))
		}
		if s.rec != nil {
			if err := s.rec.Flush(); err != nil {
				errs = append(errs, fmt.Errorf("flush stdout: %w", err))
			}
		}
	})
	return errors.Join(errs...)
}

func (s *Server) onConnect(c *csms.Conn) {
	s.logger.Info("charge point connected", "cp_id", c.ID(), "version", string(c.Version()), "remote_addr", c.RemoteAddr())
	for _, fn := range s.hooks.onConnect {
		fn(c)
	}
}

func (s *Server) onDisconnect(c *csms.Conn, err error) {
	s.logger.Info("charge point disconnected", "cp_id", c.ID(), "version", string(c.Version()), "cause", errString(err))
	for _, fn := range s.hooks.onDisconnect {
		fn(c, err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type connectionInfo struct {
	CPID        string `json:"cpId"`
	Version     string `json:"version"`
	Subprotocol string `json:"subprotocol"`
	RemoteAddr  string `json:"remoteAddr"`
}

// connections serves GET /connections: the connected charge points.
func (s *Server) connections(w http.ResponseWriter, r *http.Request) {
	if !httpapi.AllowMethods(w, r, http.MethodGet) {
		return
	}
	out := []connectionInfo{}
	for _, id := range s.rt.CSMS.Connections() {
		c, ok := s.rt.CSMS.Get(id)
		if !ok {
			continue
		}
		out = append(out, connectionInfo{CPID: id, Version: string(c.Version()), Subprotocol: c.Subprotocol(), RemoteAddr: c.RemoteAddr()})
	}
	httpapi.WriteResult(w, out)
}

// transactions serves GET /transactions[?cpId=X][&active=true].
func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	if !httpapi.AllowMethods(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query()
	f := txn.Filter{CPID: q.Get("cpId")}
	switch q.Get("active") {
	case "", "false":
	case "true":
		f.ActiveOnly = true
	default:
		httpapi.WriteError(w, http.StatusBadRequest, `active must be "true" or "false"`)
		return
	}
	httpapi.WriteResult(w, s.rt.Txns.List(f))
}

func port(ln net.Listener) int {
	if a, ok := ln.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

// lastSegmentExtractor accepts any path under prefix and takes the charge
// point id from its last segment: /ocpp/CP1 and /ocpp/site-a/CP1 are both CP1.
func lastSegmentExtractor(prefix string) csms.CPIDExtractor {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return func(r *http.Request) (string, bool) {
		rest, ok := strings.CutPrefix(r.URL.Path, prefix)
		if !ok || rest == "" || strings.HasSuffix(rest, "/") {
			return "", false
		}
		if i := strings.LastIndexByte(rest, '/'); i >= 0 {
			rest = rest[i+1:]
		}
		return rest, rest != ""
	}
}

func schemaOption(mode string) csms.Option {
	switch mode {
	case "tolerant":
		return csms.WithTolerantSchema()
	case "lenient":
		return csms.WithLenientSchema()
	case "off":
		return csms.WithStrictSchema(false)
	default:
		return csms.WithStrictSchema(true)
	}
}

func defaultAuthenticator(cfg *config.Config) (auth.Authenticator, error) {
	var as []auth.Authenticator
	if cfg.BasicAuthFile != "" {
		creds, err := authn.LoadFile(cfg.BasicAuthFile)
		if err != nil {
			return nil, fmt.Errorf("--basic-auth-file: %w", err)
		}
		as = append(as, authn.Basic(creds))
	}
	if cfg.MTLSMatchCPID {
		as = append(as, authn.CertMatchesCPID())
	}
	return authn.All(as...), nil
}

func loadTLS(cfg *config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair: %w", err)
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if cfg.TLSClientCA != "" {
		pem, err := os.ReadFile(cfg.TLSClientCA)
		if err != nil {
			return nil, fmt.Errorf("--tls-client-ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("--tls-client-ca %s: no PEM certificates", cfg.TLSClientCA)
		}
		tc.ClientCAs = pool
		tc.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return tc, nil
}
