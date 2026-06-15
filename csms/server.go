// Package csms implements the OCPP Central System (CSMS / server) side.
package csms

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
)

// Server is an OCPP CSMS (central system).
type Server struct {
	cfg serverConfig
	reg *dispatcher.HandlerRegistry

	mu    sync.RWMutex
	conns map[string]*Conn

	ctx    context.Context
	cancel context.CancelFunc
}

// NewServer creates a CSMS server.
func NewServer(opts ...Option) *Server {
	cfg := defaultServerConfig()
	for _, o := range opts {
		o.apply(&cfg)
	}
	if cfg.registry != nil && cfg.strictSchema {
		cfg.dispatcher.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
			v, ok := cfg.registry.Lookup(string(version), action, kind)
			if !ok {
				return nil
			}
			return v.Validate(payload)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:    cfg,
		reg:    dispatcher.NewHandlerRegistry(),
		conns:  make(map[string]*Conn),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Handler returns the http.Handler that upgrades charge point connections.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveWS)
}

// ListenAndServe starts an HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	id, err := s.cfg.auth.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	cpID := id.CPID
	if cpID == "" {
		cpID = strings.TrimPrefix(r.URL.Path, s.cfg.path)
	}
	if cpID == "" || strings.Contains(cpID, "/") {
		http.Error(w, "invalid charge point id", http.StatusBadRequest)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: s.cfg.subProtocols,
	})
	if err != nil {
		return
	}
	if c.Subprotocol() == "" {
		c.Close(websocket.StatusProtocolError, "no common subprotocol")
		return
	}

	ws := transport.NewCoderWS(c)

	// Per-connection dispatcher config: version-bound metrics + tracer.
	dcfg := s.cfg.dispatcher
	dcfg.Metrics = dispatcher.MetricsHookFrom(s.cfg.metrics, c.Subprotocol())
	dcfg.Tracer = observability.NewTracer(s.cfg.tracerProvider)

	dconn := dispatcher.NewConn(cpID, ws, dcfg, s.reg)
	conn := &Conn{inner: dconn}

	// Start the connection (initializing its context and goroutines) BEFORE
	// publishing it in the registry, so a concurrent Get + Call cannot observe
	// a half-initialized Conn (data race on c.ctx).
	dconn.Start(s.ctx)
	s.addConn(cpID, conn)
	_ = s.cfg.connReg.PutLocal(s.ctx, cpID, dconn)
	defer func() {
		s.removeConn(cpID)
		_ = s.cfg.connReg.DeleteLocal(s.ctx, cpID)
	}()

	<-dconn.Context().Done()
	_ = dconn.Close(nil)
}

func (s *Server) addConn(id string, c *Conn) {
	s.mu.Lock()
	// Duplicate connection policy: close the old one (spec OQ-22 default).
	if old, ok := s.conns[id]; ok {
		go old.inner.Close(nil)
	}
	s.conns[id] = c
	s.mu.Unlock()
}

func (s *Server) removeConn(id string) {
	s.mu.Lock()
	delete(s.conns, id)
	s.mu.Unlock()
}

// Get returns the live connection for a charge point, if connected.
func (s *Server) Get(id string) (*Conn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.conns[id]
	return c, ok
}

// Close shuts down the server and all connections.
func (s *Server) Close() {
	s.cancel()
}
