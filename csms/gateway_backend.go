package csms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
)

// WithGatewayLink makes the server the processing tier of a split deployment:
// instead of terminating WebSockets it receives inbound messages from gateways
// through link (see ServeGateway) and reaches chargers through the same link.
// Handlers (On, OnSend, the generated registrations), schema options,
// WithAuthenticator, WithConnConfig, WithOnConnect and WithOnDisconnect keep
// their meaning; WithOnConnect/WithOnDisconnect are driven by session events
// and are delivered at least once, so they should be idempotent per session
// (Conn.GatewaySession().Epoch identifies it).
//
// On such a server Get and Connections only see nothing (it holds no sockets);
// use Lookup, which finds a charger through the link. See docs/gateway.md.
//
// Experimental: this API may change before v1.0.
func WithGatewayLink(link gateway.BackendLink) Option {
	return optionFunc(func(c *serverConfig) { c.link = link })
}

// WithOnOrphanResult registers a callback for the answer to a CSMS call whose
// caller had gone away by the time the charger replied (a backend that crashed
// or timed out after the command was written). err is the CALLERROR when the
// charger answered with one.
func WithOnOrphanResult(fn func(cpID, callID, action string, payload []byte, err error)) Option {
	return optionFunc(func(c *serverConfig) { c.onOrphan = fn })
}

// backend is the processing-tier state of a Server.
type backend struct {
	s    *Server
	link gateway.BackendLink

	mu       sync.Mutex
	sessions map[string]*gwSession // newest session seen per charge point
	// seen is when each cached session was last used or announced, to drop
	// entries nothing has touched for cacheIdle: a SessionClosed that went to
	// another backend would otherwise leave this backend's entry forever (bounded
	// by the number of charge point ids, but not by the connected ones).
	seen      map[string]time.Time
	lastSweep time.Time
	cacheIdle time.Duration

	// events serializes the session events of one charge point: observing a
	// session and running the application's connect/disconnect hooks for it must
	// not interleave with the same for another session of the same charger, or
	// the old session's disconnect could run after the new one's connect. The lock
	// is per charger, so a hook that blocks (an OnConnect that makes a Call, say)
	// delays only that charger's next event, never another charger's.
	eventsMu sync.Mutex
	events   map[string]*cpLock
}

// cpLock is a refcounted mutex, dropped from the map when nobody holds or waits
// for it.
type cpLock struct {
	mu   sync.Mutex
	refs int
}

// lockEvents takes the event lock of cpID and returns its release.
func (b *backend) lockEvents(cpID string) (unlock func()) {
	b.eventsMu.Lock()
	l := b.events[cpID]
	if l == nil {
		l = &cpLock{}
		b.events[cpID] = l
	}
	l.refs++
	b.eventsMu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		b.eventsMu.Lock()
		if l.refs--; l.refs == 0 {
			delete(b.events, cpID)
		}
		b.eventsMu.Unlock()
	}
}

// sessionCacheIdle is how long a cached session may go unused before the
// backend forgets it. A session that is still connected is simply cached again
// by its next message.
const sessionCacheIdle = time.Hour

func newBackend(s *Server, link gateway.BackendLink) *backend {
	return &backend{s: s, link: link, sessions: make(map[string]*gwSession), seen: make(map[string]time.Time), cacheIdle: sessionCacheIdle, events: make(map[string]*cpLock)}
}

// Upstream returns the server's gateway.Upstream: what a link delivers the
// messages of gateways to. ServeGateway uses it; a custom link, or a test that
// wires a Hub by hand, can too. It is nil on a server without WithGatewayLink.
func (s *Server) Upstream() gateway.Upstream {
	if s.backend == nil {
		return nil
	}
	return backendUpstream{s.backend}
}

// ErrNoGatewayLink is returned by ServeGateway on a server created without
// WithGatewayLink.
var ErrNoGatewayLink = errors.New("csms: server has no gateway link (WithGatewayLink)")

