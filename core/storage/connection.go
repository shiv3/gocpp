// Package storage defines pluggable persistence and routing abstractions. All
// interfaces ship with in-memory defaults in core/storage/memory.
package storage

import "context"

// LiveConn is the minimal view of a live connection the registry needs. The
// dispatcher's *Conn satisfies this, avoiding an import cycle.
type LiveConn interface {
	ID() string
	Close(reason error) error
}

// ConnectionRegistry tracks which charge points are connected and (optionally) on
// which instance in a multi-instance deployment.
type ConnectionRegistry interface {
	PutLocal(ctx context.Context, cpID string, conn LiveConn) error
	GetLocal(cpID string) (LiveConn, bool)
	DeleteLocal(ctx context.Context, cpID string) error
	RangeLocal(fn func(cpID string, conn LiveConn) bool)

	PutGlobal(ctx context.Context, cpID, instanceID string) error
	LookupGlobal(ctx context.Context, cpID string) (instanceID string, ok bool, err error)
	DeleteGlobal(ctx context.Context, cpID string) error
}

// OwnedGlobalDeleter is an optional extension of ConnectionRegistry. A registry
// that implements it lets the server delete a global route only while it still
// names the caller, as one atomic compare-and-delete.
//
// Without it a disconnecting instance has to LookupGlobal and then DeleteGlobal,
// which can erase the route of a newer connection that another instance
// registered in between. The CSMS server uses DeleteGlobalIf when the registry
// provides it and falls back to that check-then-delete otherwise, so existing
// registry implementations keep working unchanged.
type OwnedGlobalDeleter interface {
	// DeleteGlobalIf removes the global route for cpID only if it currently
	// names instanceID. It reports whether the route was removed; a route owned
	// by another instance (or absent) is not an error.
	DeleteGlobalIf(ctx context.Context, cpID, instanceID string) (deleted bool, err error)
}
