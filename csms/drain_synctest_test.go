//go:build go1.25

package csms

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeTarget is a session for the pacer: busy until busyUntil.
type fakeTarget struct {
	mu        sync.Mutex
	name      int
	busyUntil time.Time
	closedAt  time.Time
	forced    bool
	doneCh    chan struct{}
}

func newFake(name int) *fakeTarget { return &fakeTarget{name: name, doneCh: make(chan struct{})} }

func (f *fakeTarget) closeIfIdle() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Now().Before(f.busyUntil) {
		return false
	}
	f.closedAt = time.Now()
	close(f.doneCh)
	return true
}

func (f *fakeTarget) quiesce() {}

func (f *fakeTarget) forceClose() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedAt, f.forced = time.Now(), true
	close(f.doneCh)
}

func (f *fakeTarget) done() <-chan struct{} { return f.doneCh }

func targetsOf(fs []*fakeTarget) []drainTarget {
	out := make([]drainTarget, len(fs))
	for i, f := range fs {
		out[i] = f
	}
	return out
}

func TestRunDrain_PacesClosesAtRate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := make([]*fakeTarget, 300)
		for i := range fs {
			fs[i] = newFake(i)
		}
		start := time.Now()
		// 300 sessions in a 10 s window: paced over 9 s, 33.3 per second.
		opts := DrainOptions{Window: 10 * time.Second}.withDefaults(len(fs))
		require.InDelta(t, 300.0/9, opts.Rate, 0.001)
		require.NoError(t, runDrain(context.Background(), targetsOf(fs), opts, nil))

		perSecond := map[int]int{}
		for _, f := range fs {
			require.False(t, f.forced)
			perSecond[int(f.closedAt.Sub(start)/time.Second)]++
		}
		for sec := 0; sec < 8; sec++ { // within 10% of the target rate in every full second
			require.InDelta(t, opts.Rate, perSecond[sec], 0.1*opts.Rate, "second %d: %v", sec, perSecond)
		}
		require.LessOrEqual(t, time.Since(start), 11*time.Second)
	})
}

func TestRunDrain_IdleFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := make([]*fakeTarget, 20)
		for i := range fs {
			fs[i] = newFake(i)
		}
		start := time.Now()
		// The first five are busy for 3 s; the rest are idle.
		for _, f := range fs[:5] {
			f.busyUntil = start.Add(3 * time.Second)
		}
		opts := DrainOptions{Window: 20 * time.Second, Rate: 5, IdleGrace: time.Minute}.withDefaults(len(fs))
		require.NoError(t, runDrain(context.Background(), targetsOf(fs), opts, nil))
		var lastIdle time.Time
		for _, f := range fs[5:] {
			if f.closedAt.After(lastIdle) {
				lastIdle = f.closedAt
			}
		}
		for _, f := range fs[:5] {
			require.False(t, f.forced, "busy sessions are waited for, not forced, within their grace")
			require.True(t, f.closedAt.After(lastIdle) || f.closedAt.Equal(lastIdle), "busy session closed before an idle one")
			require.False(t, f.closedAt.Before(f.busyUntil), "closed while busy")
		}
	})
}

func TestRunDrain_GraceThenForce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFake(0)
		start := time.Now()
		f.busyUntil = start.Add(time.Hour) // never idle in time
		opts := DrainOptions{Window: time.Minute, Rate: 10, IdleGrace: 5 * time.Second}.withDefaults(1)
		require.NoError(t, runDrain(context.Background(), []drainTarget{f}, opts, nil))
		require.True(t, f.forced)
		require.InDelta(t, 5, f.closedAt.Sub(start).Seconds(), 0.2, "forced when the grace ran out")
	})
}

func TestRunDrain_WindowForcesTheRest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := []*fakeTarget{newFake(0), newFake(1)}
		start := time.Now()
		for _, f := range fs {
			f.busyUntil = start.Add(time.Hour)
		}
		opts := DrainOptions{Window: 8 * time.Second, Rate: 10, IdleGrace: time.Hour}.withDefaults(2)
		require.NoError(t, runDrain(context.Background(), targetsOf(fs), opts, nil))
		for _, f := range fs {
			require.True(t, f.forced)
			require.InDelta(t, 8, f.closedAt.Sub(start).Seconds(), 0.2)
		}
	})
}

func TestRunDrain_ContextCancelLeavesSessionsOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := make([]*fakeTarget, 100)
		for i := range fs {
			fs[i] = newFake(i)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		opts := DrainOptions{Window: time.Hour, Rate: 10}.withDefaults(len(fs))
		require.ErrorIs(t, runDrain(ctx, targetsOf(fs), opts, nil), context.DeadlineExceeded)
		closed := 0
		for _, f := range fs {
			if !f.closedAt.IsZero() {
				closed++
			}
		}
		require.InDelta(t, 20, closed, 3)
	})
}

func TestTokenBucket(t *testing.T) {
	b := newTokenBucket(10, 5)
	now := time.Unix(100, 0)
	for i := 0; i < 5; i++ {
		require.True(t, b.allow(now), "burst %d", i)
	}
	require.False(t, b.allow(now))
	require.True(t, b.allow(now.Add(100*time.Millisecond)), "one token after 100ms at 10/s")
	require.False(t, b.allow(now.Add(100*time.Millisecond)))
	require.True(t, b.allow(now.Add(10*time.Second)))
	require.Equal(t, 500*time.Millisecond, b.refillTime())
}
