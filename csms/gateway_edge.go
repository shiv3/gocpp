package csms

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway"
	"go.opentelemetry.io/otel/trace"
)

// defaultGatewayHandlerTimeout is the per-CALL budget of a gateway: it must stay
// under the usual 30 s charger timeout so the CALLERROR still arrives in time.
const defaultGatewayHandlerTimeout = 25 * time.Second

// edgeNotifyTimeout bounds delivery of a session event to the processing tier.
const edgeNotifyTimeout = 5 * time.Second

// edgeNotifyClosing is the same bound once the gateway itself is closing.
const edgeNotifyClosing = 20 * time.Millisecond

// NewGatewayServer returns a Server that is the connection tier of a split
// deployment. It terminates WebSockets exactly like NewServer (every connection
// option keeps its meaning) but runs no handlers: each inbound CALL is forwarded,
// payload untouched, to the processing tier through link and the answer is
// written back. Authentication is decided by the processing tier (its
// WithAuthenticator and WithConnConfig), session events and CSMS-initiated calls
// travel over the same link.
//
// Options that belong to the processing tier are ignored here: schema
// validation, time encoding, handlers and the OnConnect/OnDisconnect callbacks,
// so one option list can configure both tiers. See docs/gateway.md.
//
// Experimental: this API may change before v1.0.
func NewGatewayServer(link gateway.GatewayLink, opts ...Option) *Server {
	opts = append(opts[:len(opts):len(opts)], optionFunc(func(c *serverConfig) {
		c.edgeLink = link
	}))
	return NewServer(opts...)
}

// WithGatewayID sets the stable identity of a gateway (and of the session
// references it issues). Default: random per process.
func WithGatewayID(id string) Option {
	return optionFunc(func(c *serverConfig) { c.gatewayID = id })
}

// edge is the gateway-side state of a Server.
type edge struct {
	s    *Server
	link gateway.GatewayLink
	id   string
	boot string

	router gateway.Router
	auth   *authCache

	epoch atomic.Uint64
	seq   atomic.Uint64

	detach func()

	responders map[string]EdgeResponder
	edgeMetric observability.EdgeMetrics

	callsMu sync.Mutex
	calls   map[string]*edgeCall

	// routeWrites bounds the registry writes of route.pool in flight (a policy
	// step changes the pool of every session at once).
	routeWrites chan struct{}
}

// routePoolKey is the key of the routing copy: Session.Meta["route.pool"].
const routePoolKey = "pool"

const edgeRouteWrites = 32

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newEdge(s *Server, link gateway.GatewayLink, id string) *edge {
	boot := randHex(6)
	if s.sessions != nil {
		// The session registry names this gateway; the references we issue must
		// agree with the records it holds.
		id, boot = s.sessions.cfg.GatewayID, s.sessions.boot
	}
	if id == "" {
		id = "gw-" + randHex(4)
	}
	router := s.cfg.gwRouter
	if router == nil {
		router = gateway.DefaultRouter{}
	}
	e := &edge{s: s, link: link, id: id, boot: boot, router: router, calls: make(map[string]*edgeCall),
		routeWrites: make(chan struct{}, edgeRouteWrites), responders: s.cfg.buildEdgeResponders()}
	e.edgeMetric, _ = s.cfg.metrics.(observability.EdgeMetrics)
	if c := s.cfg.gwAuthCache; c != nil {
		e.auth = newAuthCache(*c)
	}
	s.reg.SetFallback(e.forward)
	s.reg.SetSendFallback(e.forwardSend)
	e.detach = link.Attach(id, edgeDownstream{e})
	if s.cfg.attrSource != nil && s.cfg.attrSourceOpts.Refresh > 0 {
		go e.refreshExt(s.cfg.attrSourceOpts.Refresh)
	}
	return e
}

