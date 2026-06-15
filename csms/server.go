// Package csms implements the OCPP Central System (CSMS / server) side.
package csms

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
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
	cpID := strings.TrimPrefix(r.URL.Path, s.cfg.path)
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
	dconn := dispatcher.NewConn(cpID, ws, s.cfg.dispatcher, s.reg)
	conn := &Conn{inner: dconn}

	s.addConn(cpID, conn)
	defer s.removeConn(cpID)

	dconn.Start(s.ctx)
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
