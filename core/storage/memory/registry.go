// Package memory provides in-memory implementations of the storage interfaces.
package memory

import (
	"context"
	"sync"

	"github.com/shiv3/gocpp/core/storage"
)

type connectionRegistry struct {
	mu    sync.RWMutex
	local map[string]storage.LiveConn
}

// NewConnectionRegistry returns an in-memory registry. Global lookups are no-ops
// (single-instance); replace with a Redis adapter for multi-instance routing.
func NewConnectionRegistry() storage.ConnectionRegistry {
	return &connectionRegistry{local: make(map[string]storage.LiveConn)}
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

func (r *connectionRegistry) PutGlobal(context.Context, string, string) error { return nil }
func (r *connectionRegistry) LookupGlobal(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (r *connectionRegistry) DeleteGlobal(context.Context, string) error { return nil }