// applyEdgeConfig neutralises the options that belong to the processing tier.
func applyEdgeConfig(c *serverConfig) {
	c.registry = nil
	c.edgeTimeEncoding = c.timeEncoding
	c.timeEncoding = schema.TimeEncoding{}
	c.dispatcher.SchemaMode = dispatcher.SchemaModeOff
	c.dispatcher.SchemaValidate = nil
	c.dispatcher.SchemaValidateLenient = nil
	c.onConnect, c.onDisconnect = nil, nil
	c.connConfig = nil
	c.instanceID = ""
	c.link = nil
	c.splitInProcess = false
	c.splitLink = nil
	if c.dispatcher.HandlerTimeout == 0 {
		c.dispatcher.HandlerTimeout = defaultGatewayHandlerTimeout
	}
	// A gateway's handler is the forward path (pool selection, the link
	// transport's send path, its encoder): about 10 KB deep.
	c.dispatcher.DeepHandlerStack = true
}

// newSession allocates the reference of a session being accepted. With a
// session registry the epoch is the one the registry assigned at claim time
// (0 when the claim did not complete: the session then runs unfenced until the
// registry is reachable); without one the gateway numbers its own sessions.
func (e *edge) newSession(cpID string, claimedEpoch uint64) *gwSession {
	epoch := claimedEpoch
	if e.s.sessions == nil {
		epoch = e.epoch.Add(1)
	}
	return &gwSession{ref: gateway.SessionRef{CPID: cpID, GatewayID: e.id, Boot: e.boot, Epoch: epoch}}
}

// connect asks the processing tier to decide an upgrade. status != 0 refuses it.
func (e *edge) connect(r *http.Request, cpID string) (rep gateway.ConnectReply, status int, msg string) {
	req := gateway.ConnectRequest{
		CPID: cpID, Path: r.URL.Path, RemoteAddr: r.RemoteAddr, Header: r.Header.Clone(),
		PeerCerts: peerCerts(r), Subprotocol: websocketSubprotocols(r),
		Gateway: gateway.SessionRef{GatewayID: e.id, Boot: e.boot},
	}
	req.Route = e.route(gateway.KindCall, cpID, "$connect", "", nil)
	var key authKey
	var gen uint64
	if e.auth != nil {
		gen = e.auth.generation()
		key = authKeyOf(req)
		if cached, ok := e.auth.fresh(key); ok {
			return cached, 0, ""
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), edgeNotifyTimeout)
	defer cancel()
	rep, err := e.link.Connect(ctx, req)
	if err != nil {
		if e.auth != nil {
			if cached, ok := e.auth.stale(key); ok {
				e.s.cfg.dispatcher.Logger.Warn("gateway: connect decision unavailable, using cached", "cp_id", cpID, "error", err)
				return cached, 0, ""
			}
		}
		e.s.cfg.dispatcher.Logger.Warn("gateway: connect decision unavailable", "cp_id", cpID, "error", err)
		return rep, http.StatusServiceUnavailable, "service unavailable"
	}
	if e.auth != nil {
		if rep.Reject == nil {
			e.auth.put(key, cpID, rep, gen)
		} else {
			e.auth.drop(key)
		}
	}
	if rep.Reject != nil {
		st := rep.Reject.Status
		if st == 0 {
			st = http.StatusUnauthorized
		}
		return rep, st, rep.Reject.Message
	}
	return rep, 0, ""
}

func websocketSubprotocols(r *http.Request) []string {
	var out []string
	for _, h := range r.Header.Values("Sec-WebSocket-Protocol") {
		out = append(out, splitComma(h)...)
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			p := s[start:i]
			for len(p) > 0 && p[0] == ' ' {
				p = p[1:]
			}
			for len(p) > 0 && p[len(p)-1] == ' ' {
				p = p[:len(p)-1]
			}
			if p != "" {
				out = append(out, p)
			}
			start = i + 1
		}
	}
	return out
}

