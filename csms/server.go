// Package csms implements the OCPP Central System (CSMS / server) side.
package csms

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/transport"
	"golang.org/x/sync/semaphore"
)

// Server is an OCPP CSMS (central system).
type Server struct {
	cfg serverConfig
	reg *dispatcher.HandlerRegistry

	mu    sync.RWMutex
	conns map[string]*Conn

	ctx    context.Context
	cancel context.CancelFunc

	// stopping is set when Shutdown or Close begins; new upgrades are refused
	// from then on.
	stopping atomic.Bool
}

// NewServer creates a CSMS server.
func NewServer(opts ...Option) *Server {
	cfg := defaultServerConfig()
	for _, o := range opts {
		o.apply(&cfg)
	}
	if cfg.registry != nil && cfg.dispatcher.SchemaMode != dispatcher.SchemaModeOff {
		if cfg.dispatcher.SchemaMode == dispatcher.SchemaModeLenient {
			cfg.dispatcher.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
				v, ok := cfg.registry.Lookup(string(version), action, kind)
				if !ok {
					return payload, nil, nil
				}
				return v.ValidateLenientWith(payload, schema.LenientOptions{TimeLocation: cfg.lenientTimeLocation})
			}
		} else {
			cfg.dispatcher.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
				v, ok := cfg.registry.Lookup(string(version), action, kind)
				if !ok {
					return nil
				}
				return v.Validate(payload)
			}
		}
	}
	if cfg.globalConcurrencyLimit > 0 {
		// One limiter shared by every connection makes the cap server-wide.
		cfg.dispatcher.GlobalHandlerLimiter = semaphore.NewWeighted(int64(cfg.globalConcurrencyLimit))
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg:    cfg,
		reg:    dispatcher.NewHandlerRegistry(),
		conns:  make(map[string]*Conn),
		ctx:    ctx,
		cancel: cancel,
	}
	if s.clustered() {
		// Serve calls other instances forward to the charge points connected here,
		// until Shutdown or Close.
		go s.serveRemote()
	}
	return s
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
	// Refuse before upgrading once the server is shutting down or closed, so a
	// charge point sees a plain HTTP failure it can retry elsewhere instead of a
	// connect that is closed immediately.
	if s.stopping.Load() || s.ctx.Err() != nil {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	cpID, ok := s.extractCPID(r)
	if !ok {
		http.Error(w, "invalid charge point id", http.StatusBadRequest)
		return
	}
	id, err := s.cfg.auth.Authenticate(r, cpID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if id.CPID != "" {
		cpID = id.CPID
	}
	if !validCPID(cpID) {
		http.Error(w, "invalid charge point id", http.StatusBadRequest)
		return
	}
	if s.cfg.duplicatePolicy == DuplicatePolicyRejectNew && s.hasConn(cpID) {
		http.Error(w, "duplicate charge point id", http.StatusConflict)
		return
	}

	if s.cfg.checkOrigin != nil && !s.cfg.checkOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	dcfg := s.cfg.dispatcher
	if s.cfg.connConfig != nil {
		t := ConnTimeouts{
			CallTimeout:  dcfg.CallTimeout,
			WriteTimeout: dcfg.WriteTimeout,
			PingInterval: dcfg.PingInterval,
			PongWait:     dcfg.PongWait,
			ReadTimeout:  dcfg.ReadTimeout,
		}
		s.cfg.connConfig(r, cpID, &t)
		dcfg.CallTimeout, dcfg.WriteTimeout = t.CallTimeout, t.WriteTimeout
		dcfg.PingInterval, dcfg.PongWait, dcfg.ReadTimeout = t.PingInterval, t.PongWait, t.ReadTimeout
	}

	var dconn *dispatcher.Conn
	acceptOpts := &websocket.AcceptOptions{
		Subprotocols:   s.cfg.subProtocols,
		OriginPatterns: s.cfg.originPatterns,
		// A custom checkOrigin has already decided; let coder skip its own check.
		InsecureSkipVerify: s.cfg.insecureSkipVerifyOrigin || s.cfg.checkOrigin != nil,
		CompressionMode:    s.cfg.compressionMode.Coder(),
	}
	acceptOpts.OnPingReceived = func(context.Context, []byte) bool {
		if dconn != nil {
			dconn.NoteActivity()
		}
		return true
	}
	acceptOpts.OnPongReceived = func(context.Context, []byte) {
		if dconn != nil {
			dconn.NoteActivity()
		}
	}
	c, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		return
	}
	if c.Subprotocol() == "" {
		_ = c.Close(websocket.StatusProtocolError, "no common subprotocol")
		return
	}

	ws := transport.NewCoderWS(c)

	// Per-connection dispatcher config: version-bound metrics + tracer.
	dcfg.Metrics = dispatcher.MetricsHookFrom(s.cfg.metrics, c.Subprotocol())
	dcfg.Tracer = observability.NewTracer(s.cfg.tracerProvider)

	dconn = dispatcher.NewConn(cpID, ws, dcfg, s.reg, dispatcher.ConnMetadata{
		RemoteAddr:    r.RemoteAddr,
		RequestHeader: r.Header,
		TLS:           r.TLS,
	})
	conn := &Conn{inner: dconn, gone: make(chan struct{})}
	defer close(conn.gone)

	// Hold inbound CALL/SEND handling until the connection is published and
	// onConnect has returned, so a handler can rely on Get(id) and on the connect
	// callback having run.
	dconn.HoldInbound()

	// Start the connection (initializing its context and goroutines) BEFORE
	// publishing it in the registry, so a concurrent Get + Call cannot observe
	// a half-initialized Conn (data race on c.ctx).
	dconn.Start(s.ctx)
	if !s.addConn(cpID, conn) {
		_ = dconn.Close(nil)
		return
	}
	if s.stopping.Load() {
		// Shutdown began between the check above and registration; its snapshot
		// of live connections may have missed this one.
		dconn.Abort(ocppj.ErrConnClosed)
	}
	s.handleConnected(conn)
	_ = s.cfg.connReg.PutLocal(s.ctx, cpID, dconn)
	s.publishGlobal(cpID)
	dconn.ReleaseInbound()
	defer func() {
		if s.removeConn(cpID, conn) {
			_ = s.cfg.connReg.DeleteLocal(s.ctx, cpID)
			s.retractGlobal(cpID)
		}
	}()

	<-dconn.Context().Done()
	s.handleDisconnected(conn, context.Cause(dconn.Context()))
	_ = dconn.Close(nil)
}

