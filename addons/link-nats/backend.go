package linknats

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/shiv3/gocpp/addons/link-grpc/linkconv"
	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"google.golang.org/protobuf/proto"
)

const defaultMaxInFlight = 4096

// Backend is the backend's end of the NATS link: it subscribes to its pool's
// queue group, announces itself to the gateways, and implements
// gateway.BackendLink.
type Backend struct {
	nc           *nats.Conn
	prefix       string
	metrics      Metrics
	interval     time.Duration
	drainTimeout time.Duration
	maxInFlight  int
	id           string
	mux          *mux

	mu  sync.Mutex
	gws map[string]*gwPeer
}

// gwPeer is a gateway that answered a Hello.
type gwPeer struct {
	last  time.Time
	proto uint32
	caps  []string
	boot  string
}

// BackendOption configures a Backend.
type BackendOption func(*Backend)

// WithBackendMetrics sets the metrics sink.
func WithBackendMetrics(m Metrics) BackendOption { return func(b *Backend) { b.metrics = m } }

// WithBackendPrefix sets the subject prefix; it must match the gateways'.
func WithBackendPrefix(p string) BackendOption { return func(b *Backend) { b.prefix = p } }

// WithHelloInterval sets how often the backend announces itself (default 5 s).
// Gateways drop a backend after three missed announcements, and a backend drops
// a gateway after three missed answers, so this is the failure detection time
// of the link (about three intervals).
func WithHelloInterval(d time.Duration) BackendOption { return func(b *Backend) { b.interval = d } }

// WithDrainTimeout bounds how long Serve waits for work in flight after its
// context ends (default 30 s).
func WithDrainTimeout(d time.Duration) BackendOption { return func(b *Backend) { b.drainTimeout = d } }

// WithMaxInFlight caps the requests a backend works on at once (default 4096);
// the excess is Nacked and goes to another backend.
func WithMaxInFlight(n int) BackendOption { return func(b *Backend) { b.maxInFlight = n } }

// NewBackend returns the backend end of the link over nc. The caller owns nc;
// see NewGateway for the recommended connection options. Close releases what
// NewBackend allocated.
func NewBackend(nc *nats.Conn, opts ...BackendOption) (*Backend, error) {
	b := &Backend{
		nc: nc, prefix: DefaultPrefix, metrics: NopMetrics{}, interval: defaultHelloInterval,
		drainTimeout: 30 * time.Second, maxInFlight: defaultMaxInFlight, gws: make(map[string]*gwPeer),
		id: "be-" + nuid.Next(),
	}
	for _, o := range opts {
		o(b)
	}
	var err error
	if b.mux, err = newMux(nc, b.prefix, b.id); err != nil {
		return nil, err
	}
	return b, nil
}

// Close releases the backend's reply subscription; it does not close the NATS
// connection. Call it after Serve has returned.
func (b *Backend) Close() { b.mux.close() }

// Gateways returns the ids of the gateways that answered a Hello recently.
func (b *Backend) Gateways() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for id, g := range b.gws {
		if time.Since(g.last) <= leaseFactor*b.interval {
			out = append(out, id)
		}
	}
	return out
}

// resolveGateway maps a target gateway id to a live one ("" = the only one).
func (b *Backend) resolveGateway(id string) (string, bool) {
	if id != "" {
		return id, true
	}
	live := b.Gateways()
	if len(live) == 1 {
		return live[0], true
	}
	return "", false
}

func (b *Backend) onAck(m *nats.Msg) {
	var a linkpb.HelloAck
	if err := proto.Unmarshal(m.Data, &a); err != nil {
		return
	}
	if a.Proto == 0 {
		b.metrics.AttachResult(0, "proto_mismatch")
		return
	}
	id := a.Gateway.GetId()
	if id == "" {
		return // the gateway is not attached yet; it asks for Hellos when it is
	}
	b.mu.Lock()
	g := b.gws[id]
	if g == nil || g.boot != a.Gateway.GetBoot() {
		g = &gwPeer{}
		b.gws[id] = g
		b.metrics.AttachResult(a.Proto, "ok")
	}
	g.last, g.proto, g.caps, g.boot = time.Now(), a.Proto, a.Caps, a.Gateway.GetBoot()
	b.mu.Unlock()
}