// opened tells the processing tier about a new session. A failure is logged and
// does not refuse the connection: the first inbound envelope carries enough for
// the processing tier to work.
func (e *edge) opened(conn *Conn, r *http.Request, rep gateway.ConnectReply) {

	sess := conn.gw
	info := &gateway.SessionInfo{
		RemoteAddr: r.RemoteAddr, Subprotocol: conn.inner.Subprotocol(),
		RequestHeader: r.Header.Clone(), PeerCerts: peerCerts(r),
		Identity: rep.Identity, Meta: rep.Meta,
	}
	sess.setInfo(info)
	env := gateway.Envelope{
		Proto: gateway.ProtoVersion, Session: sess.ref, Version: sess.version,
		Kind: gateway.KindSessionOpened, DeliveryID: e.deliveryID(), Attempt: 1,
		ReceivedAt: time.Now(), Open: info, Attrs: rep.Attrs,
		Route: e.route(gateway.KindSessionOpened, sess.ref.CPID, "$opened", sess.version, rep.Attrs),
	}
	e.noteRoute(conn.inner, sess, env.Route.Pool)
	e.notify(env)
}

func (e *edge) closed(conn *Conn, cause error) {

	if conn.gw == nil {
		return
	}
	ci := &gateway.CloseInfo{Cause: gateway.SentinelName(cause)}
	if cause != nil {
		ci.Message = cause.Error()
	}
	e.notify(gateway.Envelope{
		Proto: gateway.ProtoVersion, Session: conn.gw.ref, Version: conn.gw.version,
		Kind: gateway.KindSessionClosed, DeliveryID: e.deliveryID(), Attempt: 1,
		ReceivedAt: time.Now(), Close: ci,
		Route: e.route(gateway.KindSessionClosed, conn.gw.ref.CPID, "$closed", conn.gw.version, conn.gw.attrs()),
	})
}

func (e *edge) notify(env gateway.Envelope) {
	// Detached from the server context so a closing server still reports the
	// disconnects it causes.
	timeout := edgeNotifyTimeout
	if e.s.ctx.Err() != nil {
		timeout = edgeNotifyClosing // closing: do not wait for a backend to come back
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.s.ctx), timeout)
	defer cancel()
	// A server that starts closing while this notify waits for a backend (an
	// empty pool holds for seconds) must not leave the goroutine behind: give the
	// delivery the closing bound from that moment.
	stop := context.AfterFunc(e.s.ctx, func() { time.AfterFunc(edgeNotifyClosing, cancel) })
	defer stop()
	if err := e.link.Notify(ctx, env); err != nil {
		e.s.cfg.dispatcher.Logger.Warn("gateway: session event not delivered",
			"cp_id", env.Session.CPID, "kind", env.Kind.String(), "error", err)
	}
}

// route asks the router where a message goes. The result is also what the
// envelope reports to the backend.
func (e *edge) route(kind gateway.Kind, cpID, action string, version ocppj.Version, attrs map[string]string) *gateway.Route {
	d := e.router.Route(gateway.RouteInput{CPID: cpID, Action: action, Kind: kind, OCPP: version, Attrs: attrs})
	return &d
}

func (e *edge) deliveryID() string {
	return e.boot + "-" + strconv.FormatUint(e.seq.Add(1), 10)
}

func traceparent(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-" + sc.TraceFlags().String()
}

func (e *edge) envelope(ctx context.Context, dc *dispatcher.Conn, kind gateway.Kind, action string, payload []byte) (gateway.Envelope, bool) {
	sess, _ := dc.Owner().(*gwSession)
	if sess == nil {
		return gateway.Envelope{}, false
	}
	msgID, _ := ocppj.MessageIDFromContext(ctx)
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(e.s.cfg.dispatcher.HandlerTimeout)
	}
	return gateway.Envelope{
		Proto: gateway.ProtoVersion, Session: sess.ref, Version: sess.version, Kind: kind,
		DeliveryID: e.deliveryID(), Attempt: 1, MsgID: msgID, Action: action, Payload: payload,
		ReceivedAt: time.Now(), Deadline: dl, Trace: traceparent(ctx), Attrs: sess.attrs(),
		Route: e.routeFor(dc, sess, kind, action),
	}, true
}

