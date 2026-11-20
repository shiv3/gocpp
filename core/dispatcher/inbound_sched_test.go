package dispatcher

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/semaphore"
)

func resolved(c *Conn, id string) chan rawResult {
	ch := make(chan rawResult, 1)
	c.pending.add(id, &pendingCall{msgID: id, respCh: ch})
	return ch
}

// Only a full queue stops the reader: with the one handler slot busy and the
// queue full, the next CALL parks the reader (and the CALLRESULT behind it) until
// a handler finishes; before that point results resolve.
func TestInboundSched_BackpressureOnlyWhenQueueFull(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.MaxConcurrentHandlers = 1
	cfg.InboundQueueSize = 1
	started := make(chan string, 8)
	release := make(chan struct{})
	reg := NewHandlerRegistry()
	reg.Register("Slow", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		id, _ := ocppj.MessageIDFromContext(ctx)
		started <- id
		<-release
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"s1","Slow",{}]`))
	require.Equal(t, "s1", <-started)
	f.Inject([]byte(`[2,"s2","Slow",{}]`)) // queued: queue now full, reader not blocked yet

	r1 := resolved(c, "m1")
	f.Inject([]byte(`[3,"m1",{}]`))
	select {
	case <-r1:
	case <-time.After(2 * time.Second):
		t.Fatal("CALLRESULT not resolved with handlers saturated and the queue just full")
	}

	f.Inject([]byte(`[2,"s3","Slow",{}]`)) // reader blocks: queue full
	r2 := resolved(c, "m2")
	f.Inject([]byte(`[3,"m2",{}]`))
	select {
	case <-r2:
		t.Fatal("reader kept reading with a full queue")
	case <-time.After(150 * time.Millisecond):
	}

	release <- struct{}{} // s1 done -> s2 popped, room for s3, reader resumes
	require.Equal(t, "s2", <-started)
	select {
	case <-r2:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not resume after a handler finished")
	}
	close(release)
	require.Equal(t, "s3", <-started)
}

// Frames that arrive while the gate is closed are held in order, CALLRESULTs
// still resolve, and release runs the held CALLs in arrival order.
func TestInboundSched_HoldInboundKeepsOrderAndResolvesResults(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.MaxConcurrentHandlers = 1
	cfg.InboundQueueSize = 8
	order := make(chan string, 8)
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		id, _ := ocppj.MessageIDFromContext(ctx)
		order <- id
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.HoldInbound()
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	for _, id := range []string{"h1", "h2", "h3"} {
		f.Inject([]byte(`[2,"` + id + `","Heartbeat",{}]`))
	}
	r := resolved(c, "m1")
	f.Inject([]byte(`[3,"m1",{}]`))
	select {
	case <-r:
	case <-time.After(2 * time.Second):
		t.Fatal("CALLRESULT blocked behind held CALLs")
	}
	select {
	case id := <-order:
		t.Fatalf("handler %s ran while inbound was held", id)
	case <-time.After(100 * time.Millisecond):
	}
	c.ReleaseInbound()
	c.ReleaseInbound() // idempotent
	for _, want := range []string{"h1", "h2", "h3"} {
		select {
		case got := <-order:
			require.Equal(t, want, got)
		case <-time.After(2 * time.Second):
			t.Fatalf("handler %s never ran after release", want)
		}
	}
	// Once released, new frames run immediately.
	f.Inject([]byte(`[2,"h4","Heartbeat",{}]`))
	require.Equal(t, "h4", <-order)
}

// A handler waiting for the shared limiter parks on its own goroutine, not the
// reader: results on that connection keep resolving, and closing the connection
// releases the waiter.
func TestInboundSched_GlobalLimiterWaitDoesNotBlockReader(t *testing.T) {
	defer goleak.VerifyNone(t)
	lim := semaphore.NewWeighted(1)
	require.NoError(t, lim.Acquire(context.Background(), 1)) // another connection holds it
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.GlobalHandlerLimiter = lim
	var ran atomic.Int32
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		ran.Add(1)
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())

	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))
	r := resolved(c, "m1")
	f.Inject([]byte(`[3,"m1",{}]`))
	select {
	case <-r:
	case <-time.After(2 * time.Second):
		t.Fatal("reader blocked on the global limiter")
	}
	require.Zero(t, ran.Load())
	require.NoError(t, c.Close(nil)) // releases the waiting handler without running it
	require.Eventually(t, func() bool { return handlersIdle(c) }, 2*time.Second, time.Millisecond)
	require.Zero(t, ran.Load())
	lim.Release(1)
}

// Close while the reader is parked on a full queue must not hang.
func TestInboundSched_CloseWithFullQueueReleasesReader(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.MaxConcurrentHandlers = 1
	cfg.InboundQueueSize = 1
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	reg := NewHandlerRegistry()
	reg.Register("Slow", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		started <- struct{}{}
		<-release
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	f.Inject([]byte(`[2,"s1","Slow",{}]`))
	<-started
	f.Inject([]byte(`[2,"s2","Slow",{}]`))
	f.Inject([]byte(`[2,"s3","Slow",{}]`)) // reader parks here
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = c.Close(nil); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung with the reader parked on a full queue")
	}
	close(release)
	require.Eventually(t, func() bool { return handlersIdle(c) }, 2*time.Second, time.Millisecond)
}

// Random arrivals and handler durations: every CALL is handled exactly once,
// never more than MaxConcurrentHandlers at a time, and the scheduler returns to
// idle.
func TestInboundSched_RandomArrivalsHandledExactlyOnce(t *testing.T) {
	for seed := int64(1); seed <= 6; seed++ {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			defer goleak.VerifyNone(t)
			rng := rand.New(rand.NewSource(seed))
			maxH := 1 + rng.Intn(4)
			qsize := rng.Intn(5) // includes 0
			n := 150

			f := transport.NewFakeWS("ocpp1.6")
			cfg := DefaultConfig()
			cfg.MaxConcurrentHandlers = int64(maxH)
			cfg.InboundQueueSize = qsize
			var mu sync.Mutex
			seen := map[string]int{}
			var cur, peak atomic.Int32
			reg := NewHandlerRegistry()
			reg.Register("Work", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
				id, _ := ocppj.MessageIDFromContext(ctx)
				v := cur.Add(1)
				for {
					old := peak.Load()
					if v <= old || peak.CompareAndSwap(old, v) {
						break
					}
				}
				time.Sleep(time.Duration(rand.Intn(300)) * time.Microsecond)
				cur.Add(-1)
				mu.Lock()
				seen[id]++
				mu.Unlock()
				return []byte(`{}`), nil
			})
			c := NewConn("CP_1", f, cfg, reg)
			if seed%2 == 0 {
				c.HoldInbound()
				go func() { time.Sleep(5 * time.Millisecond); c.ReleaseInbound() }()
			}
			c.Start(context.Background())
			defer func() { _ = c.Close(nil) }()

			go func() {
				for i := 0; i < n; i++ {
					f.Inject([]byte(fmt.Sprintf(`[2,"w%d","Work",{}]`, i)))
					if rng.Intn(4) == 0 {
						time.Sleep(time.Duration(rng.Intn(200)) * time.Microsecond)
					}
				}
			}()
			for i := 0; i < n; i++ {
				select {
				case <-f.Sent():
				case <-time.After(10 * time.Second):
					t.Fatalf("only %d of %d replies", i, n)
				}
			}
			mu.Lock()
			require.Len(t, seen, n)
			for id, k := range seen {
				require.Equal(t, 1, k, id)
			}
			mu.Unlock()
			require.LessOrEqual(t, int(peak.Load()), maxH)
			require.Eventually(t, func() bool { return handlersIdle(c) }, 2*time.Second, time.Millisecond)
		})
	}
}
