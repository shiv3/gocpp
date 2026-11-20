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
	dir      *globalDirectory // cpID -> instanceID, shared with Registry()
	handlers map[string]storage.RemoteHandler
}

// NewInProcessHub creates a hub.
func NewInProcessHub() *InProcessHub {
	return &InProcessHub{
		dir:      newGlobalDirectory(),
		handlers: make(map[string]storage.RemoteHandler),
	}
}

// Bind records which instance holds a charge point.
func (h *InProcessHub) Bind(cpID, instanceID string) {
	h.dir.put(cpID, instanceID)
}

// Registry returns a ConnectionRegistry for one instance: its local connection
// table is private, while the global route table is the hub's, shared by every
// registry and router the hub hands out. Pair it with Router for the same
// instance id.
func (h *InProcessHub) Registry() storage.ConnectionRegistry {
	return newRegistry(h.dir)
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
	inst, ok := r.hub.dir.lookup(cpID)
	r.hub.mu.RLock()
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
	r.hub.mu.Lock()
	delete(r.hub.handlers, r.instanceID)
	r.hub.mu.Unlock()
	return ctx.Err()
}
