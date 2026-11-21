package linkgrpc

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/addons/link-grpc/linkconv"
	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/proto"
)

// Resolver lists the gateway addresses (host:port) a backend should be attached
// to. It is called periodically; addresses that appear are dialled, addresses
// that vanish are dropped.
type Resolver func(ctx context.Context) ([]string, error)

// StaticTargets is a Resolver for a fixed list.
func StaticTargets(addrs ...string) Resolver {
	return func(context.Context) ([]string, error) { return addrs, nil }
}

// DNSTargets resolves host (a headless Service name) to every address and pairs
// each with port, so a backend attaches to every gateway pod.
func DNSTargets(host, port string) Resolver {
	return func(ctx context.Context) ([]string, error) {
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(ips))
		for _, ip := range ips {
			out = append(out, net.JoinHostPort(ip, port))
		}
		return out, nil
	}
}

// Backend is the backend's end of the gRPC link: it dials every gateway, keeps
// an Attach stream to each, and implements gateway.BackendLink.
type Backend struct {
	resolve      Resolver
	dialOpts     []grpc.DialOption
	metrics      Metrics
	resolveEvery time.Duration
	drainTimeout time.Duration
	keepalive    keepalive.ClientParameters
	proto        gateway.ProtoRange // link versions spoken (SupportedProto)
	caps         []string           // capabilities offered (supportedCaps)

	mu       sync.RWMutex
	streams  map[string][]*stream // by gateway id; nStreams entries each when all are attached
	rr       atomic.Uint64
	nStreams int
}

// BackendOption configures a Backend.
type BackendOption func(*Backend)

// WithDialOptions adds grpc dial options (credentials above all).
func WithDialOptions(o ...grpc.DialOption) BackendOption {
	return func(b *Backend) { b.dialOpts = append(b.dialOpts, o...) }
}

// WithStreams sets how many Attach streams (each on its own connection) the
// backend keeps to every gateway (default 1). More streams spread the link's
// write and read work over more goroutines and sockets; one is enough until a
// single gateway-backend pair carries tens of thousands of messages per second.
func WithStreams(n int) BackendOption {
	return func(b *Backend) { b.nStreams = max(n, 1) }
}

// WithBackendMetrics sets the metrics sink.
func WithBackendMetrics(m Metrics) BackendOption { return func(b *Backend) { b.metrics = m } }

// WithResolveInterval sets how often the resolver is consulted (default 5 s).
func WithResolveInterval(d time.Duration) BackendOption {
	return func(b *Backend) { b.resolveEvery = d }
}

// WithDrainTimeout bounds how long Serve waits for work in flight after its
// context ends (default 30 s).
func WithDrainTimeout(d time.Duration) BackendOption { return func(b *Backend) { b.drainTimeout = d } }

// WithClientKeepalive sets the HTTP/2 keepalive that detects a dead gateway
// (default: ping after 10 s idle, 5 s to answer).
func WithClientKeepalive(p keepalive.ClientParameters) BackendOption {
	return func(b *Backend) { b.keepalive = p }
}

// NewBackend returns the backend end of the link for the gateways resolve names.
func NewBackend(resolve Resolver, opts ...BackendOption) *Backend {
	b := &Backend{
		resolve: resolve, metrics: NopMetrics{}, resolveEvery: 5 * time.Second, drainTimeout: 30 * time.Second,
		keepalive: keepalive.ClientParameters{Time: 10 * time.Second, Timeout: 5 * time.Second, PermitWithoutStream: true},
		streams:   make(map[string][]*stream), nStreams: 1, proto: SupportedProto, caps: supportedCaps,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Gateways returns the ids of the gateways currently attached.
func (b *Backend) Gateways() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.streams))
	for id := range b.streams {
		out = append(out, id)
	}
	return out
}