// routeFor routes one message of sess and keeps the registry copy of the
// decision (Session.Meta["route.pool"]) current.
func (e *edge) routeFor(dc *dispatcher.Conn, sess *gwSession, kind gateway.Kind, action string) *gateway.Route {
	r := e.route(kind, sess.ref.CPID, action, sess.version, sess.attrs())
	e.noteRoute(dc, sess, r.Pool)
	return r
}

// noteRoute copies the pool a session is routed to into its registry record
// when it differs from the last copy, asynchronously and best effort: the hot
// path never waits for the registry, a failed or skipped write is retried by the
// next message, and the record also gets the value at the next claim. With a
// rule limited to some actions the copy follows the action of the latest
// message.
func (e *edge) noteRoute(dc *dispatcher.Conn, sess *gwSession, pool string) {
	if pool == "" || pool == gateway.PoolReject || e.s.sessions == nil {
		return
	}
	if p := sess.routePool.Load(); p != nil && *p == pool {
		return
	}
	rw, ok := e.s.sessions.reg.(storage.RouteWriter)
	if !ok {
		return
	}
	ref, _ := dc.Attachment().(*sessionRef)
	if ref == nil || ref.s.Epoch == 0 {
		return // not claimed yet: the claim carries the value
	}
	if !sess.routeBusy.CompareAndSwap(false, true) {
		return // a write for this session is in flight; the next message tries again
	}
	select {
	case e.routeWrites <- struct{}{}:
	default:
		sess.routeBusy.Store(false)
		return // busy; the next message tries again
	}
	go func() {
		defer func() { <-e.routeWrites; sess.routeBusy.Store(false) }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(e.s.ctx), edgeNotifyTimeout)
		defer cancel()
		written, err := rw.SetRoute(ctx, ref.s.CPID, ref.s.Epoch, map[string]string{routePoolKey: pool})
		if err != nil {
			e.s.cfg.dispatcher.Logger.Debug("gateway: route copy failed", "cp_id", ref.s.CPID, "error", err)
			return
		}
		if written {
			sess.routePool.Store(&pool)
		}
	}()
}

// forward is the catch-all CALL handler: it hands the call to the processing
// tier and turns the answer into what the dispatcher writes.
func (e *edge) forward(ctx context.Context, dc *dispatcher.Conn, action string, payload []byte) ([]byte, error) {
	if r := e.responders[action]; r != nil {
		if sess, _ := dc.Owner().(*gwSession); sess != nil {
			req := EdgeRequest{CPID: sess.ref.CPID, Version: sess.version, Action: action, Payload: payload, Attrs: sess.attrs()}
			if out, ok := r(ctx, req); ok {
				if e.edgeMetric != nil {
					e.edgeMetric.EdgeResponse(string(sess.version), action)
				}
				if fn := e.s.cfg.onEdgeResponse; fn != nil {
					fn(sess.ref.CPID, action)
				}
				return out, nil
			}
		}
	}
	env, ok := e.envelope(ctx, dc, gateway.KindCall, action, payload)
	if !ok {
		return nil, ocppj.NewCallError(ocppj.ErrorCodeInternalError, "no gateway session", nil)
	}
	rep, err := e.link.Handle(ctx, env)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		e.s.cfg.dispatcher.Logger.Warn("gateway: upstream failed",
			"cp_id", env.Session.CPID, "action", action, "error", err)
		return nil, ocppj.NewCallError(ocppj.ErrorCodeInternalError, "upstream unavailable", nil)
	}
	if rep.NoReply {
		return nil, &dispatcher.RawCallError{NoReply: true}
	}
	if we := rep.Error; we != nil {
		if we.Kind == gateway.ErrKindCallError {
			return nil, &dispatcher.RawCallError{
				Err:  &ocppj.CallError{Code: ocppj.ErrorCode(we.Code), Description: we.Description, Details: we.Details},
				Wire: we.Code,
			}
		}
		return nil, ocppj.NewCallError(ocppj.ErrorCodeInternalError, "upstream error", nil)
	}
	return rep.Payload, nil
}