// reap drops gateways that stopped answering and fails the calls held by them.
func (b *Backend) reap() {
	var gone []string
	b.mu.Lock()
	for id, g := range b.gws {
		if time.Since(g.last) > leaseFactor*b.interval {
			delete(b.gws, id)
			gone = append(gone, id)
		}
	}
	b.mu.Unlock()
	for _, id := range gone {
		b.mux.failWhere(func(exec string) bool { return exec == id }, errPeerLost)
	}
}

// server is one Serve run.
type server struct {
	b       *Backend
	up      gateway.Upstream
	info    gateway.BackendInfo
	ctx     context.Context // outlives Serve's own context: work in flight finishes
	closing atomic.Bool
	sem     chan struct{}
	wg      sync.WaitGroup // work in flight

	cancels sync.Map // delivery id -> context.CancelFunc
}

// Serve announces the backend, delivers the work addressed to its pool to up and
// blocks until ctx ends. It then drains: it stops taking work, tells the
// gateways, lets work in flight finish (up to the drain timeout) and returns.
func (b *Backend) Serve(ctx context.Context, up gateway.Upstream, info gateway.BackendInfo) error {
	info.ID = b.id
	info.Proto = SupportedProto
	info.Caps = supportedCaps
	if info.Pool == "" {
		info.Pool = gateway.DefaultPool
	}
	labels := make(map[string]string, len(info.Labels)+1)
	for k, v := range info.Labels {
		labels[k] = v
	}
	labels[helloMSLabel] = strconv.FormatInt(b.interval.Milliseconds(), 10)
	info.Labels = labels

	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()
	s := &server{b: b, up: up, info: info, ctx: runCtx, sem: make(chan struct{}, b.maxInFlight)}

	var subs, queueSubs []*nats.Subscription
	defer func() {
		for _, sub := range subs {
			_ = sub.Unsubscribe()
		}
	}()
	add := func(sub *nats.Subscription, err error, queue bool) error {
		if err != nil {
			return err
		}
		sub.SetPendingLimits(-1, -1)
		subs = append(subs, sub)
		if queue {
			queueSubs = append(queueSubs, sub)
		}
		return nil
	}
	ackInbox := b.prefix + ".ack." + b.id
	hello := func() {
		data, _ := proto.Marshal(&linkpb.Hello{Backend: linkconv.InfoBackendToPB(info)})
		_ = b.nc.PublishMsg(&nats.Msg{Subject: b.prefix + ".hello", Reply: ackInbox, Data: data})
	}
	sub, err := b.nc.Subscribe(ackInbox, b.onAck)
	if err := add(sub, err, false); err != nil {
		return err
	}
	sub, err = b.nc.Subscribe(b.prefix+".who", func(*nats.Msg) { hello() })
	if err := add(sub, err, false); err != nil {
		return err
	}
	if info.Role != gateway.RoleCaller {
		q := b.prefix + "." + token(info.Pool)
		if len(info.Actions) == 0 {
			sub, err = b.nc.QueueSubscribe(b.prefix+".in."+token(info.Pool)+".>", q, s.onHandle)
			if err := add(sub, err, true); err != nil {
				return err
			}
		}
		for _, a := range info.Actions {
			sub, err = b.nc.QueueSubscribe(b.prefix+".in."+token(info.Pool)+".*."+token(a), q, s.onHandle)
			if err := add(sub, err, true); err != nil {
				return err
			}
		}
		sub, err = b.nc.QueueSubscribe(b.prefix+".connect."+token(info.Pool), q, s.onConnect)
		if err := add(sub, err, true); err != nil {
			return err
		}
		sub, err = b.nc.QueueSubscribe(b.prefix+".notify."+token(info.Pool), q, s.onNotify)
		if err := add(sub, err, true); err != nil {
			return err
		}
		sub, err = b.nc.Subscribe(b.prefix+".cancel", s.onCancel)
		if err := add(sub, err, false); err != nil {
			return err
		}
	}
	// Subscriptions must be known to the server before the first Hello: a gateway
	// that sees the backend may publish to it at once.
	if err := b.nc.Flush(); err != nil {
		return err
	}
	hello()

	// The announcements continue through the drain: a backend that is finishing
	// work is alive, and its lease must not lapse under the requests it holds.
	stopLoop := make(chan struct{})
	var loop sync.WaitGroup
	loop.Add(1)
	go func() {
		defer loop.Done()
		t := time.NewTicker(b.interval)
		defer t.Stop()
		reap := time.NewTicker(max(b.interval/4, 20*time.Millisecond))
		defer reap.Stop()
		for {
			select {
			case <-stopLoop:
				return
			case <-t.C:
				hello()
			case <-reap.C:
				b.reap()
			}
		}
	}()
	defer func() { close(stopLoop); loop.Wait() }()
	<-ctx.Done()
	return s.drain(ctx, queueSubs)
}

