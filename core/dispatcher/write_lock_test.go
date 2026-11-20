package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// gateWS is a FakeWS whose Write can be held until released, honouring the write
// ctx and the transport being closed like a real transport.
type gateWS struct {
	*transport.FakeWS
	mu      sync.Mutex
	block   bool
	entered chan struct{}
	release chan struct{}
	writes  atomic.Int32
	closed  chan struct{}
	once    sync.Once
}

func newGateWS() *gateWS {
	return &gateWS{
		FakeWS:  transport.NewFakeWS("ocpp1.6"),
		entered: make(chan struct{}, 64),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (g *gateWS) hold(v bool) { g.mu.Lock(); g.block = v; g.mu.Unlock() }

func (g *gateWS) Write(ctx context.Context, data []byte) error {
	g.writes.Add(1)
	g.mu.Lock()
	b := g.block
	g.mu.Unlock()
	if b {
		g.entered <- struct{}{}
		select {
		case <-g.release:
		case <-ctx.Done():
			return ctx.Err()
		case <-g.closed:
			return transport.ErrFakeClosed
		}
	}
	return g.FakeWS.Write(ctx, data)
}

func (g *gateWS) Close(code transport.StatusCode, reason string) error {
	g.once.Do(func() { close(g.closed) })
	return g.FakeWS.Close(code, reason)
}

// Concurrent repliers and a caller share one connection: every frame arrives
// intact, and the frame observer sees them in exactly the order they hit the wire.
func TestSend_ConcurrentSendersObserverOrderIsWireOrder(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	var omu sync.Mutex
	var observed []string
	cfg.FrameObserver = func(_ context.Context, ev observability.FrameEvent) {
		if ev.Direction == observability.FrameOut {
			omu.Lock()
			observed = append(observed, string(ev.Raw))
			omu.Unlock()
		}
	}
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())

	const repliers, each = 16, 20
	var wire []string
	wdone := make(chan struct{})
	go func() {
		defer close(wdone)
		for i := 0; i < repliers*each+each; i++ {
			m := <-f.Sent()
			wire = append(wire, string(m))
			// Answer the caller's CALL so DoCall completes.
			if m[1] == '2' {
				var id string
				_, _ = fmt.Sscanf(string(m), `[2,"%36s`, &id)
				f.Inject([]byte(`[3,"` + id[:len(id)-1] + `",{}]`))
			}
		}
	}()
	var wg sync.WaitGroup
	for r := 0; r < repliers; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				c.sendCallResult(fmt.Sprintf("r%d-%d", r, i), []byte(`{}`))
			}
		}(r)
	}
	for i := 0; i < each; i++ {
		_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		require.NoError(t, err)
	}
	wg.Wait()
	<-wdone
	omu.Lock()
	require.Equal(t, wire, observed, "observer order must equal wire order")
	omu.Unlock()
	require.Len(t, wire, repliers*each+each)
	require.NoError(t, c.Close(nil))
}

// A sender waiting for the lock leaves on its own ctx; the one holding it is
// unaffected and the waiter's frame is never written.
func TestSend_WaiterLeavesOnCallerContext(t *testing.T) {
	defer goleak.VerifyNone(t)
	g := newGateWS()
	g.hold(true)
	c := NewConn("CP_1", g, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	first := make(chan error, 1)
	go func() { first <- c.send(context.Background(), []byte(`[3,"a",{}]`)) }()
	<-g.entered // first holds the lock inside Write

	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() { second <- c.send(ctx, []byte(`[3,"b",{}]`)) }()
	require.Eventually(t, func() bool { return c.wwait.Load() == 2 }, time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-second:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("waiting sender did not leave on its context")
	}
	require.Equal(t, int32(1), g.writes.Load(), "the cancelled sender must not have written")
	g.hold(false)
	close(g.release)
	require.NoError(t, <-first)
}

// WriteTimeout ends a write stuck on a slow peer: the sender gets the error,
// the connection is cancelled with a "write:" cause and every waiter is released.
func TestSend_WriteTimeoutCancelsConnectionAndReleasesWaiters(t *testing.T) {
	defer goleak.VerifyNone(t)
	g := newGateWS()
	g.hold(true)
	cfg := DefaultConfig()
	cfg.WriteTimeout = 80 * time.Millisecond
	c := NewConn("CP_1", g, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errs <- c.send(context.Background(), []byte(`[3,"x",{}]`)) }()
		if i == 0 {
			<-g.entered
		}
	}
	for i := 0; i < 4; i++ {
		select {
		case err := <-errs:
			require.Error(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("sender still blocked after WriteTimeout")
		}
	}
	require.ErrorContains(t, context.Cause(c.Context()), "write:")
	require.Equal(t, int32(1), g.writes.Load(), "waiters must not write after the failure")
}

// Close during a blocked write returns promptly (closing the transport unblocks
// the write) and neither a write nor an observer call happens after it.
func TestSend_CloseDuringBlockedWrite(t *testing.T) {
	defer goleak.VerifyNone(t)
	g := newGateWS()
	g.hold(true)
	cfg := DefaultConfig()
	var obs atomic.Int32
	cfg.FrameObserver = func(_ context.Context, ev observability.FrameEvent) {
		if ev.Direction == observability.FrameOut {
			obs.Add(1)
		}
	}
	c := NewConn("CP_1", g, cfg, NewHandlerRegistry())
	c.Start(context.Background())

	res := make(chan error, 2)
	go func() { res <- c.send(context.Background(), []byte(`[3,"a",{}]`)) }()
	<-g.entered
	go func() { res <- c.send(context.Background(), []byte(`[3,"b",{}]`)) }()
	require.Eventually(t, func() bool { return c.wwait.Load() == 2 }, time.Second, time.Millisecond)

	closed := make(chan struct{})
	go func() { _ = c.Close(nil); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return with a write in flight")
	}
	require.Error(t, <-res)
	require.Error(t, <-res)
	before := obs.Load()
	require.Error(t, c.send(context.Background(), []byte(`[3,"c",{}]`)))
	require.Equal(t, before, obs.Load(), "no observer call after Close")
	require.Equal(t, int32(1), g.writes.Load())
}

// The reader answers malformed CALLs from background goroutines, at most
// maxErrReplies at a time, so a flood against a stuck peer neither blocks the
// reader nor fans out.
func TestSend_ErrorReplyFloodIsCapped(t *testing.T) {
	defer goleak.VerifyNone(t)
	g := newGateWS()
	g.hold(true)
	c := NewConn("CP_1", g, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	// Each frame is a CALL with a readable id but a bad shape.
	for i := 0; i < 50; i++ {
		g.Inject([]byte(fmt.Sprintf(`[2,"bad%d"]`, i)))
	}
	<-g.entered
	// The reader must still be consuming (a CALLRESULT behind the flood resolves).
	r := resolved(c, "m1")
	g.Inject([]byte(`[3,"m1",{}]`))
	select {
	case <-r:
	case <-time.After(2 * time.Second):
		t.Fatal("reader blocked behind error replies")
	}
	require.Eventually(t, func() bool { return c.errReplies.Load() == maxErrReplies }, time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	require.LessOrEqual(t, c.errReplies.Load(), int32(maxErrReplies))
	require.LessOrEqual(t, c.wwait.Load(), int32(maxErrReplies))
	g.hold(false)
	close(g.release)
}