// stream is one attached gateway.
type stream struct {
	b       *Backend
	addr    string
	gw      *linkpb.GatewayPeer
	proto   uint32
	caps    []string
	st      linkpb.Link_AttachClient
	out     sendQueue[*linkpb.BackendFrame]
	unary   *pendingSet[*linkpb.GatewayFrame]
	closing atomic.Bool

	cancels  sync.Map // request id -> context.CancelFunc
	inflight atomic.Int64
	drained  chan struct{} // closed when the gateway acknowledged our Drain
	drainOne sync.Once
}

func (s *stream) send(f *linkpb.BackendFrame) error { return s.out.push(f) }

func (s *stream) initSend() {
	s.out.write = s.st.Send
	s.out.size = func(f *linkpb.BackendFrame) int { return proto.Size(f) }
	if slices.Contains(s.caps, gateway.CapBatch) {
		s.out.batch = func(fs []*linkpb.BackendFrame) error {
			return s.st.Send(&linkpb.BackendFrame{Body: &linkpb.BackendFrame_Batch{Batch: &linkpb.BackendBatch{Frames: fs}}})
		}
	}
}

func (b *Backend) stream(id string) (*stream, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if ss := b.streams[id]; len(ss) > 0 {
		return ss[int(b.rr.Add(1)%uint64(len(ss)))], true
	}
	if id == "" && len(b.streams) == 1 {
		for _, ss := range b.streams {
			return ss[int(b.rr.Add(1)%uint64(len(ss)))], true
		}
	}
	return nil, false
}

// Serve attaches to every gateway the resolver names, delivers their messages to
// up, and blocks until ctx ends. It then drains: a Drain frame tells each
// gateway to send no new work, work in flight is allowed to finish (up to the
// drain timeout), and the streams close.
func (b *Backend) Serve(ctx context.Context, up gateway.Upstream, info gateway.BackendInfo) error {
	info.Proto = b.proto
	info.Caps = b.caps
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()

	var wg sync.WaitGroup
	active := make(map[string]context.CancelFunc)
	refresh := func() {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		addrs, err := b.resolve(rctx)
		if err != nil {
			return
		}
		want := make(map[string]bool, len(addrs))
		for _, a := range addrs {
			want[a] = true
			if _, ok := active[a]; !ok {
				tctx, tcancel := context.WithCancel(runCtx)
				active[a] = tcancel
				for range b.nStreams {
					wg.Add(1)
					go func() {
						defer wg.Done()
						b.runTarget(tctx, a, up, info)
					}()
				}
			}
		}
		for a, c := range active {
			if !want[a] {
				c()
				delete(active, a)
			}
		}
	}
	refresh()
	t := time.NewTicker(b.resolveEvery)
	defer t.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-t.C:
			refresh()
		}
	}
	// Drain: stop new work everywhere, wait for what is in flight.
	b.mu.RLock()
	ss := make([]*stream, 0, len(b.streams))
	for _, l := range b.streams {
		ss = append(ss, l...)
	}
	b.mu.RUnlock()
	for _, s := range ss {
		s.closing.Store(true)
		_ = s.send(&linkpb.BackendFrame{Body: &linkpb.BackendFrame_Drain{Drain: &linkpb.Drain{}}})
	}
	done := make(chan struct{})
	go func() {
		for _, s := range ss {
			// Wait for the gateway's DrainAck (nothing more will be sent on the
			// stream), then for our own work in flight.
			select {
			case <-s.drained:
			case <-time.After(b.drainTimeout):
			}
			for s.inflight.Load() > 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
		close(done)
	}()
	var err error
	select {
	case <-done:
	case <-time.After(b.drainTimeout):
		err = fmt.Errorf("linkgrpc: drain timed out with work in flight")
	}
	cancelRun()
	wg.Wait()
	return err
}

