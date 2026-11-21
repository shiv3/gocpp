package linknats

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	"github.com/shiv3/gocpp/addons/link-grpc/linkconv"
	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"google.golang.org/protobuf/proto"
)

// SupportedProto is the link protocol range this build speaks; it is the gRPC
// link's, the messages being the same.
var SupportedProto = linkgrpc.SupportedProto

// supportedCaps are the capabilities this build advertises (both ends).
var supportedCaps = []string{
	gateway.CapNack, gateway.CapOrphan, gateway.CapAttrsSet, gateway.CapRouteV1, gateway.CapSend21, gateway.CapCancel,
	gateway.CapActionList, gateway.CapLocate,
}

const (
	// DefaultPrefix is the subject prefix of every link subject.
	DefaultPrefix = "gocpp"

	defaultHelloInterval = 5 * time.Second
	leaseFactor          = 3 // intervals without a Hello before a peer is gone
	helloMSLabel         = "link.hello_ms"
	maxNackRetries       = 4
)

// Gateway is the gateway's end of the NATS link. It implements
// gateway.GatewayLink (hand it to csms.NewGatewayServer): inbound work is
// published to the backends' queue groups, and the Downstream attached to it
// is served on the gateway's own subjects.
type Gateway struct {
	nc      *nats.Conn
	prefix  string
	version string
	metrics Metrics
	pools   *gateway.PoolSet
	accept  time.Duration
	boot    string
	gwID    atomic.Value // string
	mux     *mux

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	subs   []*nats.Subscription

	mu     sync.Mutex
	roster map[string]*entry
	pubs   map[string]*poolUpstream
	downs  map[string]gateway.Downstream
	closed bool
}

// entry is one backend known from its Hello.
type entry struct {
	info     gateway.BackendInfo
	proto    uint32
	interval time.Duration
	last     time.Time
	member   *gateway.Member
	draining bool
}

// GatewayOption configures a Gateway.
type GatewayOption func(*Gateway)

// WithGatewayVersion labels the gateway build (reported in HelloAck).
func WithGatewayVersion(v string) GatewayOption { return func(g *Gateway) { g.version = v } }

// WithGatewayMetrics sets the metrics sink.
func WithGatewayMetrics(m Metrics) GatewayOption { return func(g *Gateway) { g.metrics = m } }

// WithUpstreamWait sets how long an inbound CALL waits for a backend (default
// gateway.DefaultUpstreamWait).
func WithUpstreamWait(d time.Duration) GatewayOption {
	return func(g *Gateway) { g.pools = gateway.NewPoolSet(d) }
}

// WithAcceptTimeout bounds how long a request may wait for any backend to
// acknowledge it (default 5 s). A request nobody acknowledged is failed with an
// InternalError for the charger, not redispatched: it may have been swallowed
// by a backend that died (or was partitioned from NATS) just then.
func WithAcceptTimeout(d time.Duration) GatewayOption { return func(g *Gateway) { g.accept = d } }

// WithPrefix sets the subject prefix (default "gocpp"); gateways and backends
// of one deployment must agree, which also lets several deployments share a
// NATS cluster.
func WithPrefix(p string) GatewayOption { return func(g *Gateway) { g.prefix = p } }

// NewGateway returns the gateway end of the link over nc. The caller owns nc
// and closes it after Close. Configure nc with nats.ReconnectBufSize(-1) so
// nothing is buffered across a reconnect (a request published after its
// deadline would otherwise still reach a backend).
func NewGateway(nc *nats.Conn, opts ...GatewayOption) (*Gateway, error) {
	g := &Gateway{
		nc: nc, prefix: DefaultPrefix, metrics: NopMetrics{}, pools: gateway.NewPoolSet(0), accept: 5 * time.Second,
		boot: nuid.Next(), roster: make(map[string]*entry), pubs: make(map[string]*poolUpstream),
		downs: make(map[string]gateway.Downstream),
	}
	for _, o := range opts {
		o(g)
	}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	var err error
	if g.mux, err = newMux(nc, g.prefix, "g"+g.boot); err != nil {
		return nil, err
	}
	g.mux.acceptTimeout = g.accept
	for subj, fn := range map[string]nats.MsgHandler{
		g.prefix + ".hello": g.onHello,
		g.prefix + ".drain": g.onDrain,
	} {
		s, err := nc.Subscribe(subj, fn)
		if err != nil {
			g.Close()
			return nil, err
		}
		g.subs = append(g.subs, s)
	}
	g.wg.Add(1)
	go g.reaper()
	return g, nil
}

