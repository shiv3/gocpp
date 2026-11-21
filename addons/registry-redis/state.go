package registryredis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shiv3/gocpp/core/storage"
)

// stateTTL garbage-collects controller state nobody touched for a month; a live
// controller rewrites it on every change.
const stateTTL = 30 * 24 * time.Hour

var _ storage.StateStore = (*Registry)(nil)

func (r *Registry) stateKey(key string) string { return r.prefix + ":ctl:" + key }

// PutState implements storage.StateStore.
func (r *Registry) PutState(ctx context.Context, key string, data []byte) error {
	if err := r.rdb.Set(ctx, r.stateKey(key), data, stateTTL).Err(); err != nil {
		return fmt.Errorf("registryredis: put state: %w", err)
	}
	return nil
}

// State implements storage.StateStore.
func (r *Registry) State(ctx context.Context, key string) ([]byte, error) {
	b, err := r.rdb.Get(ctx, r.stateKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registryredis: state: %w", err)
	}
	return b, nil
}

// DeleteState implements storage.StateStore.
func (r *Registry) DeleteState(ctx context.Context, key string) error {
	if err := r.rdb.Del(ctx, r.stateKey(key)).Err(); err != nil {
		return fmt.Errorf("registryredis: delete state: %w", err)
	}
	return nil
}