// runTarget keeps one stream to addr alive until ctx ends.
func (b *Backend) runTarget(ctx context.Context, addr string, up gateway.Upstream, info gateway.BackendInfo) {
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		started := time.Now()
		err := b.attachOnce(ctx, addr, up, info)
		if ctx.Err() != nil {
			return
		}
		_ = err
		if time.Since(started) > 10*time.Second {
			backoff = 100 * time.Millisecond
		}
		d := backoff/2 + time.Duration(rand.Int64N(int64(backoff)))
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

func (b *Backend) attachOnce(ctx context.Context, addr string, up gateway.Upstream, info gateway.BackendInfo) error {
	opts := append([]grpc.DialOption{grpc.WithKeepaliveParams(b.keepalive)}, b.dialOpts...)
	cc, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st, err := linkpb.NewLinkClient(cc).Attach(sctx)
	if err != nil {
		return err
	}
	if err := st.Send(&linkpb.BackendFrame{Body: &linkpb.BackendFrame_Hello{Hello: &linkpb.Hello{Backend: linkconv.InfoBackendToPB(info)}}}); err != nil {
		return err
	}
	f, err := st.Recv()
	if err != nil {
		return err
	}
	ack := f.GetHelloAck()
	if ack == nil {
		return errors.New("linkgrpc: gateway did not answer Hello")
	}
	if ack.Proto == 0 {
		b.metrics.AttachResult(0, "proto_mismatch")
		return fmt.Errorf("linkgrpc: gateway %s rejected attach: %s", addr, ack.Reason)
	}
	b.metrics.AttachResult(ack.Proto, "ok")
	s := &stream{b: b, addr: addr, gw: ack.Gateway, proto: ack.Proto, caps: ack.Caps, st: st, unary: newPendingSet[*linkpb.GatewayFrame](), drained: make(chan struct{})}
	s.initSend()
	gwID := ack.Gateway.GetId()
	b.mu.Lock()
	b.streams[gwID] = append(b.streams[gwID], s)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if l := slices.DeleteFunc(b.streams[gwID], func(x *stream) bool { return x == s }); len(l) > 0 {
			b.streams[gwID] = l
		} else {
			delete(b.streams, gwID)
		}
		b.mu.Unlock()
		s.unary.close()
		s.drainOne.Do(func() { close(s.drained) }) // a dead stream needs no acknowledgement
	}()
	return s.readLoop(sctx, up, info)
}

func (s *stream) readLoop(ctx context.Context, up gateway.Upstream, info gateway.BackendInfo) error {
	for {
		f, err := s.st.Recv()
		if err != nil {
			return err
		}
		if bb, ok := f.Body.(*linkpb.GatewayFrame_Batch); ok && s.out.batch != nil {
			for _, sub := range bb.Batch.GetFrames() {
				s.dispatch(ctx, up, sub)
			}
			continue
		}
		s.dispatch(ctx, up, f)
	}
}

