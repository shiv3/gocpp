package registryredis_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/stretchr/testify/require"
)

func TestPolicyCASAndHistory(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		r, _ := newReg(t, b)
		ctx := context.Background()
		rev, data, err := r.Policy(ctx)
		require.NoError(t, err)
		require.Zero(t, rev)
		require.Nil(t, data)

		ok, err := r.PutPolicy(ctx, 0, 1, []byte(`{"default":"a"}`))
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = r.PutPolicy(ctx, 0, 2, []byte(`{"default":"b"}`))
		require.NoError(t, err)
		require.False(t, ok, "CAS miss leaves the stored policy")
		_, err = r.PutPolicy(ctx, 5, 5, nil)
		require.ErrorIs(t, err, storage.ErrPolicyRevision)
		ok, err = r.PutPolicy(ctx, 1, 7, []byte(`{"default":"c"}`))
		require.NoError(t, err)
		require.True(t, ok)

		rev, data, err = r.Policy(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 7, rev)
		require.JSONEq(t, `{"default":"c"}`, string(data))

		h, err := r.PolicyHistory(ctx)
		require.NoError(t, err)
		require.Len(t, h, 2)
		require.EqualValues(t, 7, h[0].Rev)
		require.EqualValues(t, 1, h[1].Rev)
		for i := uint64(8); i < 40; i++ {
			ok, err := r.PutPolicy(ctx, i-1, i, []byte(`{}`))
			require.NoError(t, err)
			require.True(t, ok)
		}
		h, _ = r.PolicyHistory(ctx)
		require.Len(t, h, storage.PolicyHistoryLimit)
	})
}

// A watcher sees the current policy and every newer revision within well
// under a second (push), and a second watcher on another connection too.
func TestWatchPolicyPropagatesFast(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		w := b.client(t)
		t.Cleanup(func() { _ = w.Close() })
		writer, _ := newReg(t, b)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var mu sync.Mutex
		got := map[string]uint64{}
		var wg sync.WaitGroup
		for _, name := range []string{"gw1", "gw2"} {
			c := b.client(t)
			t.Cleanup(func() { _ = c.Close() })
			reg := registryredis.New(c, registryredis.WithKeyPrefix(b.prefix), registryredis.WithPolicyPoll(time.Hour))
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = reg.WatchPolicy(ctx, func(rev uint64, _ []byte) { mu.Lock(); got[name] = rev; mu.Unlock() })
			}()
		}
		time.Sleep(200 * time.Millisecond) // subscriptions up
		start := time.Now()
		ok, err := writer.PutPolicy(ctx, 0, 1, []byte(`{}`))
		require.NoError(t, err)
		require.True(t, ok)
		require.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return got["gw1"] == 1 && got["gw2"] == 1
		}, 2*time.Second, 5*time.Millisecond)
		require.Less(t, time.Since(start), time.Second)
		cancel()
		wg.Wait()
	})
}

// With the push lost (a watcher that never receives messages) the poll still
// catches up.
func TestWatchPolicyPollBackstop(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		writer, _ := newReg(t, b)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Publish before the watcher subscribes: only the initial read / poll can see it.
		ok, err := writer.PutPolicy(ctx, 0, 3, []byte(`{}`))
		require.NoError(t, err)
		require.True(t, ok)
		c := redis.NewClient(&redis.Options{Addr: b.client(t).Options().Addr})
		t.Cleanup(func() { _ = c.Close() })
		reg := registryredis.New(c, registryredis.WithKeyPrefix(b.prefix), registryredis.WithPolicyPoll(100*time.Millisecond))
		var mu sync.Mutex
		var seen []uint64
		go func() {
			_ = reg.WatchPolicy(ctx, func(rev uint64, _ []byte) { mu.Lock(); seen = append(seen, rev); mu.Unlock() })
		}()
		require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(seen) == 1 }, time.Second, 10*time.Millisecond)
		time.Sleep(300 * time.Millisecond)
		mu.Lock()
		require.Equal(t, []uint64{3}, seen, "an unchanged revision is not redelivered")
		mu.Unlock()
	})
}
