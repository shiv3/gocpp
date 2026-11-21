// Package memory provides in-memory implementations of the storage interfaces.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

type connectionRegistry struct {
	mu     sync.RWMutex
	local  map[string]storage.LiveConn
	global *globalDirectory
}

var (
	_ storage.ConnectionRegistry = (*connectionRegistry)(nil)
	_ storage.SessionRegistry    = (*connectionRegistry)(nil)
)

// NewConnectionRegistry returns an in-memory registry. Local connections and the
// global route table both live in this one registry, so it serves a single
// instance. It also implements storage.SessionRegistry over the same table (see
// NewSessionRegistry); instances that must see each other's routes in one process (tests,
// dev) take their registries from an InProcessHub, and production multi-instance
// deployments need a shared-store registry such as a Redis adapter.
func NewConnectionRegistry() storage.ConnectionRegistry {
	return newRegistry(newGlobalDirectory())
}

func newRegistry(g *globalDirectory) *connectionRegistry {
	return &connectionRegistry{local: make(map[string]storage.LiveConn), global: g}
}

func (r *connectionRegistry) PutLocal(_ context.Context, cpID string, conn storage.LiveConn) error {
	r.mu.Lock()
	r.local[cpID] = conn
	r.mu.Unlock()
	return nil
}

func (r *connectionRegistry) GetLocal(cpID string) (storage.LiveConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.local[cpID]
	return c, ok
}

func (r *connectionRegistry) DeleteLocal(_ context.Context, cpID string) error {
	r.mu.Lock()
	delete(r.local, cpID)
	r.mu.Unlock()
	return nil
}

func (r *connectionRegistry) RangeLocal(fn func(string, storage.LiveConn) bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, c := range r.local {
		if !fn(id, c) {
			return
		}
	}
}

func (r *connectionRegistry) PutGlobal(_ context.Context, cpID, instanceID string) error {
	r.global.put(cpID, instanceID)
	return nil
}

// LookupGlobal reports the owner of cpID without checking gateway leases; use
// Lookup for the lease-aware view.
func (r *connectionRegistry) LookupGlobal(_ context.Context, cpID string) (string, bool, error) {
	id, ok := r.global.lookup(cpID)
	return id, ok, nil
}

func (r *connectionRegistry) Claim(ctx context.Context, s storage.Session) (uint64, *storage.Session, error) {
	return r.global.claim(ctx, s)
}

func (r *connectionRegistry) Release(ctx context.Context, cpID string, epoch uint64) (bool, error) {
	return r.global.release(ctx, cpID, epoch)
}

func (r *connectionRegistry) Reassert(ctx context.Context, s storage.Session) (bool, error) {
	return r.global.reassert(ctx, s)
}

func (r *connectionRegistry) SetAttrs(ctx context.Context, cpID string, epoch uint64, attrs map[string]string) (bool, error) {
	return r.global.setAttrs(ctx, cpID, epoch, attrs)
}

// SetRoute implements storage.RouteWriter.
func (r *connectionRegistry) SetRoute(ctx context.Context, cpID string, epoch uint64, route map[string]string) (bool, error) {
	return r.global.setRoute(ctx, cpID, epoch, route)
}

func (r *connectionRegistry) Lookup(ctx context.Context, cpID string) (storage.Session, bool, error) {
	return r.global.lookupSession(ctx, cpID)
}

func (r *connectionRegistry) LookupMany(ctx context.Context, cpIDs []string) ([]storage.Session, error) {
	return r.global.lookupMany(ctx, cpIDs)
}

func (r *connectionRegistry) Heartbeat(ctx context.Context, g storage.GatewayInfo, ttl time.Duration) error {
	return r.global.heartbeat(ctx, g, ttl)
}

func (r *connectionRegistry) Gateways(ctx context.Context) ([]storage.GatewayInfo, error) {
	return r.global.gateways(ctx)
}

func (r *connectionRegistry) DeleteGlobal(_ context.Context, cpID string) error {
	r.global.delete(cpID)
	return nil
}

// DeleteGlobalIf implements storage.OwnedGlobalDeleter.
func (r *connectionRegistry) DeleteGlobalIf(_ context.Context, cpID, instanceID string) (bool, error) {
	return r.global.deleteIf(cpID, instanceID), nil
}
