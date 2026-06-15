package storage

import (
	"context"
	"errors"
)

// ErrNotLocal indicates the target charge point is not connected to this instance.
var ErrNotLocal = errors.New("storage: charge point not local")

// ErrRouterNotImplemented indicates remote routing is unsupported (single-instance).
var ErrRouterNotImplemented = errors.New("storage: remote routing not implemented")

// RemoteHandler serves a forwarded call on the instance that holds the connection.
type RemoteHandler func(ctx context.Context, cpID, action string, req []byte) ([]byte, error)

// MessageRouter forwards calls between CSMS instances. Single-instance deployments
// use a no-op router whose CallRemote returns ErrRouterNotImplemented.
type MessageRouter interface {
	CallLocal(ctx context.Context, cpID, action string, req []byte) ([]byte, error)
	CallRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error)
	ServeRemote(ctx context.Context, handler RemoteHandler) error
}