func (s *server) drain(_ context.Context, queueSubs []*nats.Subscription) error {
	b := s.b
	s.closing.Store(true)
	// Stop taking work: the server stops routing to this backend; what it had
	// already buffered is Nacked (nothing ran, the gateway asks again).
	for _, sub := range queueSubs {
		_ = sub.Drain()
	}
	// Tell the gateways, and wait for their DrainAck: each answers once the
	// requests it had dispatched to this backend are answered.
	live := b.Gateways()
	inbox := b.nc.NewRespInbox()
	acks := make(chan struct{}, len(live)+1)
	asub, err := b.nc.Subscribe(inbox, func(*nats.Msg) { acks <- struct{}{} })
	if err == nil {
		defer func() { _ = asub.Unsubscribe() }()
		data, _ := proto.Marshal(&linkpb.Hello{Backend: linkconv.InfoBackendToPB(s.info)})
		if err := b.nc.PublishMsg(&nats.Msg{Subject: b.prefix + ".drain", Reply: inbox, Data: data}); err == nil {
			deadline := time.After(b.drainTimeout)
			for n := 0; n < len(live); n++ {
				select {
				case <-acks:
				case <-deadline:
					n = len(live)
				}
			}
		}
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(b.drainTimeout):
		return fmt.Errorf("linknats: drain timed out with work in flight")
	}
}