func (s *Server) extractCPID(r *http.Request) (string, bool) {
	if s.cfg.cpIDExtractor != nil {
		cpID, ok := s.cfg.cpIDExtractor(r)
		return cpID, ok && validCPID(cpID)
	}
	cpID := strings.TrimPrefix(r.URL.Path, s.cfg.path)
	if cpID == r.URL.Path {
		return "", false
	}
	return cpID, validCPID(cpID)
}

func validCPID(cpID string) bool {
	return cpID != "" && !strings.Contains(cpID, "/")
}

func (s *Server) hasConn(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.conns[id]
	return ok
}

func (s *Server) addConn(id string, c *Conn) bool {
	policy := s.cfg.duplicatePolicy
	if policy != DuplicatePolicyRejectNew && policy != DuplicatePolicyCloseExisting {
		policy = DuplicatePolicyCloseExisting
	}
	s.mu.Lock()
	old, ok := s.conns[id]
	if ok && policy == DuplicatePolicyRejectNew {
		s.mu.Unlock()
		return false
	}
	s.conns[id] = c
	s.mu.Unlock()
	if ok {
		// The application must see the old connection go away before the new one
		// comes up, and with a cause that tells it apart from a real drop; otherwise
		// a handler that marks the charger offline on disconnect would mark the
		// freshly connected one offline. Abort is non-blocking (the old socket gets
		// a 1008 close frame from its closer goroutine); the rest of the teardown
		// runs in the background. The caller fires connect(new) after we return.
		old.inner.Abort(ocppj.ErrConnReplaced)
		s.handleDisconnected(old, ocppj.ErrConnReplaced)
		go func() { _ = old.inner.Close(nil) }()
	}
	return true
}

func (s *Server) removeConn(id string, c *Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[id] != c {
		return false
	}
	delete(s.conns, id)
	return true
}

func (s *Server) handleConnected(c *Conn) {
	if s.cfg.onConnect != nil {
		s.callHook("onConnect", c, func() { s.cfg.onConnect(c) })
	}
}

func (s *Server) handleDisconnected(c *Conn, err error) {
	if err == nil {
		err = ocppj.ErrConnClosed
	}
	if s.cfg.onDisconnect != nil {
		c.disconnectOnce.Do(func() {
			s.callHook("onDisconnect", c, func() { s.cfg.onDisconnect(c, err) })
		})
	}
}

// callHook runs an application lifecycle callback, recovering from a panic so a
// faulty hook cannot crash the process or skip the rest of the connection
// lifecycle (registry cleanup, close).
func (s *Server) callHook(name string, c *Conn, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			s.cfg.dispatcher.Logger.Error("csms lifecycle callback panic recovered",
				"cp_id", c.ID(), "callback", name, "panic", r)
		}
	}()
	fn()
}

// Get returns the live connection for a charge point, if connected.
func (s *Server) Get(id string) (*Conn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.conns[id]
	return c, ok
}

// Disconnect closes the connection of charge point id from the server side; see
// Conn.Close for the semantics. It returns ocppj.ErrNotConnected when no such
// charge point is connected.
func (s *Server) Disconnect(ctx context.Context, id string) error {
	c, ok := s.Get(id)
	if !ok {
		return ocppj.ErrNotConnected
	}
	return c.Close(ctx)
}

// Close shuts down the server and all connections immediately.
func (s *Server) Close() {
	s.stopping.Store(true)
	s.cancel()
}

// Shutdown gracefully closes all live charge-point connections with a normal
// WebSocket close and waits for them to drain, up to the deadline of ctx. If ctx
// is cancelled or times out first, remaining connections are force-closed and
// ctx.Err() is returned. From the moment Shutdown (or Close) is called, new
// upgrade attempts are refused with HTTP 503 Service Unavailable before the
// WebSocket handshake; the HTTP server hosting Handler() is still the caller's
// to stop.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopping.Store(true)
	s.mu.RLock()
	conns := make([]*Conn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.RUnlock()
	// Each Close may wait out a close handshake with an unresponsive peer, so
	// close in parallel rather than one after another.
	for _, c := range conns {
		go func() { _ = c.inner.Close(nil) }()
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.RLock()
		n := len(s.conns)
		s.mu.RUnlock()
		if n == 0 {
			s.cancel()
			return nil
		}
		select {
		case <-ctx.Done():
			s.cancel()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
