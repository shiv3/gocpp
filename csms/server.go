// Package csms implements the OCPP Central System (CSMS / server) side.
package csms

import (
	"context"
	"encoding/base64"
	"errors"
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
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
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

	// edge is non-nil on a gateway, backend on a processing tier (see
	// gateway_edge.go and gateway_backend.go).
	edge    *edge
	backend *backend
	// split is the gateway half of a server created with WithSplitInProcess.
	split      *Server
	splitDrain func(context.Context) error // detaches the in-process backend

	// stopping is set when Shutdown or Close begins; new upgrades are refused
	// from then on.
	stopping atomic.Bool

	// draining is set by Drain: new upgrades are refused. drainRetryAfter is the
	// base of their Retry-After; acceptRejected and drainRejected count refusals.
	draining        atomic.Bool
	drainActive     atomic.Bool
	partialDrain    atomic.Bool
	softMax         atomic.Int64
	sessMetrics     observability.SessionMetrics
	drainSt         *drainState
	drainRetryAfter atomic.Int64
	acceptRejected  atomic.Uint64
	drainRejected   atomic.Uint64

	// tenants holds the per-tenant limits (WithTenantLimits); nil when unset.
	tenants *tenants
}

// NewServer creates a CSMS server.
func NewServer(opts ...Option) *Server {
	cfg := defaultServerConfig()
	for _, o := range opts {
		o.apply(&cfg)
	}
	if cfg.edgeLink != nil {
		applyEdgeConfig(&cfg)
	}
	if cfg.gatewayID != "" && cfg.sessionCfg.GatewayID == "" {
		cfg.sessionCfg.GatewayID = cfg.gatewayID
	}
	var splitHub SplitLink
	if cfg.link == nil {
		switch {
		case cfg.splitLink != nil:
			splitHub = cfg.splitLink
		case cfg.splitInProcess:
			splitHub = inproc.New()
		}
		if splitHub != nil {
			cfg.link = splitHub.Backend()
		}
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
		cfg:     cfg,
		reg:     dispatcher.NewHandlerRegistry(),
		conns:   make(map[string]*Conn),
		ctx:     ctx,
		cancel:  cancel,
		drainSt: &drainState{},
	}
	s.softMax.Store(int64(cfg.softMax))
	s.sessMetrics, _ = cfg.metrics.(observability.SessionMetrics)
	if cfg.tenantLimits != nil {
		s.tenants = newTenants(*cfg.tenantLimits, cfg.metrics)
	}
	if cfg.link == nil {
		// A processing tier holds no sockets, so it has nothing to claim or
		// reconcile; it only reads the registry (see lookupGateway).
		s.initSessions()
	}
	if cfg.edgeLink != nil {
		s.edge = newEdge(s, cfg.edgeLink, cfg.gatewayID)
		context.AfterFunc(ctx, s.edge.detach)
	}
	if cfg.link != nil {
		s.backend = newBackend(s, cfg.link)
	}
	if splitHub != nil {
		s.split = NewGatewayServer(splitHub.Gateway(), opts...)
		s.splitDrain = splitHub.AttachBackend(backendUpstream{s.backend}, s.info())
	}
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
	if s.split != nil {
		return s.split.Handler()
	}
	return http.HandlerFunc(s.serveWS)
}