// Close stops the gateway end: subscriptions end, requests in flight fail. It
// does not close the NATS connection.
func (g *Gateway) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	g.mu.Unlock()
	g.cancel()
	for _, s := range g.subs {
		_ = s.Unsubscribe()
	}
	g.wg.Wait()
	g.mux.close()
}

// Pools reports the announced backends by pool.
func (g *Gateway) Pools() []gateway.PoolStatus { return g.pools.Status() }

// Available reports whether pool name has an announced, non-draining backend.
func (g *Gateway) Available(name string) bool { return g.pools.Available(name) }

// Connect implements gateway.Upstream.
func (g *Gateway) Connect(ctx context.Context, req gateway.ConnectRequest) (gateway.ConnectReply, error) {
	return g.pools.Connect(ctx, req)
}

// Handle implements gateway.Upstream.
func (g *Gateway) Handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, error) {
	return g.pools.Handle(ctx, env)
}

// Notify implements gateway.Upstream.
func (g *Gateway) Notify(ctx context.Context, env gateway.Envelope) error {
	return g.pools.Notify(ctx, env)
}

func (g *Gateway) peerID() string {
	s, _ := g.gwID.Load().(string)
	return s
}

func (g *Gateway) peer() *linkpb.GatewayPeer {
	return &linkpb.GatewayPeer{Id: g.peerID(), Boot: g.boot, Version: g.version}
}

// Attach implements gateway.GatewayLink: it serves d on the gateway's subjects
// and asks the backends to announce themselves so they learn this gateway's id.
func (g *Gateway) Attach(id string, d gateway.Downstream) (detach func()) {
	g.mu.Lock()
	g.downs[id] = d
	g.mu.Unlock()
	g.gwID.Store(id)
	var sub *nats.Subscription
	if s, err := g.nc.Subscribe(g.prefix+".gw."+token(id)+".*", g.serveDownstream(id)); err == nil {
		s.SetPendingLimits(-1, -1)
		sub = s
	}
	_ = g.nc.Publish(g.prefix+".who", nil)
	return func() {
		g.mu.Lock()
		if g.downs[id] == d {
			delete(g.downs, id)
		}
		g.mu.Unlock()
		if sub != nil {
			_ = sub.Unsubscribe()
		}
	}
}

func (g *Gateway) downstream(id string) (gateway.Downstream, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	d, ok := g.downs[id]
	return d, ok
}

func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		if slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func (g *Gateway) ack(m *nats.Msg, a *linkpb.HelloAck) {
	data, _ := proto.Marshal(a)
	respond(g.nc, m, data, nil)
}

