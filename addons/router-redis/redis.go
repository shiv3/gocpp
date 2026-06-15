package routerredis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type broker interface {
	Publish(ctx context.Context, channel, payload string) error
	Subscribe(ctx context.Context, channel string) (subscription, error)
}

type subscription interface {
	Channel() <-chan *redis.Message
	Close() error
}

type redisBroker struct {
	rdb *redis.Client
}

func (b redisBroker) Publish(ctx context.Context, channel, payload string) error {
	return b.rdb.Publish(ctx, channel, payload).Err()
}

func (b redisBroker) Subscribe(ctx context.Context, channel string) (subscription, error) {
	pubsub := b.rdb.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe %q: %w", channel, err)
	}
	return &redisSubscription{
		pubsub: pubsub,
		ch:     pubsub.Channel(),
	}, nil
}

type redisSubscription struct {
	pubsub *redis.PubSub
	ch     <-chan *redis.Message
}

func (s *redisSubscription) Channel() <-chan *redis.Message {
	return s.ch
}

func (s *redisSubscription) Close() error {
	return s.pubsub.Close()
}
