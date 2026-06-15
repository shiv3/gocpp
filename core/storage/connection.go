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
