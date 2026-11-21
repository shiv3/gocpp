package storage

import "context"

// StateStore keeps small opaque documents that a long-running controller (for
// example the rollout controller, addons/rollout) needs to survive a restart:
// which stage it applied, which revision to roll back to. Documents are keyed by
// the controller and not interpreted by the store. It is implemented next to
// PolicyStore (core/storage/memory, addons/registry-redis) and is optional: a
// controller without one keeps its state in memory only.
//
// Experimental: this API may change before v1.0.
type StateStore interface {
	// PutState stores data under key, replacing any earlier document.
	PutState(ctx context.Context, key string, data []byte) error
	// State returns the document under key; nil data and a nil error when there
	// is none.
	State(ctx context.Context, key string) ([]byte, error)
	// DeleteState removes the document under key (no error when absent).
	DeleteState(ctx context.Context, key string) error
}
