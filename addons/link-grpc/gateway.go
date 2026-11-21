package linkgrpc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/addons/link-grpc/linkconv"
	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"google.golang.org/grpc"
)

// SupportedProto is the link protocol range this build speaks. Every release
// speaks its own protocol and the previous one.
var SupportedProto = gateway.ProtoRange{Min: 1, Max: 1}

// supportedCaps are the capabilities this build advertises (both ends).
var supportedCaps = []string{
	gateway.CapNack, gateway.CapOrphan, gateway.CapAttrsSet, gateway.CapRouteV1, gateway.CapSend21, gateway.CapCancel, gateway.CapLocate, gateway.CapBatch,
}

const (
	defaultHelloTimeout = 10 * time.Second
	defaultMaxInFlight  = 4096
)

// Gateway is the gateway's end of the gRPC link. It implements
// gateway.GatewayLink (hand it to csms.NewGatewayServer) and serves the Attach
// streams of backends once registered on a grpc.Server.
type Gateway struct {
	pools        *gateway.PoolSet
	observer     gateway.RouteObserver
	version      string
	metrics      Metrics
	helloTimeout time.Duration
	maxInFlight  int
	gwID         atomic.Value // string: id of the attached csms gateway server
	boot         string
	proto        gateway.ProtoRange // link versions spoken (SupportedProto)
	caps         []string           // capabilities offered (supportedCaps)

	mu    sync.RWMutex
	downs map[string]gateway.Downstream
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

// WithRouteObserver sets the gateway.RouteObserver told about fallbacks and
// outcomes of routed messages (the routepolicy engine).
func WithRouteObserver(o gateway.RouteObserver) GatewayOption {
	return func(g *Gateway) { g.observer = o }
}

// WithMaxInFlight caps the requests in flight per backend stream (default 4096).
func WithMaxInFlight(n int) GatewayOption { return func(g *Gateway) { g.maxInFlight = n } }

// NewGateway returns the gateway end of the link.
func NewGateway(opts ...GatewayOption) *Gateway {
	g := &Gateway{
		pools: gateway.NewPoolSet(0), metrics: NopMetrics{}, helloTimeout: defaultHelloTimeout,
		maxInFlight: defaultMaxInFlight, proto: SupportedProto, caps: supportedCaps, downs: make(map[string]gateway.Downstream),
	}
	for _, o := range opts {
		o(g)
	}
	g.pools.SetObserver(g.observer)
	return g
}

// Register serves the Attach stream on s.
func (g *Gateway) Register(s grpc.ServiceRegistrar) { linkpb.RegisterLinkServer(s, &service{g: g}) }

// Pools reports the attached backends by pool.
func (g *Gateway) Pools() []gateway.PoolStatus { return g.pools.Status() }

// Unhandled counts, per pool and action, the messages a router sent to a pool in
// which no member serves the action (see gateway.PoolSet.Unhandled).
func (g *Gateway) Unhandled() map[[2]string]uint64 { return g.pools.Unhandled() }

// Available reports whether pool name has an attached, non-draining backend.
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
	availableBefore := env.Kind != gateway.KindOrphanResult || env.Route == nil || g.pools.Available(env.Route.Pool)
	err := g.pools.Notify(ctx, env)
	if env.Kind == gateway.KindOrphanResult {
		pool := gateway.DefaultPool
		if env.Route != nil && env.Route.Pool != "" {
			pool = env.Route.Pool
		}
		outcome := "delivered"
		switch {
		case err != nil:
			outcome = "dropped"
		case !availableBefore:
			outcome = "fallback"
		}
		ReportOrphanResult(g.metrics, pool, outcome)
	}
	return err
}

// Attach implements gateway.GatewayLink: it makes the gateway's Downstream
// reachable by backends under id.
func (g *Gateway) Attach(id string, d gateway.Downstream) (detach func()) {
	g.mu.Lock()
	g.downs[id] = d
	g.mu.Unlock()
	g.gwID.Store(id)
	return func() {
		g.mu.Lock()
		if g.downs[id] == d {
			delete(g.downs, id)
		}
		g.mu.Unlock()
	}
}

func (g *Gateway) downstream(id string) (gateway.Downstream, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if d, ok := g.downs[id]; ok {
		return d, true
	}
	if id == "" && len(g.downs) == 1 {
		for _, d := range g.downs {
			return d, true
		}
	}
	return nil, false
}

func (g *Gateway) peerID() string {
	s, _ := g.gwID.Load().(string)
	return s
}

