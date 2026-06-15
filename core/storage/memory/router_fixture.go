package memory

import (
	"context"
	"sync"

	"github.com/shiv3/gocpp/core/storage"
)

// InProcessHub is a test/dev fixture simulating multi-instance routing in one
// process — it validates the MessageRouter contract before real broker adapters.
type InProcessHub struct {
	mu       sync.RWMutex
	bindings map[string]string // cpID -> instanceID
	handlers map[string]storage.RemoteHandler
}

// NewInProcessHub creates a hub.
func NewInProcessHub() *InProcessHub {
	return &InProcessHub{
		bindings: make(map[string]string),
		handlers: make(map[string]storage.RemoteHandler),
	}
}

// Bind records which instance holds a charge point.
func (h *InProcessHub) Bind(cpID, instanceID string) {
	h.mu.Lock()
	h.bindings[cpID] = instanceID
	h.mu.Unlock()
}

// Router returns a MessageRouter for the given instance.
func (h *InProcessHub) Router(instanceID string) storage.MessageRouter {
	return &hubRouter{hub: h, instanceID: instanceID}
}

type hubRouter struct {
	hub        *InProcessHub
	instanceID string
}

func (r *hubRouter) CallLocal(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrNotLocal
}

func (r *hubRouter) CallRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
	r.hub.mu.RLock()
	inst, ok := r.hub.bindings[cpID]
	h := r.hub.handlers[inst]
	r.hub.mu.RUnlock()
	if !ok || h == nil {
		return nil, storage.ErrNotLocal
	}
	return h(ctx, cpID, action, req)
}

func (r *hubRouter) ServeRemote(ctx context.Context, handler storage.RemoteHandler) error {
	r.hub.mu.Lock()
	r.hub.handlers[r.instanceID] = handler
	r.hub.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}
