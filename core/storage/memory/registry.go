// Package memory provides in-memory implementations of the storage interfaces.
package memory

import (
	"context"
	"sync"

	"github.com/shiv3/gocpp/core/storage"
)

// globalDirectory is the cpID -> owning-instance map behind PutGlobal and
// LookupGlobal. Registries created by one InProcessHub share a directory.
type globalDirectory struct {
	mu    sync.RWMutex
	owner map[string]string
}

func newGlobalDirectory() *globalDirectory {
	return &globalDirectory{owner: make(map[string]string)}
}

func (d *globalDirectory) put(cpID, instanceID string) {
	d.mu.Lock()
	d.owner[cpID] = instanceID
	d.mu.Unlock()
}

func (d *globalDirectory) lookup(cpID string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	id, ok := d.owner[cpID]
	return id, ok
}

func (d *globalDirectory) delete(cpID string) {
	d.mu.Lock()
	delete(d.owner, cpID)
	d.mu.Unlock()
}

func (d *globalDirectory) deleteIf(cpID, instanceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cur, ok := d.owner[cpID]; !ok || cur != instanceID {
		return false
	}
	delete(d.owner, cpID)
	return true
}

type connectionRegistry struct {
	mu     sync.RWMutex
	local  map[string]storage.LiveConn
	global *globalDirectory
}

// NewConnectionRegistry returns an in-memory registry. Local connections and the
// global route table both live in this one registry, so it serves a single
// instance; instances that must see each other's routes in one process (tests,
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

func (r *connectionRegistry) LookupGlobal(_ context.Context, cpID string) (string, bool, error) {
	id, ok := r.global.lookup(cpID)
	return id, ok, nil
}

func (r *connectionRegistry) DeleteGlobal(_ context.Context, cpID string) error {
	r.global.delete(cpID)
	return nil
}

// DeleteGlobalIf implements storage.OwnedGlobalDeleter.
func (r *connectionRegistry) DeleteGlobalIf(_ context.Context, cpID, instanceID string) (bool, error) {
	return r.global.deleteIf(cpID, instanceID), nil
}