func (g *Gateway) onHello(m *nats.Msg) {
	var hello linkpb.Hello
	if err := proto.Unmarshal(m.Data, &hello); err != nil || hello.Backend == nil {
		g.metrics.AttachResult(0, "error")
		g.ack(m, &linkpb.HelloAck{Reason: "malformed Hello", Gateway: g.peer()})
		return
	}
	info := linkconv.InfoBackendFromPB(hello.Backend)
	pr := min(info.Proto.Max, SupportedProto.Max)
	if info.Proto.Max == 0 || pr < info.Proto.Min || pr < SupportedProto.Min {
		g.metrics.AttachResult(info.Proto.Max, "proto_mismatch")
		g.ack(m, &linkpb.HelloAck{Gateway: g.peer(), Reason: fmt.Sprintf("no common protocol: backend %d-%d, gateway %d-%d",
			info.Proto.Min, info.Proto.Max, SupportedProto.Min, SupportedProto.Max)})
		return
	}
	if info.ID == "" {
		g.metrics.AttachResult(pr, "error")
		g.ack(m, &linkpb.HelloAck{Reason: "backend id required", Gateway: g.peer()})
		return
	}
	caps := intersect(supportedCaps, info.Caps)
	g.ack(m, &linkpb.HelloAck{Proto: pr, Caps: caps, Gateway: g.peer()})
	if info.Role == gateway.RoleCaller {
		return // callers take no inbound work
	}
	info.Caps = caps
	if info.Pool == "" {
		info.Pool = gateway.DefaultPool
	}
	interval := defaultHelloInterval
	if ms, err := time.ParseDuration(info.Labels[helloMSLabel] + "ms"); err == nil && ms > 0 {
		interval = ms
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	e := g.roster[info.ID]
	now := time.Now()
	if e != nil && e.draining {
		e.last = now // still alive, still finishing work
		return
	}
	if e != nil && (e.info.Pool != info.Pool || !slices.Equal(e.info.Caps, info.Caps) || e.proto != pr) {
		g.pools.Remove(e.member)
		e = nil
	}
	if e == nil {
		e = &entry{info: info, proto: pr}
		e.member = g.pools.Add(info, g.pubLocked(info.Pool))
		g.roster[info.ID] = e
		g.metrics.AttachResult(pr, "ok")
		g.refreshProtoLocked(info.Pool)
	}
	e.interval, e.last = interval, now
}

// pubLocked returns the publisher of pool name; g.mu is held.
func (g *Gateway) pubLocked(name string) *poolUpstream {
	p := g.pubs[name]
	if p == nil {
		p = &poolUpstream{g: g, pool: name}
		p.proto.Store(SupportedProto.Max)
		g.pubs[name] = p
	}
	return p
}

// refreshProtoLocked sets the protocol a pool's requests carry: what every
// member negotiated (a queue group cannot be addressed per member).
func (g *Gateway) refreshProtoLocked(pool string) {
	pr := SupportedProto.Max
	for _, e := range g.roster {
		if e.info.Pool == pool && !e.draining {
			pr = min(pr, e.proto)
		}
	}
	g.pubLocked(pool).proto.Store(pr)
}

// onDrain handles a backend's Drain: it takes no new work from now on. The
// DrainAck is sent once every request that backend was working on is answered,
// so after it the backend may stop as soon as its own bookkeeping is done.
func (g *Gateway) onDrain(m *nats.Msg) {
	var hello linkpb.Hello
	if err := proto.Unmarshal(m.Data, &hello); err != nil || hello.Backend == nil {
		return
	}
	id := hello.Backend.Id
	g.mu.Lock()
	e := g.roster[id]
	if e != nil && !e.draining {
		e.draining = true
		g.refreshProtoLocked(e.info.Pool)
	}
	g.mu.Unlock()
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if e != nil {
			ctx, cancel := context.WithTimeout(g.ctx, 10*time.Minute)
			defer cancel()
			_ = g.pools.Drain(ctx, e.member)
			for g.mux.busyWith(id) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		respond(g.nc, m, nil, nil)
	}()
}

// reaper drops backends whose Hello lapsed and fails the requests they held.
func (g *Gateway) reaper() {
	defer g.wg.Done()
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	rc := g.nc.Stats().Reconnects
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		if !g.nc.IsConnected() {
			continue // our own outage says nothing about the backends
		}
		reconnected := g.nc.Stats().Reconnects != rc
		if reconnected {
			rc = g.nc.Stats().Reconnects
			_ = g.nc.Publish(g.prefix+".who", nil)
		}
		var gone []string
		g.mu.Lock()
		for id, e := range g.roster {
			if reconnected {
				e.last = now // Hellos may have been missed while disconnected
				continue
			}
			if now.Sub(e.last) > leaseFactor*e.interval {
				g.pools.Remove(e.member)
				delete(g.roster, id)
				g.refreshProtoLocked(e.info.Pool)
				gone = append(gone, id)
			}
		}
		g.mu.Unlock()
		for _, id := range gone {
			g.mux.failWhere(func(exec string) bool { return exec == id }, errPeerLost)
		}
	}
}

// poolUpstream publishes to the queue group of one pool. Every roster member of
// the pool shares it: the broker, not the gateway, picks the backend.
type poolUpstream struct {
	g     *Gateway
	pool  string
	proto atomic.Uint32
}

func (p *poolUpstream) request(ctx context.Context, subject string, msg proto.Message) (*nats.Msg, string, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, "", err
	}
	return p.g.mux.request(ctx, subject, nats.Header{}, data, "")
}

var errUnsupportedByPeer = fmt.Errorf("%w: peer does not know this request", gateway.ErrUnsupported)

func (p *poolUpstream) Connect(ctx context.Context, req gateway.ConnectRequest) (gateway.ConnectReply, error) {
	m, _, err := p.request(ctx, p.g.prefix+".connect."+token(p.pool), linkconv.ConnectReqToPB(req))
	if err != nil {
		return gateway.ConnectReply{}, err
	}
	if m.Header.Get(hUnsupported) != "" {
		return gateway.ConnectReply{}, errUnsupportedByPeer
	}
	var rep linkpb.ConnectReply
	if err := proto.Unmarshal(m.Data, &rep); err != nil {
		return gateway.ConnectReply{}, err
	}
	return linkconv.ConnectRepFromPB(&rep), nil
}

