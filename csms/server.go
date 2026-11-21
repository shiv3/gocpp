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
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/transport"
	"golang.org/x/sync/semaphore"
)

// Server is an OCPP CSMS (central system).
type Server struct {
	cfg serverConfig
	reg *dispatcher.HandlerRegistry
	// handlers mirrors reg with handlers typed on *Conn, for dispatch that does
	// not start from a dispatcher.Conn.
	handlers handlerTable
	// verCfg caches the shared dispatcher.Config per subprotocol.
	verCfg sync.Map

	mu    sync.RWMutex
	conns map[string]*Conn
	// reserved counts upgrades admitted under WithMaxConnections that have not
	// yet registered; guarded by mu together with conns.
	reserved int

	// sessions is nil unless the server is clustered (WithInstanceID or
	// WithSessionRegistry).
	sessions *sessionManager

	ctx    context.Context
	cancel context.CancelFunc

	// stopping is set when Shutdown or Close begins; new upgrades are refused
	// from then on.
	stopping atomic.Bool

	// draining is set by Drain: new upgrades are refused. drainRetryAfter is the
	// base of their Retry-After; acceptRejected and drainRejected count refusals.
	draining        atomic.Bool
	drainRetryAfter atomic.Int64
	acceptRejected  atomic.Uint64
	drainRejected   atomic.Uint64
}