// ServeGateway serves the gateways attached through the link until ctx ends or
// the server is closed, like http.Serve. When it ends the backend drains: it
// takes no new work and returns once the messages in flight have been answered.
func (s *Server) ServeGateway(ctx context.Context) error {
	if s.backend == nil {
		return ErrNoGatewayLink
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	return s.backend.link.Serve(ctx, backendUpstream{s.backend}, s.info())
}

// observe records the newest session seen per charge point. It returns the
// session to use and, when ref displaces an older one, that older one. stale
// reports that ref is an older session than the cached one (a late message of a
// session that has already been replaced): it is returned for use by the message
// at hand but not cached, and must not be announced as a connect.
func (b *backend) observe(ref gateway.SessionRef, version ocppj.Version, info *gateway.SessionInfo) (cur, displaced *gwSession, stale bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.sweepLocked(now)
	b.seen[ref.CPID] = now
	old := b.sessions[ref.CPID]
	if old != nil {
		switch {
		case old.ref == ref:
			if info != nil && old.info() == nil {
				n := newGwSession(old.ref, old.version, info)
				n.setAttrs(old.attrs())
				b.sessions[ref.CPID] = n
				return n, nil, false
			}
			return old, nil, false
		case b.newerSession(old.ref, ref):
			return newGwSession(ref, version, info), nil, true
		}
	}
	cur = newGwSession(ref, version, info)
	b.sessions[ref.CPID] = cur
	return cur, old, false
}

// observeChecked is observe for an event or message of a live session. When
// observe calls ref stale (an older epoch than the cached one) and a session
// registry exists, the registry has the last word: if it holds ref as the
// current session, the cached higher epoch is the leftover of a registry that
// lost its data and restarted its epoch counters, and ref replaces it.
func (b *backend) observeChecked(ctx context.Context, ref gateway.SessionRef, version ocppj.Version, info *gateway.SessionInfo) (cur, displaced *gwSession, stale bool) {
	cur, displaced, stale = b.observe(ref, version, info)
	reg := b.s.cfg.sessionReg
	if !stale || reg == nil {
		return
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rec, found, err := reg.Lookup(lctx, ref.CPID)
	if err != nil || !found || fromStorage(rec).ref != ref {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.sessions[ref.CPID]
	if old != nil && old.ref == ref {
		return old, nil, false
	}
	cur = newGwSession(ref, version, info)
	b.sessions[ref.CPID] = cur
	b.seen[ref.CPID] = time.Now()
	return cur, old, false
}

// sweepLocked drops sessions unused for cacheIdle, at most once per minute (or
// per tenth of cacheIdle when that is shorter).
func (b *backend) sweepLocked(now time.Time) {
	every := min(time.Minute, b.cacheIdle/10)
	if now.Sub(b.lastSweep) < every {
		return
	}
	b.lastSweep = now
	for id, t := range b.seen {
		if now.Sub(t) >= b.cacheIdle {
			delete(b.seen, id)
			delete(b.sessions, id)
		}
	}
}

// newerSession reports whether a is a newer session of the same charger than
// b. Epochs order sessions of one gateway; with a session registry they are
// assigned by it and order sessions across gateways as well. Without one
// (epochs numbered per gateway) sessions of different gateways cannot be
// compared and the one seen last wins.
func (b *backend) newerSession(a, other gateway.SessionRef) bool {
	if a.Epoch <= other.Epoch {
		return false
	}
	if sameSessionOwner(a, other) {
		return true
	}
	return b.s.cfg.sessionReg != nil && other.Epoch != 0
}

// forget handles the close of session ref. It drops the cached session when it
// is ref, and reports whether the application should hear about the
// disconnect. It should unless a newer session of the same charger is known (a
// late close of a replaced session, already reported when it was replaced).
//
// Nothing cached is not proof that no newer session exists: the close may have
// been routed to a different backend than the open, and that backend must still
// report it, or the disconnect is lost. But the same backend may also have
// missed the newer session's open, and then reporting the old session's close
// after the new session's connect (on another backend, say) would tell the
// application the charger is gone while it is connected. So with a session
// registry the registry decides: a record of a newer (higher epoch) or
// different session suppresses the event. Without a registry such a close
// cannot be told from a legitimate one and fires; the application must then
// ignore a disconnect whose epoch is older than its current session's
// (Conn.GatewaySession().Epoch). See docs/gateway.md.
func (b *backend) forget(ctx context.Context, ref gateway.SessionRef) (fire bool) {
	cur, fire := b.uncache(ref)
	if fire {
		return true
	}
	reg := b.s.cfg.sessionReg
	if reg == nil {
		return cur == nil
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rec, found, err := reg.Lookup(lctx, ref.CPID)
	if err != nil || !found {
		// The registry knows no session of this charger. A cached older one that
		// is not ref cannot be ref's successor either way; keep the decision the
		// cache made.
		return cur == nil
	}
	got := fromStorage(rec).ref
	switch {
	case got == ref:
		// The registry says ref is the current session (the cache held an older
		// one, or an epoch counter that restarted): trust the registry.
		b.dropIfCached(cur)
		return true
	case got.Epoch >= ref.Epoch:
		return false // a newer (or different) session is current
	}
	// The registry still shows something older than ref (it lags): ref's close
	// is the news.
	return cur == nil
}

// uncache drops the cached session of ref's charger when ref is it or is newer
// than it, and returns what was cached (nil for nothing). fire is true when the
// close is to be reported without further checks.
func (b *backend) uncache(ref gateway.SessionRef) (cur *gwSession, fire bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur = b.sessions[ref.CPID]
	switch {
	case cur == nil:
		return nil, false
	case cur.ref == ref, b.newerSession(ref, cur.ref):
		// Either ref is the cached session, or the cache holds an older session
		// of this charger whose own close went elsewhere; ref supersedes it.
		delete(b.sessions, ref.CPID)
		delete(b.seen, ref.CPID)
		return cur, true
	}
	return cur, false
}

// dropIfCached removes cur from the cache if it is still what is cached.
func (b *backend) dropIfCached(cur *gwSession) {
	if cur == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions[cur.ref.CPID] == cur {
		delete(b.sessions, cur.ref.CPID)
		delete(b.seen, cur.ref.CPID)
	}
}

func sameSessionOwner(a, b gateway.SessionRef) bool {
	return a.GatewayID == b.GatewayID && a.Boot == b.Boot
}

func (b *backend) cached(cpID string) *gwSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	// A read does not refresh seen: only messages and session events of the
	// charger do. Otherwise a session whose close went to another backend, but
	// that is looked up now and then, would never leave the cache.
	return b.sessions[cpID]
}

func (b *backend) handle(sess *gwSession) *Conn {
	return &Conn{remoteID: sess.ref.CPID, srv: b.s, gw: sess}
}

// backendUpstream is the backend's implementation of gateway.Upstream.
type backendUpstream struct{ b *backend }

// Connect decides an upgrade with the server's authenticator and per-connection
// settings, on a request rebuilt from what the gateway saw.
func (u backendUpstream) Connect(ctx context.Context, req gateway.ConnectRequest) (gateway.ConnectReply, error) {
	s := u.b.s
	r := &http.Request{
		Method:     http.MethodGet,
		URL:        &url.URL{Path: req.Path},
		Header:     req.Header.Clone(),
		RemoteAddr: req.RemoteAddr,
	}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r = r.WithContext(ctx)
	if len(req.PeerCerts) > 0 {
		st := &tls.ConnectionState{HandshakeComplete: true}
		for _, der := range req.PeerCerts {
			if c, err := x509.ParseCertificate(der); err == nil {
				st.PeerCertificates = append(st.PeerCertificates, c)
			}
		}
		r.TLS = st
	}
	cpID := req.CPID
	id, err := s.cfg.auth.Authenticate(r, cpID)
	if err != nil {
		return gateway.ConnectReply{Reject: &gateway.Rejection{Status: http.StatusUnauthorized, Message: "unauthorized"}}, nil
	}
	rep := gateway.ConnectReply{Identity: id.CPID, Meta: id.Metadata}
	if id.CPID != "" {
		cpID = id.CPID
		rep.CPID = id.CPID
	}
	if !validCPID(cpID) {
		return gateway.ConnectReply{Reject: &gateway.Rejection{Status: http.StatusBadRequest, Message: "invalid charge point id"}}, nil
	}
	if s.cfg.connConfig != nil {
		d := s.cfg.dispatcher
		t := ConnTimeouts{
			CallTimeout: d.CallTimeout, WriteTimeout: d.WriteTimeout, PingInterval: d.PingInterval,
			PongWait: d.PongWait, ReadTimeout: d.ReadTimeout,
		}
		s.cfg.connConfig(r, cpID, &t)
		rep.Timeouts = &gateway.Timeouts{
			CallTimeout: t.CallTimeout, WriteTimeout: t.WriteTimeout, PingInterval: t.PingInterval,
			PongWait: t.PongWait, ReadTimeout: t.ReadTimeout,
		}
	}
	return rep, nil
}

// Handle runs an inbound CALL through the payload pipeline and the registered
// handler and returns the final answer for the gateway to write.
func (u backendUpstream) Handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, error) {
	s := u.b.s
	if s.stopping.Load() || s.ctx.Err() != nil {
		return gateway.Reply{Nack: true}, nil
	}
	sess, displaced, _ := u.b.observeChecked(ctx, env.Session, env.Version, nil)
	if displaced != nil {
		u.b.replaced(displaced, sess)
	}
	conn := u.b.handle(sess)
	cfg := s.versionConfig("ocpp" + string(env.Version))
	pipe := dispatcher.NewPipeline(cfg, env.Version, env.Session.CPID)

	// The handler context is detached from the link's cancellation (a dropped
	// stream must not abort the work a handler is halfway through) but keeps the
	// deadline the gateway set.
	hctx := ocppj.ContextWithMessageID(context.WithoutCancel(ctx), env.MsgID)
	hctx = withGatewayConn(hctx, conn)
	if b, ok := envelopeBudget(env); ok {
		var cancel context.CancelFunc
		hctx, cancel = context.WithTimeout(hctx, b)
		defer cancel()
	}
	var run func(ctx context.Context, payload []byte) ([]byte, error)
	if h, ok := s.handlers.call(env.Action); ok {
		run = func(ctx context.Context, payload []byte) ([]byte, error) { return h(ctx, conn, payload) }
	}
	frame := ocppj.Frame{Type: ocppj.Call, MsgID: env.MsgID, Action: env.Action, Payload: env.Payload}
	out := pipe.RunCall(hctx, &frame, run)
	switch {
	case out.Err == nil:
		return gateway.Reply{Payload: out.Payload}, nil
	case out.NoReply:
		return gateway.Reply{NoReply: true}, nil
	}
	return gateway.Reply{Error: &gateway.WireError{
		Kind: gateway.ErrKindCallError, Code: out.Wire, Description: out.Err.Description, Details: out.Err.Details,
	}}, nil
}

// envelopeBudget is the time the handler of env has. The gateway's absolute
// deadline is on its clock, which may differ from this host's, so the budget is
// the distance between two readings of that one clock (ReceivedAt and Deadline).
// An envelope from a gateway that does not set ReceivedAt falls back to the
// absolute deadline.
func envelopeBudget(env gateway.Envelope) (time.Duration, bool) {
	switch {
	case env.Deadline.IsZero():
		return 0, false
	case !env.ReceivedAt.IsZero():
		return max(env.Deadline.Sub(env.ReceivedAt), 0), true
	}
	return max(time.Until(env.Deadline), 0), true
}

// Notify handles SEND, session events and orphan results.
func (u backendUpstream) Notify(ctx context.Context, env gateway.Envelope) error {
	s := u.b.s
	switch env.Kind {
	case gateway.KindSend:
		sess, displaced, _ := u.b.observeChecked(ctx, env.Session, env.Version, nil)
		if displaced != nil {
			u.b.replaced(displaced, sess)
		}
		conn := u.b.handle(sess)
		cfg := s.versionConfig("ocpp" + string(env.Version))
		pipe := dispatcher.NewPipeline(cfg, env.Version, env.Session.CPID)
		hctx := ocppj.ContextWithMessageID(context.WithoutCancel(ctx), env.MsgID)
		hctx = withGatewayConn(hctx, conn)
		var run func(ctx context.Context, payload []byte) error
		if h, ok := s.handlers.send(env.Action); ok {
			run = func(ctx context.Context, payload []byte) error { return h(ctx, conn, payload) }
		}
		frame := ocppj.Frame{Type: ocppj.Send, MsgID: env.MsgID, Action: env.Action, Payload: env.Payload}
		pipe.RunSend(hctx, &frame, run)
	case gateway.KindSessionOpened:
		defer u.b.lockEvents(env.Session.CPID)()
		sess, displaced, stale := u.b.observeChecked(ctx, env.Session, env.Version, env.Open)
		if stale {
			return nil // a late open of a session that was already replaced
		}
		if displaced != nil {
			u.b.replaced(displaced, sess)
		}
		s.handleConnected(u.b.handle(sess))
	case gateway.KindSessionClosed:
		defer u.b.lockEvents(env.Session.CPID)()
		if !u.b.forget(ctx, env.Session) {
			return nil // a newer session of this charger is known
		}
		cause := closeCause(env.Close)
		s.handleDisconnected(u.b.handle(&gwSession{ref: env.Session, version: env.Version}), cause)
	case gateway.KindOrphanResult:
		if s.cfg.onOrphan != nil {
			s.cfg.onOrphan(env.Session.CPID, env.CallID, env.Action, env.Payload, env.Error.Err())
		}
	}
	return nil
}

// replaced fires the disconnect of a session a newer one displaced, so the
// application sees the old one go before the new one is announced. When the old
// session is held by another gateway (the charger reconnected elsewhere) and a
// session registry orders the two, it also asks that gateway to close the old
// socket at once (Control, ErrConnReplaced), instead of leaving it to the
// gateway's reconcile pass (design 2.4, rule 4). The gateway fences the request
// by epoch, so a request that arrives late is harmless.
func (b *backend) replaced(old, cur *gwSession) {
	b.s.handleDisconnected(b.handle(old), ocppj.ErrConnReplaced)
	// Only a session the registry ranks below the new one is closed: an unknown
	// (0) or equal epoch proves nothing about which of the two is current.
	if b.s.cfg.sessionReg == nil || old.ref.GatewayID == cur.ref.GatewayID ||
		old.ref.Epoch == 0 || cur.ref.Epoch <= old.ref.Epoch {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.s.ctx, 5*time.Second)
		defer cancel()
		err := b.link.Control(ctx, gateway.ControlRequest{Target: old.ref, Op: gateway.ControlReplaced, Reason: "replaced by a newer session"})
		if err != nil && !errors.Is(err, gateway.ErrSessionStale) && !errors.Is(err, ocppj.ErrNotConnected) {
			b.s.cfg.dispatcher.Logger.Debug("csms: closing the replaced session on its gateway failed",
				"cp_id", old.ref.CPID, "gateway", old.ref.GatewayID, "epoch", old.ref.Epoch, "error", err)
		}
	}()
}

// lookupGateway resolves a handle for a charger through the session cache and,
// on a miss, the link.
func (s *Server) lookupGateway(ctx context.Context, id string) (*Conn, error) {
	if sess := s.backend.cached(id); sess != nil {
		return s.backend.handle(sess), nil
	}
	// A miss: the session registry is the authority (leases decide liveness);
	// without one the link is asked.
	if reg := s.cfg.sessionReg; reg != nil {
		rec, found, err := reg.Lookup(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("csms: look up %q: %w", id, err)
		}
		if !found {
			return nil, ocppj.ErrNotConnected
		}
		sess, _, _ := s.backend.observe(fromStorage(rec).ref, fromStorage(rec).version, nil)
		return s.backend.handle(sess), nil
	}
	loc, ok, err := s.backend.link.Locate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("csms: look up %q: %w", id, err)
	}
	if !ok {
		return nil, ocppj.ErrNotConnected
	}
	sess, _, _ := s.backend.observe(loc.Ref, loc.Version, loc.Info)
	return s.backend.handle(sess), nil
}

