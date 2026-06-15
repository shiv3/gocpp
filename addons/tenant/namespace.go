package tenant

import (
	"context"
	"strconv"
	"strings"

	"github.com/shiv3/gocpp/core/storage"
)

type namespace struct {
	prefix string
}

func newNamespace(tenantID string) namespace {
	return namespace{prefix: strconv.Itoa(len(tenantID)) + ":" + tenantID + ":"}
}

func (n namespace) wrap(key string) string {
	return n.prefix + key
}

func (n namespace) unwrap(key string) (string, bool) {
	value, ok := strings.CutPrefix(key, n.prefix)
	return value, ok
}

// NamespacedConnectionRegistry partitions a shared ConnectionRegistry by tenant.
type NamespacedConnectionRegistry struct {
	base ConnectionRegistry
	ns   namespace
}

// NewNamespacedConnectionRegistry wraps base with a tenant namespace.
func NewNamespacedConnectionRegistry(tenantID string, base ConnectionRegistry) *NamespacedConnectionRegistry {
	if base == nil {
		panic("tenant: nil ConnectionRegistry")
	}
	return &NamespacedConnectionRegistry{base: base, ns: newNamespace(tenantID)}
}

// PutLocal stores a live connection under the tenant-prefixed charge point id.
func (r *NamespacedConnectionRegistry) PutLocal(ctx context.Context, cpID string, conn storage.LiveConn) error {
	return r.base.PutLocal(ctx, r.ns.wrap(cpID), conn)
}

// GetLocal returns a live connection for cpID within this tenant.
func (r *NamespacedConnectionRegistry) GetLocal(cpID string) (storage.LiveConn, bool) {
	return r.base.GetLocal(r.ns.wrap(cpID))
}

// DeleteLocal removes a live connection for cpID within this tenant.
func (r *NamespacedConnectionRegistry) DeleteLocal(ctx context.Context, cpID string) error {
	return r.base.DeleteLocal(ctx, r.ns.wrap(cpID))
}

// RangeLocal iterates over this tenant's local connections with unprefixed ids.
func (r *NamespacedConnectionRegistry) RangeLocal(fn func(cpID string, conn storage.LiveConn) bool) {
	r.base.RangeLocal(func(cpID string, conn storage.LiveConn) bool {
		unprefixed, ok := r.ns.unwrap(cpID)
		if !ok {
			return true
		}
		return fn(unprefixed, conn)
	})
}

// PutGlobal stores the tenant-prefixed global route for cpID.
func (r *NamespacedConnectionRegistry) PutGlobal(ctx context.Context, cpID, instanceID string) error {
	return r.base.PutGlobal(ctx, r.ns.wrap(cpID), instanceID)
}

// LookupGlobal returns the global route for cpID within this tenant.
func (r *NamespacedConnectionRegistry) LookupGlobal(ctx context.Context, cpID string) (string, bool, error) {
	return r.base.LookupGlobal(ctx, r.ns.wrap(cpID))
}

// DeleteGlobal removes the global route for cpID within this tenant.
func (r *NamespacedConnectionRegistry) DeleteGlobal(ctx context.Context, cpID string) error {
	return r.base.DeleteGlobal(ctx, r.ns.wrap(cpID))
}

// NamespacedTransactionStore partitions a shared TransactionStore by tenant.
type NamespacedTransactionStore struct {
	base TransactionStore
	ns   namespace
}

// NewNamespacedTransactionStore wraps base with a tenant namespace.
func NewNamespacedTransactionStore(tenantID string, base TransactionStore) *NamespacedTransactionStore {
	if base == nil {
		panic("tenant: nil TransactionStore")
	}
	return &NamespacedTransactionStore{base: base, ns: newNamespace(tenantID)}
}

// Begin starts a transaction under tenant-prefixed transaction and charge point ids.
func (s *NamespacedTransactionStore) Begin(ctx context.Context, tx storage.Transaction) error {
	tx.ID = s.ns.wrap(tx.ID)
	tx.CPID = s.ns.wrap(tx.CPID)
	return s.base.Begin(ctx, tx)
}

// Update applies a mutation to txID within this tenant.
func (s *NamespacedTransactionStore) Update(ctx context.Context, txID string, mut storage.TransactionMutation) error {
	return s.base.Update(ctx, s.ns.wrap(txID), mut)
}

// End finalizes txID within this tenant.
func (s *NamespacedTransactionStore) End(ctx context.Context, txID string, end storage.TransactionEnd) error {
	return s.base.End(ctx, s.ns.wrap(txID), end)
}

// Get returns txID within this tenant with public ids restored.
func (s *NamespacedTransactionStore) Get(ctx context.Context, txID string) (storage.Transaction, error) {
	tx, err := s.base.Get(ctx, s.ns.wrap(txID))
	if err != nil {
		return storage.Transaction{}, err
	}
	s.unnamespaceTransaction(&tx)
	return tx, nil
}

// ListActive returns active transactions for cpID within this tenant.
func (s *NamespacedTransactionStore) ListActive(ctx context.Context, cpID string) ([]storage.Transaction, error) {
	txs, err := s.base.ListActive(ctx, s.ns.wrap(cpID))
	if err != nil {
		return nil, err
	}
	for i := range txs {
		s.unnamespaceTransaction(&txs[i])
	}
	return txs, nil
}

func (s *NamespacedTransactionStore) unnamespaceTransaction(tx *storage.Transaction) {
	if id, ok := s.ns.unwrap(tx.ID); ok {
		tx.ID = id
	}
	if cpID, ok := s.ns.unwrap(tx.CPID); ok {
		tx.CPID = cpID
	}
}

// NamespacedConfigStore partitions a shared ConfigStore by tenant.
type NamespacedConfigStore struct {
	base ConfigStore
	ns   namespace
}

// NewNamespacedConfigStore wraps base with a tenant namespace.
func NewNamespacedConfigStore(tenantID string, base ConfigStore) *NamespacedConfigStore {
	if base == nil {
		panic("tenant: nil ConfigStore")
	}
	return &NamespacedConfigStore{base: base, ns: newNamespace(tenantID)}
}

// Put stores a configuration value for cpID within this tenant.
func (s *NamespacedConfigStore) Put(ctx context.Context, cpID, key, value string) error {
	return s.base.Put(ctx, s.ns.wrap(cpID), key, value)
}

// Get returns a configuration value for cpID within this tenant.
func (s *NamespacedConfigStore) Get(ctx context.Context, cpID, key string) (string, bool, error) {
	return s.base.Get(ctx, s.ns.wrap(cpID), key)
}

// List returns configuration values for cpID within this tenant.
func (s *NamespacedConfigStore) List(ctx context.Context, cpID string) (map[string]string, error) {
	return s.base.List(ctx, s.ns.wrap(cpID))
}

// Delete removes a configuration value for cpID within this tenant.
func (s *NamespacedConfigStore) Delete(ctx context.Context, cpID, key string) error {
	return s.base.Delete(ctx, s.ns.wrap(cpID), key)
}
