package memory

import (
	"context"
	"sync"

	"github.com/shiv3/gocpp/core/storage"
)

// StateStore is an in-memory storage.StateStore.
//
// Experimental: this API may change before v1.0.
type StateStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

var _ storage.StateStore = (*StateStore)(nil)

// NewStateStore returns an empty store.
func NewStateStore() *StateStore { return &StateStore{m: map[string][]byte{}} }

// PutState implements storage.StateStore.
func (s *StateStore) PutState(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), data...)
	return nil
}

// State implements storage.StateStore.
func (s *StateStore) State(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[key]
	if !ok {
		return nil, nil
	}
	return append([]byte(nil), d...), nil
}

// DeleteState implements storage.StateStore.
func (s *StateStore) DeleteState(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}