// begin admits one request; ok is false when the backend is closing or full and
// the request has been Nacked.
func (s *server) begin(m *nats.Msg, nack func()) bool {
	if m.Reply == "" {
		return false // a request without a reply subject cannot be answered
	}
	select {
	case s.sem <- struct{}{}:
	default:
		nack()
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *server) end() {
	<-s.sem
	s.wg.Done()
}

func (s *server) nackReply(m *nats.Msg) {
	data, _ := proto.Marshal(&linkpb.Reply{Nack: true})
	respond(s.b.nc, m, data, nil)
}

func (s *server) onHandle(m *nats.Msg) {
	if s.closing.Load() {
		s.nackReply(m)
		return
	}
	var pb linkpb.Envelope
	if err := proto.Unmarshal(m.Data, &pb); err != nil {
		s.b.metrics.Unsupported("recv", "envelope")
		respondUnsupported(s.b.nc, m, "envelope")
		return
	}
	if !s.begin(m, func() { s.nackReply(m) }) {
		return
	}
	go func() {
		defer s.end()
		accepted(s.b.nc, m, s.info.ID)
		env := linkconv.EnvFromPB(&pb)
		hctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		if env.DeliveryID != "" {
			s.cancels.Store(env.DeliveryID, context.CancelFunc(cancel))
			defer s.cancels.Delete(env.DeliveryID)
		}
		rep, err := s.up.Handle(hctx, env)
		if err != nil {
			rep = gateway.Reply{Error: &gateway.WireError{
				Kind: gateway.ErrKindCallError, Code: "InternalError", Description: "internal error",
			}}
		}
		data, _ := proto.Marshal(linkconv.ReplyToPB(rep))
		respond(s.b.nc, m, data, nil)
	}()
}

func (s *server) onConnect(m *nats.Msg) {
	var pb linkpb.ConnectRequest
	if err := proto.Unmarshal(m.Data, &pb); err != nil {
		respondUnsupported(s.b.nc, m, "connect")
		return
	}
	nack := func() { // Connect has no Nack: refuse, the charger reconnects
		data, _ := proto.Marshal(linkconv.ConnectRepToPB(gateway.ConnectReply{Reject: &gateway.Rejection{Status: 503, Message: "service unavailable"}}))
		respond(s.b.nc, m, data, nil)
	}
	if s.closing.Load() {
		nack()
		return
	}
	if !s.begin(m, nack) {
		return
	}
	go func() {
		defer s.end()
		accepted(s.b.nc, m, s.info.ID)
		rep, err := s.up.Connect(s.ctx, linkconv.ConnectReqFromPB(&pb))
		if err != nil {
			// The backend could not decide: refuse rather than guess.
			rep = gateway.ConnectReply{Reject: &gateway.Rejection{Status: 503, Message: "service unavailable"}}
		}
		data, _ := proto.Marshal(linkconv.ConnectRepToPB(rep))
		respond(s.b.nc, m, data, nil)
	}()
}

func (s *server) onNotify(m *nats.Msg) {
	var pb linkpb.Envelope
	if err := proto.Unmarshal(m.Data, &pb); err != nil {
		respondUnsupported(s.b.nc, m, "envelope")
		return
	}
	draining := func() {
		data, _ := proto.Marshal(&linkpb.Ack{Error: linkconv.ErrToPB(gateway.EncodeError(gateway.ErrGatewayDraining))})
		respond(s.b.nc, m, data, nil)
	}
	if s.closing.Load() && gateway.Kind(pb.Kind) != gateway.KindSessionClosed {
		draining()
		return
	}
	if !s.begin(m, draining) {
		return
	}
	go func() {
		defer s.end()
		accepted(s.b.nc, m, s.info.ID)
		e := linkconv.ErrToPB(gateway.EncodeError(s.up.Notify(s.ctx, linkconv.EnvFromPB(&pb))))
		data, _ := proto.Marshal(&linkpb.Ack{Error: e})
		respond(s.b.nc, m, data, nil)
	}()
}

func (s *server) onCancel(m *nats.Msg) {
	if c, ok := s.cancels.Load(string(m.Data)); ok {
		c.(context.CancelFunc)()
	}
}

// ---- gateway.Downstream ------------------------------------------------------

func (b *Backend) downstream(ctx context.Context, gwID, op string, msg proto.Message) (*nats.Msg, error) {
	id, ok := b.resolveGateway(gwID)
	if !ok {
		return nil, errNoResponders
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	m, _, err := b.mux.request(ctx, b.prefix+".gw."+token(id)+"."+op, nats.Header{hBackend: []string{b.id}}, data, id)
	return m, err
}

// Call implements gateway.Downstream: it addresses the gateway named by
// req.Target.GatewayID. "No responders" means the gateway is not there and
// nothing was sent; any other failure after the request left leaves the charger
// possibly having executed it.
func (b *Backend) Call(ctx context.Context, req gateway.CallRequest) (gateway.CallReply, error) {
	m, err := b.downstream(ctx, req.Target.GatewayID, "call", linkconv.CallReqToPB(req))
	switch {
	case errors.Is(err, gateway.ErrNotDelivered):
		return gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(ocppj.ErrNotConnected)}, nil
	case err != nil:
		return gateway.CallReply{Sent: gateway.SentNoResponse}, err
	}
	if m.Header.Get(hUnsupported) != "" {
		return gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(gateway.ErrUnsupported)}, nil
	}
	var rep linkpb.CallReply
	if err := proto.Unmarshal(m.Data, &rep); err != nil {
		return gateway.CallReply{}, err
	}
	return linkconv.CallRepFromPB(&rep), nil
}

func (b *Backend) ack(m *nats.Msg, err error) error {
	if err != nil {
		if errors.Is(err, gateway.ErrNotDelivered) {
			return ocppj.ErrNotConnected
		}
		return err
	}
	if m.Header.Get(hUnsupported) != "" {
		return gateway.ErrUnsupported
	}
	var a linkpb.Ack
	if err := proto.Unmarshal(m.Data, &a); err != nil {
		return err
	}
	return linkconv.ErrFromPB(a.Error).Err()
}

// Send implements gateway.Downstream.
func (b *Backend) Send(ctx context.Context, req gateway.CallRequest) error {
	m, err := b.downstream(ctx, req.Target.GatewayID, "send", linkconv.CallReqToPB(req))
	return b.ack(m, err)
}

// Control implements gateway.Downstream.
func (b *Backend) Control(ctx context.Context, req gateway.ControlRequest) error {
	m, err := b.downstream(ctx, req.Target.GatewayID, "control", &linkpb.ControlRequest{
		Target: linkconv.RefToPB(req.Target), Op: uint32(req.Op), Reason: req.Reason, Attrs: req.Attrs,
	})
	return b.ack(m, err)
}

// Locate is not provided by this link: the session registry is the directory
// of sessions across gateways.
func (b *Backend) Locate(context.Context, string) (gateway.Located, bool, error) {
	return gateway.Located{}, false, nil
}