func (e *edge) forwardSend(ctx context.Context, dc *dispatcher.Conn, action string, payload []byte) error {
	env, ok := e.envelope(ctx, dc, gateway.KindSend, action, payload)
	if !ok {
		return errors.New("no gateway session")
	}
	return e.link.Notify(ctx, env)
}

// edgeDownstream serves the processing tier's calls to the sessions held here.
type edgeDownstream struct{ e *edge }

// session finds the live connection for target and applies the epoch fence.
func (d edgeDownstream) session(target gateway.SessionRef) (*Conn, error) {
	c, ok := d.e.s.Get(target.CPID)
	if !ok || c.gw == nil {
		return nil, ocppj.ErrNotConnected
	}
	if target.Epoch != 0 && target.Epoch != c.gw.ref.Epoch {
		return nil, gateway.ErrSessionStale
	}
	return c, nil
}

func (d edgeDownstream) LocateSession(cpID string) (gateway.Located, bool) {
	c, ok := d.e.s.Get(cpID)
	if !ok || c.gw == nil {
		return gateway.Located{}, false
	}
	return gateway.Located{Ref: c.gw.ref, Version: c.gw.version, Info: c.gw.info()}, true
}

// edgeCall is one CSMS call in flight, shared by duplicate CallIDs.
type edgeCall struct {
	done  chan struct{}
	reply gateway.CallReply
}

func (d edgeDownstream) Call(ctx context.Context, req gateway.CallRequest) (gateway.CallReply, error) {
	c, err := d.session(req.Target)
	if err != nil {
		return gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(err)}, nil
	}
	key := ""
	if req.CallID != "" {
		key = req.Target.CPID + "\x00" + req.CallID
		d.e.callsMu.Lock()
		if ec, dup := d.e.calls[key]; dup {
			d.e.callsMu.Unlock()
			select {
			case <-ec.done:
				return ec.reply, nil
			case <-ctx.Done():
				return gateway.CallReply{}, ctx.Err()
			}
		}
		ec := &edgeCall{done: make(chan struct{})}
		d.e.calls[key] = ec
		d.e.callsMu.Unlock()
		defer func() {
			d.e.callsMu.Lock()
			delete(d.e.calls, key)
			d.e.callsMu.Unlock()
			close(ec.done)
		}()
		rep, err := d.call(ctx, c, req)
		ec.reply = rep
		return rep, err
	}
	return d.call(ctx, c, req)
}

