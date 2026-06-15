package memory

import (
	"context"

	"github.com/shiv3/gocpp/core/storage"
)

type router struct{}

// NewRouter returns a single-instance no-op router. CallRemote always returns
// storage.ErrRouterNotImplemented; ServeRemote blocks until ctx is cancelled.
func NewRouter() storage.MessageRouter { return router{} }

func (router) CallLocal(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrNotLocal
}
func (router) CallRemote(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrRouterNotImplemented
}
func (router) ServeRemote(ctx context.Context, _ storage.RemoteHandler) error {
	<-ctx.Done()
	return ctx.Err()
}