func (s *stream) dispatch(ctx context.Context, up gateway.Upstream, f *linkpb.GatewayFrame) {
	switch b := f.Body.(type) {
	case *linkpb.GatewayFrame_CallReply, *linkpb.GatewayFrame_SendAck, *linkpb.GatewayFrame_ControlAck, *linkpb.GatewayFrame_LocateReply, *linkpb.GatewayFrame_Unsupported:
		if u, ok := b.(*linkpb.GatewayFrame_Unsupported); ok {
			s.b.metrics.Unsupported("sent", u.Unsupported.GetWhat())
		}
		s.unary.resolve(f.Id, f)
	case *linkpb.GatewayFrame_Connect:
		s.spawn(func() {
			rep, err := up.Connect(ctx, linkconv.ConnectReqFromPB(b.Connect))
			if err != nil {
				// The backend could not decide: refuse rather than guess.
				rep = gateway.ConnectReply{Reject: &gateway.Rejection{Status: 503, Message: "service unavailable"}}
			}
			_ = s.send(&linkpb.BackendFrame{Id: f.Id, Body: &linkpb.BackendFrame_ConnectReply{ConnectReply: linkconv.ConnectRepToPB(rep)}})
		})
	case *linkpb.GatewayFrame_Handle:
		s.spawn(func() { s.handle(ctx, up, f.Id, b.Handle) })
	case *linkpb.GatewayFrame_Notify:
		s.spawn(func() {
			var e *linkpb.WireError
			if s.closing.Load() && gateway.Kind(b.Notify.Kind) != gateway.KindSessionClosed {
				e = linkconv.ErrToPB(gateway.EncodeError(gateway.ErrGatewayDraining))
			} else {
				e = linkconv.ErrToPB(gateway.EncodeError(up.Notify(ctx, linkconv.EnvFromPB(b.Notify))))
			}
			_ = s.send(&linkpb.BackendFrame{Id: f.Id, Body: &linkpb.BackendFrame_NotifyAck{NotifyAck: &linkpb.Ack{Error: e}}})
		})
	case *linkpb.GatewayFrame_DrainAck:
		s.drainOne.Do(func() { close(s.drained) })
	case *linkpb.GatewayFrame_Cancel:
		if c, ok := s.cancels.Load(f.Id); ok {
			c.(context.CancelFunc)()
		}
	default:
		s.b.metrics.Unsupported("recv", fmt.Sprintf("%T", b))
		_ = s.send(&linkpb.BackendFrame{Id: f.Id, Body: &linkpb.BackendFrame_Unsupported{Unsupported: &linkpb.Unsupported{What: fmt.Sprintf("%T", b)}}})
	}
}

func (s *stream) spawn(fn func()) {
	s.inflight.Add(1)
	go func() {
		defer s.inflight.Add(-1)
		var reserve [handlerStack]byte
		reserveStack(&reserve)
		fn()
	}()
}

func (s *stream) handle(ctx context.Context, up gateway.Upstream, id uint64, pb *linkpb.Envelope) {
	if s.closing.Load() {
		// Draining: the handler did not run, so the gateway may redispatch.
		_ = s.send(&linkpb.BackendFrame{Id: id, Body: &linkpb.BackendFrame_Reply{Reply: &linkpb.Reply{Nack: true}}})
		return
	}
	env := linkconv.EnvFromPB(pb)
	hctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancels.Store(id, context.CancelFunc(cancel))
	defer s.cancels.Delete(id)
	rep, err := up.Handle(hctx, env)
	if err != nil {
		rep = gateway.Reply{Error: &gateway.WireError{
			Kind: gateway.ErrKindCallError, Code: "InternalError", Description: "internal error",
		}}
	}
	_ = s.send(&linkpb.BackendFrame{Id: id, Body: &linkpb.BackendFrame_Reply{Reply: linkconv.ReplyToPB(rep)}})
}

// request sends f to the gateway and waits for the answer.
func (s *stream) request(ctx context.Context, f *linkpb.BackendFrame) (*linkpb.GatewayFrame, bool, error) {
	id, ch, ok := s.unary.add()
	if !ok {
		return nil, false, errStreamBroken
	}
	f.Id = id
	if err := s.send(f); err != nil {
		s.unary.drop(id)
		return nil, false, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, true, errStreamBroken
		}
		return r, true, nil
	case <-ctx.Done():
		s.unary.drop(id)
		return nil, true, ctx.Err()
	}
}

// Call implements gateway.Downstream: it routes to the gateway named by
// req.Target.GatewayID.
func (b *Backend) Call(ctx context.Context, req gateway.CallRequest) (gateway.CallReply, error) {
	s, ok := b.stream(req.Target.GatewayID)
	if !ok {
		return gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(ocppj.ErrNotConnected)}, nil
	}
	r, sent, err := s.request(ctx, &linkpb.BackendFrame{Body: &linkpb.BackendFrame_Call{Call: linkconv.CallReqToPB(req)}})
	if err != nil {
		out := gateway.CallReply{Sent: gateway.NotSent}
		if sent {
			// Written, no answer: the charger may have executed it.
			out.Sent = gateway.SentNoResponse
		}
		return out, err
	}
	switch body := r.Body.(type) {
	case *linkpb.GatewayFrame_CallReply:
		return linkconv.CallRepFromPB(body.CallReply), nil
	case *linkpb.GatewayFrame_Unsupported:
		return gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(gateway.ErrUnsupported)}, nil
	}
	return gateway.CallReply{}, errors.New("linkgrpc: unexpected answer to Call")
}

