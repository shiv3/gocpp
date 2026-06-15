package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/shiv3/gocpp/core/storage"
)

type transactionStore struct {
	mu sync.RWMutex
	m  map[string]storage.Transaction
}

// NewTransactionStore returns an in-memory TransactionStore.
func NewTransactionStore() storage.TransactionStore {
	return &transactionStore{m: make(map[string]storage.Transaction)}
}

func (s *transactionStore) Begin(_ context.Context, tx storage.Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[tx.ID]; ok {
		return fmt.Errorf("transaction %s already exists", tx.ID)
	}
	s.m[tx.ID] = tx
	return nil
}

func (s *transactionStore) Update(_ context.Context, txID string, mut storage.TransactionMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, ok := s.m[txID]
	if !ok {
		return fmt.Errorf("transaction %s not found", txID)
	}
	if mut.Status != nil {
		tx.Status = *mut.Status
	}
	if mut.MeterValue != nil {
		tx.MeterStop = mut.MeterValue
	}
	s.m[txID] = tx
	return nil
}

func (s *transactionStore) End(_ context.Context, txID string, end storage.TransactionEnd) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, ok := s.m[txID]
	if !ok {
		return fmt.Errorf("transaction %s not found", txID)
	}
	tx.EndedAt = &end.EndedAt
	tx.MeterStop = &end.MeterStop
	tx.Status = end.Status
	s.m[txID] = tx
	return nil
}

func (s *transactionStore) Get(_ context.Context, txID string) (storage.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tx, ok := s.m[txID]
	if !ok {
		return storage.Transaction{}, fmt.Errorf("transaction %s not found", txID)
	}
	return tx, nil
}

func (s *transactionStore) ListActive(_ context.Context, cpID string) ([]storage.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []storage.Transaction
	for _, tx := range s.m {
		if tx.CPID == cpID && tx.Status == storage.TransactionActive {
			out = append(out, tx)
		}
	}
	return out, nil
}