// callGateway is doCall for a handle that stands for a gateway session: the
// request is validated here (the gateway never reads payloads), sent through the
// link, and the answer validated here. A stale or vanished session is looked up
// again once.
func (s *Server) callGateway(ctx context.Context, c *Conn, action string, payload []byte) ([]byte, error) {
	sess := c.gw
	for attempt := 0; ; attempt++ {
		out, sent, err := s.callGatewayOnce(ctx, sess, action, payload)
		if err == nil {
			return out, nil
		}
		retryable := sent == gateway.NotSent &&
			(errors.Is(err, gateway.ErrSessionStale) || errors.Is(err, ocppj.ErrNotConnected))
		if !retryable || attempt > 0 {
			return nil, gateway.WithSent(err, sent)
		}
		s.backend.uncache(sess.ref)
		fresh, lerr := s.relocate(ctx, sess)
		if lerr != nil {
			return nil, gateway.WithSent(err, sent)
		}
		sess = fresh
	}
}

func (s *Server) callGatewayOnce(ctx context.Context, sess *gwSession, action string, payload []byte) ([]byte, gateway.Sent, error) {
	version := sess.version
	if version == "" {
		return nil, gateway.NotSent, ocppj.ErrNotConnected
	}
	cfg := s.versionConfig("ocpp" + string(version))
	pipe := dispatcher.NewPipeline(cfg, version, sess.ref.CPID)
	msgID := ocppj.NewMsgID()
	ocppj.NotifyMessageID(ctx, msgID)
	payload, err := pipe.ValidateOutbound(ctx, action, "request", payload)
	if err != nil {
		return nil, gateway.NotSent, err
	}
	payload, err = pipe.TransformOutbound(action, "request", payload)
	if err != nil {
		return nil, gateway.NotSent, err
	}
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(cfg.CallTimeout)
	}
	rep, err := s.backend.link.Call(ctx, gateway.CallRequest{
		Target: sess.ref, CallID: msgID, Action: action, Payload: payload,
		Deadline: dl, Budget: max(time.Until(dl), time.Millisecond), Trace: traceparent(ctx),
	})
	if err != nil {
		sent := rep.Sent
		if sent == gateway.SentUnknown {
			sent = gateway.SentNoResponse
		}
		return nil, sent, err
	}
	if rep.Error != nil {
		return nil, rep.Sent, rep.Error.Err()
	}
	out, err := pipe.ValidateInbound(ctx, action, "response", rep.Payload)
	if err != nil {
		return nil, gateway.Answered, err
	}
	return out, gateway.Answered, nil
}