type service struct {
	linkpb.UnimplementedLinkServer
	g *Gateway
}

func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		for _, y := range b {
			if x == y {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

// Attach serves one backend stream.
func (s *service) Attach(stream linkpb.Link_AttachServer) error {
	g := s.g
	first := make(chan *linkpb.BackendFrame, 1)
	go func() {
		f, err := stream.Recv()
		if err != nil {
			close(first)
			return
		}
		first <- f
	}()
	var hello *linkpb.Hello
	select {
	case f, ok := <-first:
		if !ok || f.GetHello() == nil {
			g.metrics.AttachResult(0, "error")
			return errors.New("linkgrpc: first frame must be Hello")
		}
		hello = f.GetHello()
	case <-time.After(g.helloTimeout):
		g.metrics.AttachResult(0, "hello_timeout")
		return errors.New("linkgrpc: no Hello")
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
	if hello.Backend == nil {
		g.metrics.AttachResult(0, "error")
		return errors.New("linkgrpc: Hello without backend info")
	}
	info := linkconv.InfoBackendFromPB(hello.Backend)
	if info.Pool == "" {
		info.Pool = gateway.DefaultPool // metric labels and caller pools name it
	}
	proto := min(info.Proto.Max, g.proto.Max)
	if info.Proto.Max == 0 || proto < info.Proto.Min || proto < g.proto.Min {
		g.metrics.AttachResult(info.Proto.Max, "proto_mismatch")
		_ = stream.Send(&linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_HelloAck{HelloAck: &linkpb.HelloAck{
			Reason: fmt.Sprintf("no common protocol: backend %d-%d, gateway %d-%d",
				info.Proto.Min, info.Proto.Max, g.proto.Min, g.proto.Max),
			Gateway: &linkpb.GatewayPeer{Id: g.peerID(), Boot: g.boot, Version: g.version},
		}}})
		return nil
	}
	caps := intersect(g.caps, info.Caps)
	if err := stream.Send(&linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_HelloAck{HelloAck: &linkpb.HelloAck{
		Proto: proto, Caps: caps, Gateway: &linkpb.GatewayPeer{Id: g.peerID(), Boot: g.boot, Version: g.version},
	}}}); err != nil {
		return err
	}
	g.metrics.AttachResult(proto, "ok")
	info.Caps = caps // what the stream may be used for
	p := &peer{
		g: g, stream: stream, info: info, proto: proto,
		unary: newPendingSet[*linkpb.BackendFrame](), sem: make(chan struct{}, g.maxInFlight),
	}
	if info.ID == "" {
		info.ID = fmt.Sprintf("stream-%p", p)
		p.info.ID = info.ID
	}
	p.initSend()
	p.member = g.pools.Add(p.info, p)
	defer func() {
		g.pools.Remove(p.member)
		p.unary.close()
	}()
	return p.readLoop()
}

// peer is one attached backend stream: the gateway.Upstream the pools send to,
// and the server of the backend's calls.
type peer struct {
	g      *Gateway
	stream linkpb.Link_AttachServer
	info   gateway.BackendInfo
	proto  uint32
	member *gateway.Member

	out   sendQueue[*linkpb.GatewayFrame]
	unary *pendingSet[*linkpb.BackendFrame]
	sem   chan struct{}
}

func (p *peer) send(f *linkpb.GatewayFrame) error { return p.out.push(f) }

func (p *peer) initSend() {
	p.out.write = p.stream.Send
	if slices.Contains(p.info.Caps, gateway.CapBatch) {
		p.out.batch = func(fs []*linkpb.GatewayFrame) error {
			return p.stream.Send(&linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_Batch{Batch: &linkpb.GatewayBatch{Frames: fs}}})
		}
	}
}

func (p *peer) readLoop() error {
	for {
		f, err := p.stream.Recv()
		if err != nil {
			return nil // the stream ended; deferred cleanup fails pending requests
		}
		if bb, ok := f.Body.(*linkpb.BackendFrame_Batch); ok && p.out.batch != nil {
			for _, sub := range bb.Batch.GetFrames() {
				p.dispatch(sub)
			}
			continue
		}
		p.dispatch(f)
	}
}

