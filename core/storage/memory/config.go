package memory

import (
	"context"
	"sync"

	"github.com/shiv3/gocpp/core/storage"
)

type configStore struct {
	mu sync.RWMutex
	m  map[string]map[string]string // cpID -> key -> value
}

// NewConfigStore returns an in-memory ConfigStore.
func NewConfigStore() storage.ConfigStore {
	return &configStore{m: make(map[string]map[string]string)}
}

func (s *configStore) Put(_ context.Context, cpID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m[cpID] == nil {
		s.m[cpID] = make(map[string]string)
	}
	s.m[cpID][key] = value
	return nil
}

func (s *configStore) Get(_ context.Context, cpID, key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[cpID][key]
	return v, ok, nil
}

func (s *configStore) List(_ context.Context, cpID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.m[cpID]))
	for k, v := range s.m[cpID] {
		out[k] = v
	}
	return out, nil
}

func (s *configStore) Delete(_ context.Context, cpID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m[cpID], key)
	return nil
}
