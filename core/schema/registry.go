package schema

import (
	"fmt"
	"io/fs"
	"sync"
)

// Registry holds compiled validators keyed by version/action/payload-kind.
// payloadKind is "request" or "response".
type Registry struct {
	mu sync.RWMutex
	m  map[string]*Validator
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]*Validator)}
}

func key(version, action, kind string) string {
	return version + "|" + action + "|" + kind
}

// Register compiles and stores a validator.
func (r *Registry) Register(version, action, kind string, fsys fs.FS, file string) error {
	v, err := New(fsys, file)
	if err != nil {
		return fmt.Errorf("register %s/%s/%s: %w", version, action, kind, err)
	}
	r.mu.Lock()
	r.m[key(version, action, kind)] = v
	r.mu.Unlock()
	return nil
}

// Lookup returns the validator for the given key, if registered.
func (r *Registry) Lookup(version, action, kind string) (*Validator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.m[key(version, action, kind)]
	return v, ok
}
