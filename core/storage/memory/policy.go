package memory

import (
	"context"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

// PolicyStore is an in-memory storage.PolicyStore (and PolicyHistory). Gateways
// sharing one process (tests, the in-process split) share one instance.
//
// Experimental: this API may change before v1.0.
type PolicyStore struct {
	mu      sync.Mutex
	rev     uint64
	data    []byte
	hist    []storage.PolicyEntry // newest first
	waiters map[chan struct{}]struct{}
}

var (
	_ storage.PolicyStore   = (*PolicyStore)(nil)
	_ storage.PolicyHistory = (*PolicyStore)(nil)
)

// NewPolicyStore returns an empty store.
func NewPolicyStore() *PolicyStore {
	return &PolicyStore{waiters: make(map[chan struct{}]struct{})}
}

// PutPolicy implements storage.PolicyStore.
func (s *PolicyStore) PutPolicy(_ context.Context, prevRev, rev uint64, data []byte) (bool, error) {
	if rev <= prevRev {
		return false, storage.ErrPolicyRevision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rev != prevRev {
		return false, nil
	}
	s.rev, s.data = rev, append([]byte(nil), data...)
	s.hist = append([]storage.PolicyEntry{{Rev: rev, Data: s.data, At: time.Now()}}, s.hist...)
	if len(s.hist) > storage.PolicyHistoryLimit {
		s.hist = s.hist[:storage.PolicyHistoryLimit]
	}
	for ch := range s.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return true, nil
}

// Policy implements storage.PolicyStore.
func (s *PolicyStore) Policy(context.Context) (uint64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev, append([]byte(nil), s.data...), nil
}

// PolicyHistory implements storage.PolicyHistory.
func (s *PolicyStore) PolicyHistory(context.Context) ([]storage.PolicyEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]storage.PolicyEntry(nil), s.hist...), nil
}

// WatchPolicy implements storage.PolicyStore.
func (s *PolicyStore) WatchPolicy(ctx context.Context, fn func(uint64, []byte)) error {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.waiters[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, ch)
		s.mu.Unlock()
	}()
	var seen uint64
	for {
		rev, data, _ := s.Policy(ctx)
		if rev > seen {
			seen = rev
			fn(rev, data)
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