// sendGateway is Send for a gateway session handle.
func (s *Server) sendGateway(ctx context.Context, c *Conn, action string, payload []byte) error {
	sess := c.gw
	if sess.version != ocppj.V21 {
		return fmt.Errorf("%w: SEND requires OCPP 2.1, connection is %s", ocppj.ErrUnsupportedVersion, sess.version)
	}
	cfg := s.versionConfig("ocpp" + string(sess.version))
	pipe := dispatcher.NewPipeline(cfg, sess.version, sess.ref.CPID)
	payload, err := pipe.ValidateOutbound(ctx, action, "request", payload)
	if err != nil {
		return err
	}
	payload, err = pipe.TransformOutbound(action, "request", payload)
	if err != nil {
		return err
	}
	return s.backend.link.Send(ctx, gateway.CallRequest{Target: sess.ref, Action: action, Payload: payload})
}

// closeGateway is Conn.Close for a gateway session handle.
func (s *Server) closeGateway(ctx context.Context, c *Conn) error {
	return s.backend.link.Control(ctx, gateway.ControlRequest{
		Target: c.gw.ref, Op: gateway.ControlDisconnect, Reason: "closed by the server",
	})
}

// closeCause rebuilds the disconnect cause a gateway reported: a well-known
// error keeps its identity (errors.Is) and the gateway's wording, anything else
// arrives as text.
func closeCause(ci *gateway.CloseInfo) error {
	if ci == nil {
		return ocppj.ErrConnClosed
	}
	if e := gateway.SentinelByName(ci.Cause); e != nil {
		if ci.Message == "" || ci.Message == e.Error() {
			return e
		}
		return &causeError{msg: ci.Message, err: e}
	}
	if ci.Message == "" {
		return ocppj.ErrConnClosed
	}
	return errors.New(ci.Message)
}

