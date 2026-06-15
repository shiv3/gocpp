package tenant

import (
	"sync"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
)

// ConnectionRegistry is the core connection registry interface.
type ConnectionRegistry = storage.ConnectionRegistry

// TransactionStore is the core transaction store interface.
type TransactionStore = storage.TransactionStore

// ConfigStore is the core configuration store interface.
type ConfigStore = storage.ConfigStore

type tenantStores struct {
	connReg  ConnectionRegistry
	txStore  TransactionStore
	cfgStore ConfigStore
}

// Manager lazily creates tenant-isolated in-memory storage instances.
type Manager struct {
	mu      sync.Mutex
	tenants map[string]tenantStores
}

// NewManager returns a Manager with no pre-created tenants.
func NewManager() *Manager {
	return &Manager{tenants: make(map[string]tenantStores)}
}

// For returns the storage instances for tenantID, creating them on first use.
func (m *Manager) For(tenantID string) (ConnectionRegistry, TransactionStore, ConfigStore) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tenants == nil {
		m.tenants = make(map[string]tenantStores)
	}
	stores, ok := m.tenants[tenantID]
	if !ok {
		stores = tenantStores{
			connReg:  memory.NewConnectionRegistry(),
			txStore:  memory.NewTransactionStore(),
			cfgStore: memory.NewConfigStore(),
		}
		m.tenants[tenantID] = stores
	}
	return stores.connReg, stores.txStore, stores.cfgStore
}