// ListenAndServe starts an HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	var tenantRelease func() // the tenant session slot until it is handed to the Conn
	// Refuse before upgrading once the server is shutting down or closed, so a
	// charge point sees a plain HTTP failure it can retry elsewhere instead of a
	// connect that is closed immediately.
	if s.stopping.Load() || s.ctx.Err() != nil {
		w.Header().Set("Retry-After", "5")
		s.upgradeResult("shutting_down")
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	if s.draining.Load() {
		s.drainRejected.Add(1)
		w.Header().Set("Retry-After", jitterRetryAfter(time.Duration(s.drainRetryAfter.Load())))
		s.upgradeResult("draining")
		http.Error(w, "server draining", http.StatusServiceUnavailable)
		return
	}
	if lim := s.cfg.acceptLimit; lim != nil && !lim.allow(time.Now()) {
		s.acceptRejected.Add(1)
		w.Header().Set("Retry-After", jitterRetryAfter(max(lim.refillTime(), time.Second)))
		s.upgradeResult("rate_limited")
		http.Error(w, "too many connection attempts", http.StatusServiceUnavailable)
		return
	}
	cpID, ok := s.extractCPID(r)
	if !ok {
		s.upgradeResult("bad_request")
		http.Error(w, "invalid charge point id", http.StatusBadRequest)
		return
	}
	var edgeReply gateway.ConnectReply
	if s.edge != nil {
		// The processing tier decides the upgrade (authentication, admission,
		// connection timing).
		var status int
		var msg string
		edgeReply, status, msg = s.edge.connect(r, cpID)
		if status != 0 {
			if status == http.StatusServiceUnavailable {
				w.Header().Set("Retry-After", "5")
				s.upgradeResult("upstream_error")
			} else {
				s.upgradeResult("auth_rejected")
			}
			http.Error(w, msg, status)
			return
		}
		if edgeReply.CPID != "" {
			cpID = edgeReply.CPID
		}
		edgeReply.Attrs = s.edge.withExtAttrs(r.Context(), cpID, edgeReply.Attrs)
	} else {
		id, err := s.cfg.auth.Authenticate(r, cpID)
		if err != nil {
			s.upgradeResult("auth_rejected")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if id.CPID != "" {
			cpID = id.CPID
		}
	}
	if !validCPID(cpID) {
		s.upgradeResult("bad_request")
		http.Error(w, "invalid charge point id", http.StatusBadRequest)
		return
	}
	if s.cfg.duplicatePolicy == DuplicatePolicyRejectNew && s.hasConn(cpID) {
		s.upgradeResult("bad_request")
		http.Error(w, "duplicate charge point id", http.StatusConflict)
		return
	}

	if s.cfg.checkOrigin != nil && !s.cfg.checkOrigin(r) {
		s.upgradeResult("bad_request")
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	dcfg := s.cfg.dispatcher
	perConn := s.cfg.connConfig != nil
	if t := edgeReply.Timeouts; t != nil {
		perConn = true
		dcfg.CallTimeout, dcfg.WriteTimeout = t.CallTimeout, t.WriteTimeout
		dcfg.PingInterval, dcfg.PongWait, dcfg.ReadTimeout = t.PingInterval, t.PongWait, t.ReadTimeout
	} else if perConn {
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
		s.upgradeResult("full")
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	defer release()

	// Claim the session before accepting the upgrade, so the registry's epoch
	// order follows the order in which sockets were established. An upgrade that
	// cannot succeed (not a WebSocket request, no common subprotocol) claims
	// nothing; one that fails after the claim releases it.
	var sess storage.Session
	var prevSess *storage.Session
	claimed, registeredSession := false, false
	var attrs map[string]string
	if s.edge != nil {
		attrs = edgeReply.Attrs
	}
	if s.cfg.sessionAttrs != nil {
		attrs = s.cfg.sessionAttrs(r, cpID)
	}
	// Per-tenant admission: the tenant is known now (request, or the attributes the
	// processing tier returned), and the upgrade is still a plain HTTP request.
	var tenant *tenantState
	if s.tenants != nil {
		ts, rel, ok := s.tenants.admit(s.tenants.resolve(r, cpID, attrs), s.hasConn(cpID))
		if !ok {
			w.Header().Set("Retry-After", jitterRetryAfter(s.tenants.cfg.RetryAfter))
			s.upgradeResult("tenant_limit")
			http.Error(w, "tenant session limit reached", http.StatusServiceUnavailable)
			return
		}
		tenant, tenantRelease = ts, rel
		defer func() {
			if tenantRelease != nil { // not handed to the connection
				tenantRelease()
			}
		}()
	}
	abandon := func() {
		if claimed && !registeredSession {
			s.abandonClaim(cpID, sess, prevSess)
		}
	}
	// The legacy adapter (a ConnectionRegistry without sessions) has no epochs
	// to order and no way to restore what a claim overwrote, so it claims only
	// after the upgrade has succeeded, as before sessions existed.
	preClaim := s.sessions != nil && s.sessions.loops
	if preClaim && s.upgradeWillSucceed(r) {
		sess, prevSess, claimed = s.claimSession(r, cpID, attrs)
		defer abandon()
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
		s.upgradeResult("subprotocol")
		_ = c.Close(websocket.StatusProtocolError, "no common subprotocol")
		return
	}
	if s.sessions != nil && !preClaim {
		sess, prevSess, claimed = s.claimSession(r, cpID, attrs)
		defer abandon()
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

	meta := dispatcher.ConnMetadata{
		RemoteAddr:    r.RemoteAddr,
		RequestHeader: r.Header,
		TLS:           r.TLS,
	}
	var gws *gwSession
	if s.edge != nil {
		gws = s.edge.newSession(cpID, sess.Epoch)
		gws.version = ocppj.Version(strings.TrimPrefix(c.Subprotocol(), "ocpp"))
		gws.setAttrs(edgeReply.Attrs)
		meta.Owner = gws
	}
	dconn = dispatcher.NewConnShared(cpID, ws, dcfgp, s.reg, meta)
	if tenant != nil {
		if tc := s.tenants.newConn(tenant); tc != nil {
			dconn.SetInboundGate(tc.gate)
			dconn.SetInboundDone(tc.done)
			release := tenantRelease
			tenantRelease = func() { tc.close(); release() }
		}
	}
	switch {
	case claimed:
		dconn.SetAttachment(&sessionRef{s: sess, srv: s})
	case s.cfg.sessionAttrs != nil || edgeReply.Attrs != nil:
		// Not clustered (or unclaimed): attributes still travel with the connection.
		local := storage.Session{CPID: cpID, ConnectedAt: time.Now()}
		local.Meta, _ = storage.MergeAttrs(nil, attrs)
		dconn.SetAttachment(&sessionRef{s: local, srv: s})
	}
	conn := &Conn{inner: dconn, gone: make(chan struct{}), gw: gws, tenantRelease: tenantRelease, openedAt: time.Now()}
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
	s.upgradeResult("ok")
	s.handleConnected(conn)
	s.observeSession(conn, observability.SessionOpened, nil)
	if s.edge != nil {
		s.edge.opened(conn, r, edgeReply)
	}
	_ = s.cfg.connReg.PutLocal(s.ctx, cpID, dconn)
	dconn.ReleaseInbound()
	// From here the connection's lifetime is not tied to this HTTP handler:
	// it returns now, releasing its goroutine and the *http.Request, and the
	// teardown runs from a context.AfterFunc when the connection ends. Nothing in
	// gocpp derives from r.Context() (connections hang off s.ctx), so the
	// cancellation net/http applies on return is harmless. The cleanup closure
	// must not capture r or w.
	registered = true
	tenantRelease = nil // the connection owns the slot now
	context.AfterFunc(dconn.Context(), func() { s.connCleanup(cpID, conn) })
}

// connCleanup runs once, on its own goroutine, when conn's context is done. The
// order matters: the application hears about the disconnect, the connection is
// closed (waits for its goroutines), the registries forget it, and only then
// does gone release anyone waiting on the teardown (Conn.Close, Shutdown). The
// admission slot needs no release here: addConn consumed it at registration.
func (s *Server) connCleanup(cpID string, conn *Conn) {
	defer close(conn.gone)
	if conn.tenantRelease != nil {
		defer conn.tenantRelease()
	}
	dconn := conn.inner
	cause := context.Cause(dconn.Context())
	s.handleDisconnected(conn, cause)
	s.observeSession(conn, observability.SessionClosed, cause)
	if s.edge != nil {
		// Like the disconnect callback, before the close handshake, which can
		// take as long as an unresponsive peer likes.
		s.edge.closed(conn, cause)
	}
	_ = dconn.Close(nil)
	s.sessionClosed(dconn, cause)
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
// the request will pass websocket.Accept: a GET carrying the upgrade headers (a
// token "upgrade" in Connection, Upgrade: websocket, Sec-WebSocket-Version 13, a
// Sec-WebSocket-Key that decodes to 16 bytes) and offering a subprotocol this
// server speaks. A request that fails here claims nothing, so it cannot disturb
// the record of a connected charge point. A failure after the claim (the origin
// check, say) is undone by Server.abandonClaim.
func (s *Server) upgradeWillSucceed(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if !headerHasToken(r.Header, "Connection", "upgrade") || !headerHasToken(r.Header, "Upgrade", "websocket") {
		return false
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return false
	}
	if key, err := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key")); err != nil || len(key) != 16 {
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

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
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
		s.observeSession(old, observability.SessionClosed, ocppj.ErrConnReplaced)
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

// observeSession reports a session event of a connection this server terminates
// to WithSessionObserver. The closed event fires once even though a replaced
// connection is torn down by both the replacement and its own cleanup.
func (s *Server) observeSession(c *Conn, kind observability.SessionEventKind, cause error) {
	fn := s.cfg.sessionObs
	if fn == nil || c.inner == nil {
		return
	}
	emit := func() {
		defer func() {
			if r := recover(); r != nil {
				s.cfg.dispatcher.Logger.Error("csms session observer panic recovered",
					"cp_id", c.ID(), "panic", r)
			}
		}()
		sess, _ := c.Session()
		ev := observability.SessionEvent{
			Kind: kind, CPID: c.ID(), Version: c.Version(), RemoteAddr: c.RemoteAddr(),
			Epoch: sess.Epoch, Attrs: sess.Attrs(), Time: time.Now(),
		}
		if kind == observability.SessionClosed {
			if cause != nil && !errors.Is(cause, ocppj.ErrConnClosed) {
				ev.Cause = cause.Error()
			}
			if !c.openedAt.IsZero() {
				ev.Duration = ev.Time.Sub(c.openedAt)
			}
		}
		fn(s.ctx, ev)
	}
	if kind == observability.SessionOpened {
		emit()
		return
	}
	c.tapOnce.Do(emit)
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
//
// On a server created with WithSplitInProcess, whose gateway half lives in the
// same process, it returns a handle for the session (see Lookup).
func (s *Server) Get(id string) (*Conn, bool) {
	s.mu.RLock()
	c, ok := s.conns[id]
	s.mu.RUnlock()
	if !ok && s.split != nil {
		if h, err := s.lookupGateway(context.Background(), id); err == nil {
			return h, true
		}
	}
	return c, ok
}

// Connections returns the ids of the charge points currently connected to this
// instance, in no particular order.
func (s *Server) Connections() []string {
	if s.split != nil {
		return s.split.Connections()
	}
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
	if s.split != nil {
		s.split.Close()
	}
	s.stopping.Store(true)
	s.cancel()
	s.waitSplit()
}

// Shutdown gracefully closes all live charge-point connections with a normal
// WebSocket close and waits for them to drain, up to the deadline of ctx. If ctx
// is cancelled or times out first, remaining connections are force-closed and
// ctx.Err() is returned. From the moment Shutdown (or Close) is called, new
// upgrade attempts are refused with HTTP 503 Service Unavailable before the
// WebSocket handshake; the HTTP server hosting Handler() is still the caller's
// to stop.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.split != nil {
		err := s.split.Shutdown(ctx)
		s.stopping.Store(true)
		s.cancel()
		s.waitSplit()
		return err
	}
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

func (s *Server) upgradeResult(result string) {
	if s.sessMetrics != nil {
		s.sessMetrics.UpgradeResult(result)
	}
}

// sessionClosed reports a finished connection to SessionMetrics. The close code
// is the one the server sent for its own causes; a drop is reported as 1006.
func (s *Server) sessionClosed(c *dispatcher.Conn, cause error) {
	if s.sessMetrics == nil {
		return
	}
	code, label := "1006", "other"
	switch {
	case errors.Is(cause, ocppj.ErrGatewayDraining):
		code, label = "1012", "drain"
	case errors.Is(cause, ocppj.ErrDrainTimeout):
		code, label = "1012", "drain_timeout"
	case errors.Is(cause, ocppj.ErrConnReplaced):
		code, label = "1008", "replaced"
	case errors.Is(cause, ocppj.ErrConnClosedByServer):
		code, label = "1000", "server_close"
	case errors.Is(cause, ocppj.ErrConnDropped):
		label = "dropped"
	case cause == nil || errors.Is(cause, ocppj.ErrConnClosed):
		code, label = "1000", "other"
	}
	s.sessMetrics.SessionClosed(string(c.Version()), time.Since(c.Stats().ConnectedAt), code, label)
}

// waitSplit detaches the in-process backend of a split server, waiting briefly
// for the work in flight.
func (s *Server) waitSplit() {
	if s.splitDrain == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.splitDrain(ctx)
}
