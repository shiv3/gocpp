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
}

func newBackend(s *Server, link gateway.BackendLink) *backend {
	return &backend{s: s, link: link, sessions: make(map[string]*gwSession)}
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
// session to use and, when ref displaces an older one, that older one.
func (b *backend) observe(ref gateway.SessionRef, version ocppj.Version, info *gateway.SessionInfo) (cur, displaced *gwSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.sessions[ref.CPID]
	if old != nil {
		switch {
		case old.ref.Epoch == ref.Epoch && old.ref.GatewayID == ref.GatewayID && old.ref.Boot == ref.Boot:
			if info != nil && old.info() == nil {
				n := newGwSession(old.ref, old.version, info)
				n.setAttrs(old.attrs())
				b.sessions[ref.CPID] = n
				return n, nil
			}
			return old, nil
		case old.ref.Epoch > ref.Epoch && old.ref.GatewayID == ref.GatewayID && old.ref.Boot == ref.Boot:
			// A late message of an older session of the same gateway.
			return newGwSession(ref, version, info), nil
		}
	}
	cur = newGwSession(ref, version, info)
	b.sessions[ref.CPID] = cur
	return cur, old
}

// forget drops the cached session if it is exactly ref. It reports whether it
// was the newest one.
func (b *backend) forget(ref gateway.SessionRef) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur := b.sessions[ref.CPID]
	if cur != nil && cur.ref == ref {
		delete(b.sessions, ref.CPID)
		return true
	}
	return false
}

func (b *backend) cached(cpID string) *gwSession {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	sess, displaced := u.b.observe(env.Session, env.Version, nil)
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
	if !env.Deadline.IsZero() {
		var cancel context.CancelFunc
		hctx, cancel = context.WithDeadline(hctx, env.Deadline)
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

// Notify handles SEND, session events and orphan results.
func (u backendUpstream) Notify(ctx context.Context, env gateway.Envelope) error {
	s := u.b.s
	switch env.Kind {
	case gateway.KindSend:
		sess, displaced := u.b.observe(env.Session, env.Version, nil)
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
		sess, displaced := u.b.observe(env.Session, env.Version, env.Open)
		if displaced != nil {
			u.b.replaced(displaced, sess)
		}
		s.handleConnected(u.b.handle(sess))
	case gateway.KindSessionClosed:

		if !u.b.forget(env.Session) {
			return nil // stale: a newer session of this charger is known
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
// application sees the old one go before the new one is announced.
func (b *backend) replaced(old, _ *gwSession) {
	b.s.handleDisconnected(b.handle(old), ocppj.ErrConnReplaced)
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
		sess, _ := s.backend.observe(fromStorage(rec).ref, fromStorage(rec).version, nil)
		return s.backend.handle(sess), nil
	}
	loc, ok, err := s.backend.link.Locate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("csms: look up %q: %w", id, err)
	}
	if !ok {
		return nil, ocppj.ErrNotConnected
	}
	sess, _ := s.backend.observe(loc.Ref, loc.Version, loc.Info)
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
		s.backend.forget(sess.ref)
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
		Deadline: dl, Trace: traceparent(ctx),
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
	cur, _ := s.backend.observe(ref, version, info)
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
