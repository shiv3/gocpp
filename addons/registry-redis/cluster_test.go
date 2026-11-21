package registryredis_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/stretchr/testify/require"
)

// scanCounter counts SCAN/KEYS commands sent to the server.
type scanCounter struct{ n int }

func (s *scanCounter) DialHook(next redis.DialHook) redis.DialHook { return next }
func (s *scanCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if n := strings.ToLower(cmd.Name()); n == "scan" || n == "keys" {
			s.n++
		}
		return next(ctx, cmd)
	}
}
func (s *scanCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if n := strings.ToLower(cmd.Name()); n == "scan" || n == "keys" {
				s.n++
			}
		}
		return next(ctx, cmds)
	}
}

// Gateways must not scan the keyspace (a million charger records live next to the
// leases) and must still drop gateways whose lease lapsed.
func TestGatewaysUsesLeaseIndexNotScan(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	sc := &scanCounter{}
	c.AddHook(sc)
	r := registryredis.New(c, registryredis.WithKeyPrefix("t"))
	ctx := context.Background()
	for _, id := range []string{"gw-0", "gw-1", "gw-2"} {
		require.NoError(t, r.Heartbeat(ctx, storage.GatewayInfo{ID: id, Boot: "b-" + id, Sessions: 3, Labels: map[string]string{"admin": id + ":9101"}}, 10*time.Second))
	}
	gws, err := r.Gateways(ctx)
	require.NoError(t, err)
	require.Len(t, gws, 3)
	require.Zero(t, sc.n, "no SCAN/KEYS")

	mr.FastForward(6 * time.Second)
	require.NoError(t, r.Heartbeat(ctx, storage.GatewayInfo{ID: "gw-0", Boot: "b-gw-0"}, 10*time.Second))
	mr.FastForward(6 * time.Second) // gw-1 and gw-2 lapse
	gws, err = r.Gateways(ctx)
	require.NoError(t, err)
	require.Len(t, gws, 1)
	require.Equal(t, "gw-0", gws[0].ID)
	members, err := c.SMembers(ctx, "t:gwidx").Result()
	require.NoError(t, err)
	require.Equal(t, []string{"gw-0"}, members, "lapsed leases leave the index")
	require.Zero(t, sc.n)
}

// On Redis Cluster the script's keys must hash to one slot: the three policy keys
// share one hash tag.
func TestPolicyKeysShareOneHashSlot(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	r := registryredis.New(c, registryredis.WithKeyPrefix("t"))
	ok, err := r.PutPolicy(context.Background(), 0, 1, []byte("x: 1"))
	require.NoError(t, err)
	require.True(t, ok)
	keys := mr.Keys()
	require.Len(t, keys, 2, "policy and its history: %v", keys)
	for _, k := range keys {
		require.Contains(t, k, "{policy}", "key %q must carry the shared hash tag", k)
	}
}