// Send implements gateway.Downstream.
func (b *Backend) Send(ctx context.Context, req gateway.CallRequest) error {
	s, ok := b.stream(req.Target.GatewayID)
	if !ok {
		return ocppj.ErrNotConnected
	}
	r, _, err := s.request(ctx, &linkpb.BackendFrame{Body: &linkpb.BackendFrame_Send{Send: linkconv.CallReqToPB(req)}})
	if err != nil {
		return err
	}
	if body, ok := r.Body.(*linkpb.GatewayFrame_SendAck); ok {
		return linkconv.ErrFromPB(body.SendAck.GetError()).Err()
	}
	return gateway.ErrUnsupported
}

// Control implements gateway.Downstream.
func (b *Backend) Control(ctx context.Context, req gateway.ControlRequest) error {
	s, ok := b.stream(req.Target.GatewayID)
	if !ok {
		return ocppj.ErrNotConnected
	}
	r, _, err := s.request(ctx, &linkpb.BackendFrame{Body: &linkpb.BackendFrame_Control{Control: &linkpb.ControlRequest{
		Target: linkconv.RefToPB(req.Target), Op: uint32(req.Op), Reason: req.Reason, Attrs: req.Attrs,
	}}})
	if err != nil {
		return err
	}
	if body, ok := r.Body.(*linkpb.GatewayFrame_ControlAck); ok {
		return linkconv.ErrFromPB(body.ControlAck.GetError()).Err()
	}
	return gateway.ErrUnsupported
}

// Locate asks every attached gateway that supports it which session it holds for
// cpID, so a backend without a session registry can still address a charger it
// has not seen a message from. It returns the first answer that has one. A
// gateway that does not know the question (an older build) counts as not found.
func (b *Backend) Locate(ctx context.Context, cpID string) (gateway.Located, bool, error) {
	b.mu.RLock()
	ss := make([]*stream, 0, len(b.streams))
	for _, l := range b.streams {
		if s := l[0]; slices.Contains(s.caps, gateway.CapLocate) { // one stream per gateway is enough
			ss = append(ss, s)
		}
	}
	b.mu.RUnlock()
	if len(ss) == 0 {
		return gateway.Located{}, false, nil
	}
	type result struct {
		loc gateway.Located
		ok  bool
		err error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	res := make(chan result, len(ss))
	for _, s := range ss {
		go func() {
			r, _, err := s.request(ctx, &linkpb.BackendFrame{Body: &linkpb.BackendFrame_Locate{Locate: &linkpb.LocateRequest{Cpid: cpID}}})
			if err != nil {
				res <- result{err: err}
				return
			}
			lr, isReply := r.Body.(*linkpb.GatewayFrame_LocateReply)
			if !isReply || !lr.LocateReply.GetFound() {
				res <- result{}
				return
			}
			l := lr.LocateReply
			res <- result{ok: true, loc: gateway.Located{
				Ref: linkconv.RefFromPB(l.Session), Version: ocppj.Version(l.Version), Info: linkconv.InfoFromPB(l.Info),
			}}
		}()
	}
	var firstErr error
	for range ss {
		r := <-res
		switch {
		case r.ok:
			return r.loc, true, nil
		case r.err != nil && firstErr == nil:
			firstErr = r.err
		}
	}
	// Every gateway answered "not here", or some failed: an error only when
	// nobody could say.
	return gateway.Located{}, false, firstErr
}
