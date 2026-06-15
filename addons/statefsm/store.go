package statefsm

import "sync"

// StateStore persists connector states by charge point and connector ID.
type StateStore interface {
	Save(cpID string, connectorID int, state State) error
	Load(cpID string, connectorID int) (State, bool, error)
}

// ConnectorKey identifies a connector state record.
type ConnectorKey struct {
	CPID        string
	ConnectorID int
}

// MemoryStateStore is an in-memory StateStore implementation.
type MemoryStateStore struct {
	mu     sync.RWMutex
	states map[ConnectorKey]State
}

// NewMemoryStateStore returns an empty in-memory StateStore.
func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{states: make(map[ConnectorKey]State)}
}

// Save stores the latest connector state.
func (s *MemoryStateStore) Save(cpID string, connectorID int, state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[ConnectorKey{CPID: cpID, ConnectorID: connectorID}] = state
	return nil
}

// Load returns a previously saved connector state.
func (s *MemoryStateStore) Load(cpID string, connectorID int) (State, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.states[ConnectorKey{CPID: cpID, ConnectorID: connectorID}]
	return state, ok, nil
}
