package dispatcher

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func poolConn(t *testing.T, cfg Config, reg *HandlerRegistry) (*Conn, *transport.FakeWS) {
	t.Helper()
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	return c, f
}

// Handlers run on pool workers when one is idle, and the pool's goroutines are
// the ones that execute them (the handler sees a goroutine that outlives it).
func TestHandlerPool_RunsHandlersOnWorkers(t *testing.T) {
	defer goleak.VerifyNone(t)
	pool := NewHandlerPool(2)
	defer pool.Close()
	cfg := DefaultConfig()
	cfg.HandlerPool = pool
	var calls atomic.Int32
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
		calls.Add(1)
		return []byte(`{}`), nil
	})
	c, f := poolConn(t, cfg, reg)
	defer func() { _ = c.Close(nil) }()
	for i := 0; i < 50; i++ {
		f.Inject([]byte(`[2,"h","Heartbeat",{}]`))
		<-f.Sent()
	}
	require.Equal(t, int32(50), calls.Load())
}

// The pool is a hand-off, never a queue: with every worker stuck in a slow
// handler, another connection's handler still starts at once on a new goroutine.
func TestHandlerPool_SlowHandlersDoNotDelayOthers(t *testing.T) {
	defer goleak.VerifyNone(t)
	pool := NewHandlerPool(1)
	defer pool.Close()
	cfg := DefaultConfig()
	cfg.HandlerPool = pool
	block := make(chan struct{})
	entered := make(chan string, 4)
	reg := NewHandlerRegistry()
	reg.Register("Slow", func(_ context.Context, c *Conn, _ []byte) ([]byte, error) {
		entered <- c.ID()
		<-block
		return []byte(`{}`), nil
	})
	reg.Register("Fast", func(_ context.Context, c *Conn, _ []byte) ([]byte, error) { return []byte(`{}`), nil })
	a, fa := poolConn(t, cfg, reg)
	defer func() { _ = a.Close(nil) }()
	fb := transport.NewFakeWS("ocpp1.6")
	b := NewConn("CP_B", fb, cfg, reg)
	b.Start(context.Background())
	defer func() { _ = b.Close(nil) }()

	fa.Inject([]byte(`[2,"s1","Slow",{}]`))
	<-entered // the single worker (or a goroutine) is now busy
	fb.Inject([]byte(`[2,"s2","Slow",{}]`))
	<-entered // ran although the pool has no idle worker
	fb.Inject([]byte(`[2,"f1","Fast",{}]`))
	close(block)
	for i := 0; i < 2; i++ {
		select {
		case <-fa.Sent():
		case <-fb.Sent():
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not finish")
		}
	}
}

// Workers are replaced after handlerPoolRecycle tasks, keeping the pool at its
// size, and Close waits for them all.
func TestHandlerPool_WorkersRecycleAndClose(t *testing.T) {
	defer goleak.VerifyNone(t)
	old := handlerPoolRecycle
	handlerPoolRecycle = 3
	defer func() { handlerPoolRecycle = old }()
	pool := NewHandlerPool(1)
	cfg := DefaultConfig()
	cfg.HandlerPool = pool
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) { return []byte(`{}`), nil })
	c, f := poolConn(t, cfg, reg)
	for i := 0; i < 40; i++ {
		f.Inject([]byte(`[2,"h","Heartbeat",{}]`))
		<-f.Sent()
		runtime.Gosched()
	}
	require.NoError(t, c.Close(nil))
	pool.Close()
	pool.Close() // idempotent
	// After Close, dispatch falls back to plain goroutines.
	c2, f2 := poolConn(t, cfg, reg)
	f2.Inject([]byte(`[2,"h","Heartbeat",{}]`))
	<-f2.Sent()
	require.NoError(t, c2.Close(nil))
}