func (p *peer) dispatch(f *linkpb.BackendFrame) {
	switch b := f.Body.(type) {
	case *linkpb.BackendFrame_Reply, *linkpb.BackendFrame_ConnectReply, *linkpb.BackendFrame_NotifyAck, *linkpb.BackendFrame_Unsupported:
		if u, ok := b.(*linkpb.BackendFrame_Unsupported); ok {
			p.g.metrics.Unsupported("sent", u.Unsupported.GetWhat())
		}
		if !p.unary.resolve(f.Id, f) {
			if _, isReply := b.(*linkpb.BackendFrame_Reply); isReply {
				ReportLateReply(p.g.metrics, p.info.Pool)
			}
		}
	case *linkpb.BackendFrame_Call:
		go p.serveCall(f.Id, b.Call, false)
	case *linkpb.BackendFrame_Send:
		go p.serveCall(f.Id, b.Send, true)
	case *linkpb.BackendFrame_Control:
		go p.serveControl(f.Id, b.Control)
	case *linkpb.BackendFrame_Locate:
		go p.serveLocate(f.Id, b.Locate)
	case *linkpb.BackendFrame_Drain:
		// No new work; the stream stays up until the backend closes it.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := p.g.pools.Drain(ctx, p.member); err == nil {
				_ = p.send(&linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_DrainAck{DrainAck: &linkpb.DrainAck{}}})
			}
		}()
	default:
		// Unknown body (a newer peer) or a repeated Hello: an answer, not a failure.
		p.g.metrics.Unsupported("recv", fmt.Sprintf("%T", b))
		_ = p.send(&linkpb.GatewayFrame{Id: f.Id, Body: &linkpb.GatewayFrame_Unsupported{
			Unsupported: &linkpb.Unsupported{What: fmt.Sprintf("%T", b)},
		}})
	}
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

func (p *peer) serveCall(id uint64, req *linkpb.CallRequest, isSend bool) {
	d, ok := p.g.downstream(req.GetTarget().GetGatewayId())
	ctx := p.stream.Context()
	cr := linkconv.CallReqFromPB(req)
	cr.CallerPool, cr.CallerVersion = p.info.Pool, p.info.Version
	if !isSend {
		var rep gateway.CallReply
		var err error
		if !ok {
			rep = gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(ocppj.ErrNotConnected)}
		} else {
			rep, err = d.Call(ctx, cr)
			if err != nil {
				rep = gateway.CallReply{Sent: gateway.SentUnknown, Error: gateway.EncodeError(err)}
			}
		}
		p.g.metrics.DownstreamCall(p.info.Pool, p.info.Version, cr.Action, outcomeOfCall(rep, err))
		_ = p.send(&linkpb.GatewayFrame{Id: id, Body: &linkpb.GatewayFrame_CallReply{CallReply: linkconv.CallRepToPB(rep)}})
		return
	}
	var err error = ocppj.ErrNotConnected
	if ok {
		err = d.Send(ctx, cr)
	}
	_ = p.send(&linkpb.GatewayFrame{Id: id, Body: &linkpb.GatewayFrame_SendAck{SendAck: &linkpb.Ack{Error: linkconv.ErrToPB(gateway.EncodeError(err))}}})
}

func (p *peer) serveControl(id uint64, req *linkpb.ControlRequest) {
	var err error
	d, ok := p.g.downstream(req.GetTarget().GetGatewayId())
	switch {
	case !ok:
		err = ocppj.ErrNotConnected
	case req.Op < uint32(gateway.ControlDisconnect) || req.Op > uint32(gateway.ControlSetAttrs):
		// A control op this build does not know: an answer, not a failure.
		p.g.metrics.Unsupported("recv", fmt.Sprintf("control_op_%d", req.Op))
		err = gateway.ErrUnsupported
	default:
		err = d.Control(p.stream.Context(), gateway.ControlRequest{
			Target: linkconv.RefFromPB(req.Target), Op: gateway.ControlOp(req.Op), Reason: req.Reason, Attrs: req.Attrs,
		})
	}
	_ = p.send(&linkpb.GatewayFrame{Id: id, Body: &linkpb.GatewayFrame_ControlAck{ControlAck: &linkpb.Ack{Error: linkconv.ErrToPB(gateway.EncodeError(err))}}})
}

// serveLocate answers a backend's question which session this gateway holds
// for a charge point, through the attached server's gateway.Locator.
func (p *peer) serveLocate(id uint64, req *linkpb.LocateRequest) {
	rep := &linkpb.LocateReply{}
	if d, ok := p.g.downstream(""); ok {
		if l, ok := d.(gateway.Locator); ok {
			if loc, found := l.LocateSession(req.GetCpid()); found {
				rep = &linkpb.LocateReply{Found: true, Session: linkconv.RefToPB(loc.Ref), Version: string(loc.Version), Info: linkconv.InfoToPB(loc.Info)}
			}
		}
	}
	_ = p.send(&linkpb.GatewayFrame{Id: id, Body: &linkpb.GatewayFrame_LocateReply{LocateReply: rep}})
}