type causeError struct {
	msg string
	err error
}

func (e *causeError) Error() string { return e.msg }
func (e *causeError) Unwrap() error { return e.err }

// relocate finds the current session of the charger old stood for. It fails when
// there is none or when it is still old (nothing to retry on).
func (s *Server) relocate(ctx context.Context, old *gwSession) (*gwSession, error) {
	var ref gateway.SessionRef
	var version ocppj.Version
	var info *gateway.SessionInfo
	if reg := s.cfg.sessionReg; reg != nil {
		rec, found, err := reg.Lookup(ctx, old.ref.CPID)
		if err != nil || !found {
			return nil, ocppj.ErrNotConnected
		}
		g := fromStorage(rec)
		ref, version = g.ref, g.version
	} else {
		loc, ok, err := s.backend.link.Locate(ctx, old.ref.CPID)
		if err != nil || !ok {
			return nil, ocppj.ErrNotConnected
		}
		ref, version, info = loc.Ref, loc.Version, loc.Info
	}
	if ref == old.ref {
		return nil, ocppj.ErrNotConnected
	}
	cur, _, _ := s.backend.observe(ref, version, info)
	return cur, nil
}

// info is the announcement this backend makes to a link.
func (s *Server) info() gateway.BackendInfo {
	i := s.cfg.backendInfo
	i.Proto = gateway.ProtoRange{Min: gateway.ProtoVersion, Max: gateway.ProtoVersion}
	i.Caps = []string{gateway.CapNack, gateway.CapOrphan, gateway.CapAttrsSet, gateway.CapSend21, gateway.CapRouteV1}
	if len(i.Actions) == 0 {
		// What this build has handlers for, so a gateway can notice a router
		// sending an action to a pool that cannot serve it. Handlers registered
		// after ServeGateway are not announced; none registered announces
		// nothing (= all).
		i.Actions = s.handlers.actions()
	}
	return i
}

// setAttrsGateway is Conn.SetAttrs for a gateway session handle: the gateway
// merges attrs into the session attributes it routes on (an empty value deletes
// the key; ErrAttrsTooLarge above the cap).
func (s *Server) setAttrsGateway(ctx context.Context, c *Conn, attrs map[string]string) error {
	err := s.backend.link.Control(ctx, gateway.ControlRequest{
		Target: c.gw.ref, Op: gateway.ControlSetAttrs, Attrs: attrs,
	})
	if errors.Is(err, gateway.ErrAttrsTooLarge) {
		return ErrAttrsTooLarge
	}
	return err
}