func (p *poolUpstream) Handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, error) {
	start := time.Now()
	rep, outcome, exec, err := p.handle(ctx, env)
	p.g.metrics.UpstreamRequest(p.pool, p.g.versionOf(exec), env.Action, outcome, time.Since(start))
	return rep, err
}

func (p *poolUpstream) handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, string, string, error) {
	env.Proto = p.proto.Load()
	subject := p.g.prefix + ".in." + token(p.pool) + "." + token(string(env.Version)) + "." + token(env.Action)
	for attempt := 0; ; attempt++ {
		m, exec, err := p.request(ctx, subject, linkconv.EnvToPB(env))
		if err != nil {
			switch {
			case errors.Is(err, gateway.ErrNotDelivered):
				return gateway.Reply{}, "error", exec, err
			case errors.Is(err, errPeerLost), errors.Is(err, errConnLost):
				p.cancel(env.DeliveryID)
				return gateway.Reply{}, "stream_broken", exec, err
			case errors.Is(err, errNoAccept):
				p.cancel(env.DeliveryID)
				return gateway.Reply{}, "no_accept", exec, err
			case errors.Is(err, context.DeadlineExceeded):
				p.cancel(env.DeliveryID)
				return gateway.Reply{}, "timeout", exec, err
			}
			p.cancel(env.DeliveryID)
			return gateway.Reply{}, "error", exec, err
		}
		if m.Header.Get(hUnsupported) != "" {
			return gateway.Reply{}, "error", exec, errUnsupportedByPeer
		}
		var pb linkpb.Reply
		if err := proto.Unmarshal(m.Data, &pb); err != nil {
			return gateway.Reply{}, "error", exec, err
		}
		rep := linkconv.ReplyFromPB(&pb)
		switch {
		case rep.Nack:
			// The handler did not run (the backend is draining or full): the
			// broker will pick another member of the queue group.
			if attempt >= maxNackRetries {
				return rep, "nack", exec, nil
			}
			env.Attempt = max(env.Attempt, 1) + 1
			select {
			case <-ctx.Done():
				return gateway.Reply{}, "timeout", exec, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 5 * time.Millisecond):
			}
			continue
		case rep.Error != nil:
			return rep, "callerror_" + rep.Error.Code, exec, nil
		}
		return rep, "result", exec, nil
	}
}

// cancel tells the backends the answer to deliveryID is no longer needed.
func (p *poolUpstream) cancel(deliveryID string) {
	if deliveryID != "" {
		_ = p.g.nc.Publish(p.g.prefix+".cancel", []byte(deliveryID))
	}
}

func (p *poolUpstream) Notify(ctx context.Context, env gateway.Envelope) error {
	env.Proto = p.proto.Load()
	m, _, err := p.request(ctx, p.g.prefix+".notify."+token(p.pool), linkconv.EnvToPB(env))
	if err != nil {
		return err
	}
	if m.Header.Get(hUnsupported) != "" {
		return errUnsupportedByPeer
	}
	var ack linkpb.Ack
	if err := proto.Unmarshal(m.Data, &ack); err != nil {
		return err
	}
	if err := linkconv.ErrFromPB(ack.Error).Err(); err != nil {
		if errors.Is(err, gateway.ErrGatewayDraining) {
			// The backend is draining and did not take the event: safe to give
			// to another one.
			return fmt.Errorf("%w: %w", gateway.ErrNotDelivered, err)
		}
		return err
	}
	return nil
}

// serveDownstream serves CSMS-initiated work addressed to this gateway.
func (g *Gateway) serveDownstream(id string) nats.MsgHandler {
	return func(m *nats.Msg) {
		op := m.Subject[lastDot(m.Subject)+1:]
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			d, ok := g.downstream(id)
			switch op {
			case "call":
				g.serveCall(d, ok, m, false)
			case "send":
				g.serveCall(d, ok, m, true)
			case "control":
				g.serveControl(d, ok, m)
			case "locate":
				g.serveLocate(d, ok, m)
			default:
				g.metrics.Unsupported("recv", "op_"+op)
				respondUnsupported(g.nc, m, op)
			}
		}()
	}
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// versionOf is the build version of backend id, "" when unknown.
func (g *Gateway) versionOf(id string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.roster[id]; e != nil {
		return e.info.Version
	}
	return ""
}

func (g *Gateway) callerOf(m *nats.Msg) (pool, version string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.roster[m.Header.Get(hBackend)]; e != nil {
		return e.info.Pool, e.info.Version
	}
	return "", ""
}