// request sends body to the backend and waits for its answer.
func (p *peer) request(ctx context.Context, body *linkpb.GatewayFrame) (*linkpb.BackendFrame, error) {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", gateway.ErrNotDelivered, ctx.Err())
	}
	id, ch, ok := p.unary.add()
	if !ok {
		return nil, fmt.Errorf("%w: stream closed", gateway.ErrNotDelivered)
	}
	body.Id = id
	if err := p.send(body); err != nil {
		p.unary.drop(id)
		return nil, fmt.Errorf("%w: %w", gateway.ErrNotDelivered, err)
	}
	select {
	case f, ok := <-ch:
		if !ok {
			return nil, errStreamBroken
		}
		return f, nil
	case <-ctx.Done():
		p.unary.drop(id)
		_ = p.send(&linkpb.GatewayFrame{Id: id, Body: &linkpb.GatewayFrame_Cancel{Cancel: &linkpb.Cancel{}}})
		return nil, ctx.Err()
	}
}

var errUnsupportedByPeer = fmt.Errorf("%w: peer does not know this frame", gateway.ErrUnsupported)

// Connect implements gateway.Upstream on one stream.
func (p *peer) Connect(ctx context.Context, req gateway.ConnectRequest) (gateway.ConnectReply, error) {
	f, err := p.request(ctx, &linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_Connect{Connect: linkconv.ConnectReqToPB(req)}})
	if err != nil {
		return gateway.ConnectReply{}, err
	}
	switch b := f.Body.(type) {
	case *linkpb.BackendFrame_ConnectReply:
		return linkconv.ConnectRepFromPB(b.ConnectReply), nil
	case *linkpb.BackendFrame_Unsupported:
		return gateway.ConnectReply{}, errUnsupportedByPeer
	}
	return gateway.ConnectReply{}, errors.New("linkgrpc: unexpected answer to Connect")
}

// Handle implements gateway.Upstream on one stream.
func (p *peer) Handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, error) {
	start := time.Now()
	f, err := p.request(ctx, &linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_Handle{Handle: linkconv.EnvToPB(env)}})
	var rep gateway.Reply
	outcome := "result"
	switch {
	case err != nil:
		err = fmt.Errorf("%w", err)
		switch {
		case errors.Is(err, errStreamBroken):
			outcome = "stream_broken"
		case errors.Is(err, context.DeadlineExceeded):
			outcome = "timeout"
		default:
			outcome = "error"
		}
	default:
		switch b := f.Body.(type) {
		case *linkpb.BackendFrame_Reply:
			rep = linkconv.ReplyFromPB(b.Reply)
			switch {
			case rep.Nack:
				outcome = "nack"
			case rep.Error != nil:
				outcome = "callerror_" + rep.Error.Code
			}
		case *linkpb.BackendFrame_Unsupported:
			err, outcome = errUnsupportedByPeer, "error"
		default:
			err, outcome = errors.New("linkgrpc: unexpected answer to Handle"), "error"
		}
	}
	p.g.metrics.UpstreamRequest(p.info.Pool, p.info.Version, env.Action, outcome, time.Since(start))
	return rep, err
}

// Notify implements gateway.Upstream on one stream.
func (p *peer) Notify(ctx context.Context, env gateway.Envelope) error {
	f, err := p.request(ctx, &linkpb.GatewayFrame{Body: &linkpb.GatewayFrame_Notify{Notify: linkconv.EnvToPB(env)}})
	if err != nil {
		return err
	}
	switch b := f.Body.(type) {
	case *linkpb.BackendFrame_NotifyAck:
		if err := linkconv.ErrFromPB(b.NotifyAck.GetError()).Err(); err != nil {
			if errors.Is(err, gateway.ErrGatewayDraining) {
				// The backend is draining and did not take the event: safe to
				// give to another one.
				return fmt.Errorf("%w: %w", gateway.ErrNotDelivered, err)
			}
			return err
		}
		return nil
	case *linkpb.BackendFrame_Unsupported:
		return errUnsupportedByPeer
	}
	return errors.New("linkgrpc: unexpected answer to Notify")
}