// NewServer creates a CSMS server.
func NewServer(opts ...Option) *Server {
	cfg := defaultServerConfig()
	for _, o := range opts {
		o.apply(&cfg)
	}
	if cfg.registry != nil && !cfg.timeEncoding.IsZero() {
		cfg.dispatcher.OutboundTransform = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, error) {
			v, ok := cfg.registry.Lookup(string(version), action, kind)
			if !ok {
				return payload, nil
			}
			return v.EncodeTimes(payload, cfg.timeEncoding)
		}
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
	s.initSessions()
	if s.clustered() {
		go s.sessions.run(ctx)
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
	if s.draining.Load() {
		s.drainRejected.Add(1)
		w.Header().Set("Retry-After", jitterRetryAfter(time.Duration(s.drainRetryAfter.Load())))
		http.Error(w, "server draining", http.StatusServiceUnavailable)
		return
	}
	if lim := s.cfg.acceptLimit; lim != nil && !lim.allow(time.Now()) {
		s.acceptRejected.Add(1)
		w.Header().Set("Retry-After", jitterRetryAfter(max(lim.refillTime(), time.Second)))
		http.Error(w, "too many connection attempts", http.StatusServiceUnavailable)
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
	perConn := s.cfg.connConfig != nil
	if perConn {
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

	res, release, ok := s.reserveSlot(cpID)
	if !ok {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	defer release()

	// Claim the session before accepting the upgrade, so the registry's epoch
	// order follows the order in which sockets were established. An upgrade that
	// cannot succeed (not a WebSocket request, no common subprotocol) claims
	// nothing; one that fails after the claim releases it.
	var sess storage.Session
	claimed, registeredSession := false, false
	if s.sessions != nil && s.upgradeWillSucceed(r) {
		sess, claimed = s.claimSession(r, cpID)
		defer func() {
			if claimed && !registeredSession {
				s.releaseSession(sess)
			}
		}()
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
	c, err := websocket.Accept(newSmallBufWriter(w, s.cfg.ioReadBuffer, s.cfg.ioWriteBuffer), r, acceptOpts)
	if err != nil {
		return
	}
	if c.Subprotocol() == "" {
		_ = c.Close(websocket.StatusProtocolError, "no common subprotocol")
		return
	}

	ws := transport.NewCoderWSWithLimit(c, dcfg.MaxMessageSize)

	// Dispatcher config with version-bound metrics + tracer. Without a per-
	// connection override hook it is shared by every connection of a version.
	var dcfgp *dispatcher.Config
	if perConn {
		dcfg.Metrics = dispatcher.MetricsHookFrom(s.cfg.metrics, c.Subprotocol())
		dcfg.Tracer = observability.NewTracer(s.cfg.tracerProvider)
		dcfgp = &dcfg
	} else {
		dcfgp = s.versionConfig(c.Subprotocol())
	}

	dconn = dispatcher.NewConnShared(cpID, ws, dcfgp, s.reg, dispatcher.ConnMetadata{
		RemoteAddr:    r.RemoteAddr,
		RequestHeader: r.Header,
		TLS:           r.TLS,
	})
	if claimed {
		dconn.SetAttachment(&sess)
	}
	conn := &Conn{inner: dconn, gone: make(chan struct{})}
	// gone is closed by connCleanup once the connection is registered; every
	// earlier return closes it here.
	registered := false
	defer func() {
		if !registered {
			close(conn.gone)
		}
	}()

	// Hold inbound CALL/SEND handling until the connection is published and
	// onConnect has returned, so a handler can rely on Get(id) and on the connect
	// callback having run.
	dconn.HoldInbound()

	// Start the connection (initializing its context and goroutines) BEFORE
	// publishing it in the registry, so a concurrent Get + Call cannot observe
	// a half-initialized Conn (data race on c.ctx).
	dconn.Start(s.ctx)
	if !s.addConn(cpID, conn, res) {
		_ = dconn.Close(nil)
		return
	}
	if s.stopping.Load() {
		// Shutdown began between the check above and registration; its snapshot
		// of live connections may have missed this one.
		dconn.Abort(ocppj.ErrConnClosed)
	}
	registeredSession = true
	s.handleConnected(conn)
	_ = s.cfg.connReg.PutLocal(s.ctx, cpID, dconn)
	dconn.ReleaseInbound()
	// From here the connection's lifetime is not tied to this HTTP handler:
	// it returns now, releasing its goroutine and the *http.Request, and the
	// teardown runs from a context.AfterFunc when the connection ends. Nothing in
	// gocpp derives from r.Context() (connections hang off s.ctx), so the
	// cancellation net/http applies on return is harmless. The cleanup closure
	// must not capture r or w.
	registered = true
	context.AfterFunc(dconn.Context(), func() { s.connCleanup(cpID, conn) })
}

// connCleanup runs once, on its own goroutine, when conn's context is done. The
// order matters: the application hears about the disconnect, the connection is
// closed (waits for its goroutines), the registries forget it, and only then
// does gone release anyone waiting on the teardown (Conn.Close, Shutdown). The
// admission slot needs no release here: addConn consumed it at registration.
func (s *Server) connCleanup(cpID string, conn *Conn) {
	defer close(conn.gone)
	dconn := conn.inner
	s.handleDisconnected(conn, context.Cause(dconn.Context()))
	_ = dconn.Close(nil)
	if s.removeConn(cpID, conn) {
		_ = s.cfg.connReg.DeleteLocal(s.ctx, cpID)
		// Compare-and-delete on the epoch: a newer connection of the same id,
		// here or on another instance, keeps its record.
		s.releaseSession(conn.holderSession())
	}
}

// versionConfig returns the dispatcher config shared by connections that
// negotiated subprotocol sub (metrics and tracer are bound to it). It is built
// once and never modified.
func (s *Server) versionConfig(sub string) *dispatcher.Config {
	if v, ok := s.verCfg.Load(sub); ok {
		return v.(*dispatcher.Config)
	}
	d := s.cfg.dispatcher
	d.Metrics = dispatcher.MetricsHookFrom(s.cfg.metrics, sub)
	d.Tracer = observability.NewTracer(s.cfg.tracerProvider)
	v, _ := s.verCfg.LoadOrStore(sub, &d)
	return v.(*dispatcher.Config)
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

// upgradeWillSucceed is a cheap pre-check, run before a session is claimed, that
// the request is a WebSocket upgrade offering a subprotocol this server speaks.
func (s *Server) upgradeWillSucceed(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, h := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(h, ",") {
			p = strings.TrimSpace(p)
			for _, ours := range s.cfg.subProtocols {
				if p == ours {
					return true
				}
			}
		}
	}
	return false
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

// slotReservation is a connection slot held between admission and registration.
// held is guarded by Server.mu.
type slotReservation struct{ held bool }

// reserveSlot admits an upgrade under WithMaxConnections. The check and the
// reservation happen under one lock, so concurrent upgrades cannot overshoot. An
// id that is already connected is a replacement and takes no slot: the old
// connection leaves as the new one arrives. addConn consumes the reservation
// under the same lock that registers the connection; release returns it if the
// upgrade fails earlier, and is idempotent.
func (s *Server) reserveSlot(id string) (res *slotReservation, release func(), ok bool) {
	res = &slotReservation{}
	release = func() {
		s.mu.Lock()
		if res.held {
			res.held = false
			s.reserved--
		}
		s.mu.Unlock()
	}
	if s.cfg.maxConnections <= 0 {
		return res, release, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, replacing := s.conns[id]; replacing {
		return res, release, true
	}
	if len(s.conns)+s.reserved >= s.cfg.maxConnections {
		return nil, nil, false
	}
	s.reserved++
	res.held = true
	return res, release, true
}

// addConn registers c. res is the admission reservation (nil in unit tests).
func (s *Server) addConn(id string, c *Conn, res ...*slotReservation) bool {
	var held *slotReservation
	if len(res) > 0 {
		held = res[0]
	}
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
	hasSlot := held != nil && held.held
	if !ok && s.cfg.maxConnections > 0 && held != nil && !hasSlot &&
		len(s.conns)+s.reserved >= s.cfg.maxConnections {
		// Admitted as a replacement, but the old connection is gone and the
		// server filled up meanwhile.
		s.mu.Unlock()
		return false
	}
	s.conns[id] = c
	if hasSlot {
		held.held = false
		s.reserved--
	}
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

// Connections returns the ids of the charge points currently connected to this
// instance, in no particular order.
func (s *Server) Connections() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	return ids
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