func (g *Gateway) serveCall(d gateway.Downstream, ok bool, m *nats.Msg, isSend bool) {
	var req linkpb.CallRequest
	if err := proto.Unmarshal(m.Data, &req); err != nil {
		respondUnsupported(g.nc, m, "malformed")
		return
	}
	cr := linkconv.CallReqFromPB(&req)
	ctx, cancel := context.WithCancel(g.ctx)
	defer cancel()
	if !isSend {
		var rep gateway.CallReply
		var err error
		if !ok {
			rep = gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(ocppj.ErrNotConnected)}
		} else if rep, err = d.Call(ctx, cr); err != nil {
			rep = gateway.CallReply{Sent: gateway.SentUnknown, Error: gateway.EncodeError(err)}
		}
		pool, ver := g.callerOf(m)
		g.metrics.DownstreamCall(pool, ver, cr.Action, outcomeOfCall(rep, err))
		data, _ := proto.Marshal(linkconv.CallRepToPB(rep))
		respond(g.nc, m, data, nil)
		return
	}
	var err error = ocppj.ErrNotConnected
	if ok {
		err = d.Send(ctx, cr)
	}
	data, _ := proto.Marshal(&linkpb.Ack{Error: linkconv.ErrToPB(gateway.EncodeError(err))})
	respond(g.nc, m, data, nil)
}

func (g *Gateway) serveControl(d gateway.Downstream, ok bool, m *nats.Msg) {
	var req linkpb.ControlRequest
	if err := proto.Unmarshal(m.Data, &req); err != nil {
		respondUnsupported(g.nc, m, "malformed")
		return
	}
	var err error
	switch {
	case !ok:
		err = ocppj.ErrNotConnected
	case req.Op < uint32(gateway.ControlDisconnect) || req.Op > uint32(gateway.ControlSetAttrs):
		// A control op this build does not know: an answer, not a failure.
		g.metrics.Unsupported("recv", fmt.Sprintf("control_op_%d", req.Op))
		err = gateway.ErrUnsupported
	default:
		err = d.Control(g.ctx, gateway.ControlRequest{
			Target: linkconv.RefFromPB(req.Target), Op: gateway.ControlOp(req.Op), Reason: req.Reason, Attrs: req.Attrs,
		})
	}
	data, _ := proto.Marshal(&linkpb.Ack{Error: linkconv.ErrToPB(gateway.EncodeError(err))})
	respond(g.nc, m, data, nil)
}

// serveLocate answers which session this gateway holds for a charge point.
func (g *Gateway) serveLocate(d gateway.Downstream, ok bool, m *nats.Msg) {
	var req linkpb.LocateRequest
	if err := proto.Unmarshal(m.Data, &req); err != nil {
		respondUnsupported(g.nc, m, "malformed")
		return
	}
	rep := &linkpb.LocateReply{}
	if l, isLocator := d.(gateway.Locator); ok && isLocator {
		if loc, found := l.LocateSession(req.GetCpid()); found {
			rep = &linkpb.LocateReply{Found: true, Session: linkconv.RefToPB(loc.Ref), Version: string(loc.Version), Info: linkconv.InfoToPB(loc.Info)}
		}
	}
	data, _ := proto.Marshal(rep)
	respond(g.nc, m, data, nil)
}

func outcomeOfCall(rep gateway.CallReply, err error) string {
	switch {
	case err != nil:
		return "error"
	case rep.Error == nil:
		return "result"
	case rep.Error.Kind == gateway.ErrKindCallError:
		return "callerror"
	case rep.Sent == gateway.NotSent:
		return "not_sent"
	case rep.Error.Sentinel == "call_timeout":
		return "timeout"
	case rep.Sent == gateway.SentNoResponse:
		return "no_response"
	}
	return "error"
}

// Connect dials NATS with the options a link wants: no buffering across a
// reconnect (a request must not be delivered after its deadline), endless
// reconnects, and a ping interval that notices a dead broker in about six
// seconds. Later opts override these.
func Connect(url, name string, opts ...nats.Option) (*nats.Conn, error) {
	base := []nats.Option{
		nats.Name(name), nats.ReconnectBufSize(-1), nats.MaxReconnects(-1), nats.ReconnectWait(200 * time.Millisecond),
		nats.ReconnectJitter(100*time.Millisecond, 100*time.Millisecond), nats.PingInterval(2 * time.Second),
		nats.MaxPingsOutstanding(2), nats.RetryOnFailedConnect(true),
	}
	return nats.Connect(url, append(base, opts...)...)
}
