// Package inproc is the in-process gateway link: gateway and backend call each
// other directly, with no serialization. It is used by tests (the conformance
// suite runs through it), by single-binary deployments that want the split
// topology, and as the reference implementation of the link semantics: backend
// selection, the hold while no backend is attached, redispatch on Nack, and
// draining.
package inproc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
)

// Hub connects gateways and backends in one process. Gateways and backends
// attach to the same Hub; every backend is reachable from every gateway and
// every gateway from every backend.
type Hub struct {
	pool         *gateway.PoolSet
	drainTimeout time.Duration
	backendSeq   atomic.Uint64

	mu       sync.RWMutex
	gateways map[string]gateway.Downstream
}

// Option configures a Hub.
type Option func(*Hub)

// WithUpstreamWait sets how long a gateway holds an inbound CALL while no
// backend is attached (default gateway.DefaultUpstreamWait).
func WithUpstreamWait(d time.Duration) Option {
	return func(h *Hub) { h.pool = gateway.NewPoolSet(d) }
}

// WithDrainTimeout bounds how long a backend waits for in-flight work when its
// Serve context ends (default 30 s).
func WithDrainTimeout(d time.Duration) Option {
	return func(h *Hub) { h.drainTimeout = d }
}

// New returns an empty Hub.
func New(opts ...Option) *Hub {
	h := &Hub{
		pool:         gateway.NewPoolSet(0),
		drainTimeout: 30 * time.Second,
		gateways:     make(map[string]gateway.Downstream),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Gateway returns the link end for a gateway.
func (h *Hub) Gateway() gateway.GatewayLink { return gatewayEnd{h} }

// Backend returns the link end for a processing-tier server.
func (h *Hub) Backend() gateway.BackendLink { return backendEnd{h} }

// Backends returns the number of attached backends.
func (h *Hub) Backends() int { return h.pool.Len() }

type gatewayEnd struct{ h *Hub }

func (g gatewayEnd) Connect(ctx context.Context, req gateway.ConnectRequest) (gateway.ConnectReply, error) {
	return g.h.pool.Connect(ctx, req)
}

func (g gatewayEnd) Handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, error) {
	return g.h.pool.Handle(ctx, env)
}

func (g gatewayEnd) Notify(ctx context.Context, env gateway.Envelope) error {
	return g.h.pool.Notify(ctx, env)
}

func (g gatewayEnd) Attach(id string, d gateway.Downstream) (detach func()) {
	g.h.mu.Lock()
	g.h.gateways[id] = d
	g.h.mu.Unlock()
	return func() {
		g.h.mu.Lock()
		if g.h.gateways[id] == d {
			delete(g.h.gateways, id)
		}
		g.h.mu.Unlock()
	}
}

type backendEnd struct{ h *Hub }

func (b backendEnd) gateway(id string) (gateway.Downstream, error) {
	b.h.mu.RLock()
	defer b.h.mu.RUnlock()
	if d, ok := b.h.gateways[id]; ok {
		return d, nil
	}
	if id == "" && len(b.h.gateways) == 1 {
		for _, d := range b.h.gateways {
			return d, nil
		}
	}
	return nil, fmt.Errorf("%w: no gateway %q attached", ocppj.ErrNotConnected, id)
}

func (b backendEnd) Call(ctx context.Context, req gateway.CallRequest) (gateway.CallReply, error) {
	d, err := b.gateway(req.Target.GatewayID)
	if err != nil {
		return gateway.CallReply{Sent: gateway.NotSent, Error: gateway.EncodeError(ocppj.ErrNotConnected)}, nil
	}
	return d.Call(ctx, req)
}

func (b backendEnd) Send(ctx context.Context, req gateway.CallRequest) error {
	d, err := b.gateway(req.Target.GatewayID)
	if err != nil {
		return err
	}
	return d.Send(ctx, req)
}

func (b backendEnd) Control(ctx context.Context, req gateway.ControlRequest) error {
	d, err := b.gateway(req.Target.GatewayID)
	if err != nil {
		return err
	}
	return d.Control(ctx, req)
}

// Locate asks the attached gateways, in no particular order, for the session of
// cpID. A gateway answers by implementing gateway.Locator.
func (b backendEnd) Locate(ctx context.Context, cpID string) (gateway.Located, bool, error) {
	b.h.mu.RLock()
	ds := make([]gateway.Downstream, 0, len(b.h.gateways))
	for _, d := range b.h.gateways {
		ds = append(ds, d)
	}
	b.h.mu.RUnlock()
	for _, d := range ds {
		if l, ok := d.(gateway.Locator); ok {
			if loc, found := l.LocateSession(cpID); found {
				return loc, true, nil
			}
		}
	}
	return gateway.Located{}, false, nil
}

// Serve attaches up as a backend and blocks until ctx ends, then drains.
func (b backendEnd) Serve(ctx context.Context, up gateway.Upstream, info gateway.BackendInfo) error {
	detach := b.h.AttachBackend(up, info)
	<-ctx.Done()
	dctx, cancel := context.WithTimeout(context.Background(), b.h.drainTimeout)
	defer cancel()
	return detach(dctx)
}

// AttachBackend attaches up as a backend without a goroutine. The returned
// function drains it: no new work, then it waits for the work in flight until
// ctx ends.
func (h *Hub) AttachBackend(up gateway.Upstream, info gateway.BackendInfo) (drain func(ctx context.Context) error) {
	if info.ID == "" {
		info.ID = fmt.Sprintf("backend-%d", h.backendSeq.Add(1))
	}
	m := h.pool.Add(info, up)
	return func(ctx context.Context) error {
		if m == nil {
			return nil
		}
		if err := h.pool.Drain(ctx, m); err != nil {
			return fmt.Errorf("inproc: drain %s: %w", info.ID, err)
		}
		return nil
	}
}

// Pools returns the status of the backend pools.
func (h *Hub) Pools() []gateway.PoolStatus { return h.pool.Status() }