// call runs one call on c. Once the frame is written the call keeps running
// even if the caller goes away (its stream broke); the answer is then reported
// as an orphan result instead of being dropped. Before the write, a caller that
// goes away cancels the call.
func (d edgeDownstream) call(ctx context.Context, c *Conn, req gateway.CallRequest) (gateway.CallReply, error) {
	var sent atomic.Bool
	cctx := context.WithoutCancel(ctx)
	var cancel context.CancelFunc
	switch {
	case req.Budget > 0:
		cctx, cancel = context.WithTimeout(cctx, req.Budget)
	case !req.Deadline.IsZero():
		cctx, cancel = context.WithDeadline(cctx, req.Deadline)
	default:
		cctx, cancel = context.WithCancel(cctx)
	}
	cctx = dispatcher.WithSentHook(cctx, func() { sent.Store(true) })
	type result struct {
		payload []byte
		err     error
	}
	resCh := make(chan result, 1)
	go func() {
		defer cancel()
		p, err := dispatcher.DoCall(cctx, c.inner, req.Action, req.Payload)
		resCh <- result{p, err}
	}()
	reply := func(r result) gateway.CallReply {
		out := gateway.CallReply{Session: c.gw.ref}
		if r.err == nil {
			out.Payload, out.Sent = r.payload, gateway.Answered
		} else {
			out.Error = gateway.EncodeError(r.err)
			var ce *ocppj.CallError
			out.Sent = gateway.NotSent
			if errors.As(r.err, &ce) {
				out.Sent = gateway.Answered
			} else if sent.Load() {
				out.Sent = gateway.SentNoResponse
			}
		}
		return out
	}
	// orphan reports the answer of a call whose caller is gone.
	orphan := func(r result) {
		if r.err != nil && !errors.As(r.err, new(*ocppj.CallError)) {
			return // no answer arrived; nothing to report
		}
		env := gateway.Envelope{
			Proto: gateway.ProtoVersion, Session: c.gw.ref, Version: c.gw.version,
			Kind: gateway.KindOrphanResult, DeliveryID: d.e.deliveryID(), Attempt: 1,
			CallID: req.CallID, Action: req.Action, Payload: r.payload, ReceivedAt: time.Now(),
			Error: gateway.EncodeError(r.err),
		}
		if req.CallerPool != "" {
			// The answer goes back to the pool that asked, not to wherever the
			// charger is routed now; if that pool has no backend left (the
			// caller died, say) the default pool takes it.
			env.Route = &gateway.Route{Pool: req.CallerPool, Fallback: gateway.DefaultPool, Rule: "$orphan"}
		}
		d.e.notify(env)
	}
	select {
	case r := <-resCh:
		if ctx.Err() != nil && sent.Load() {
			// The caller went away and the answer came in at the same moment (the
			// select above picks at random when both are ready): the caller is
			// gone, so the answer is an orphan, not a reply nobody reads.
			orphan(r)
			return gateway.CallReply{Sent: gateway.SentNoResponse, Session: c.gw.ref,
				Error: gateway.EncodeError(ctx.Err())}, nil
		}
		return reply(r), nil
	case <-ctx.Done():
		if !sent.Load() {
			// Cancel and classify from what the call really did: the frame may
			// have been written between the check above and the cancel, and then
			// the caller must not be told "not sent" (it would retry a command the
			// charger already holds).
			cancel()
			r := <-resCh
			rep := reply(r)
			if rep.Sent == gateway.NotSent {
				rep.Error = gateway.EncodeError(ctx.Err())
			}
			return rep, nil
		}
		// Written, caller gone: wait for the answer in the background.
		go func() { orphan(<-resCh) }()
		return gateway.CallReply{Sent: gateway.SentNoResponse, Session: c.gw.ref,
			Error: gateway.EncodeError(ctx.Err())}, nil
	}
}

func (d edgeDownstream) Send(ctx context.Context, req gateway.CallRequest) error {
	c, err := d.session(req.Target)
	if err != nil {
		return err
	}
	return dispatcher.DoSend(ctx, c.inner, req.Action, req.Payload)
}

func (d edgeDownstream) Control(ctx context.Context, req gateway.ControlRequest) error {
	if req.Op == gateway.ControlInvalidateAuth {
		d.e.s.InvalidateAuthCache(req.Target.CPID)
		return nil
	}
	c, err := d.session(req.Target)
	if err != nil {
		return err
	}
	switch req.Op {
	case gateway.ControlDisconnect:
		return c.Close(ctx)
	case gateway.ControlReplaced:
		c.inner.Abort(ocppj.ErrConnReplaced)
		return nil
	case gateway.ControlSetAttrs:
		err := c.gw.mergeAttrs(req.Attrs)
		if err != nil {
			return err
		}
		// Routing reads the new attributes from the next message on; the registry
		// copy (for operators) follows best effort.
		if d.e.s.sessions != nil {
			_ = d.e.s.SetSessionAttrs(ctx, c.ID(), req.Attrs)
		}
		return nil
	}
	return gateway.ErrUnsupported
}
