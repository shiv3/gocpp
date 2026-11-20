package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func newTestConn(t *testing.T, mutate func(*Config)) (*Conn, *transport.FakeWS) {
	t.Helper()
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(nil) })
	return c, f
}

func waitQueued(t *testing.T, c *Conn, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return c.Stats().QueuedCalls == n }, 2*time.Second, time.Millisecond)
}

func TestDefaultConfigSerializesCalls(t *testing.T) {
	require.True(t, DefaultConfig().SerializeOutboundCalls)
}

// Sync Call and CallAsync wait in one FIFO: they are sent in the order asked.
func TestSlot_SyncAndAsyncShareOneOrder(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	c, f := newTestConn(t, nil)

	var wg sync.WaitGroup
	start := func(name string, async bool) {
		wg.Add(1)
		if async {
			require.NoError(t, DoCallAsync(context.Background(), c, name, []byte(`{}`), func([]byte, error) { wg.Done() }))
			return
		}
		go func() {
			defer wg.Done()
			_, _ = DoCall(context.Background(), c, name, []byte(`{}`))
		}()
	}
	start("A", true)
	first := <-f.Sent()
	require.Equal(t, "A", callAction(t, first))
	start("B", false)
	waitQueued(t, c, 1)
	start("C", true)
	waitQueued(t, c, 2)
	start("D", false)
	waitQueued(t, c, 3)

	f.Inject([]byte(`[3,"` + callID(t, first) + `",{}]`))
	for _, want := range []string{"B", "C", "D"} {
		raw := <-f.Sent()
		require.Equal(t, want, callAction(t, raw))
		f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
	}
	wg.Wait()
}

func TestSlot_SyncQueueFullIsImmediate(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	c, f := newTestConn(t, func(cfg *Config) { cfg.AsyncQueueSize = 1 })

	go func() { _, _ = DoCall(context.Background(), c, "A", []byte(`{}`)) }()
	first := <-f.Sent()
	go func() { _, _ = DoCall(context.Background(), c, "B", []byte(`{}`)) }()
	waitQueued(t, c, 1)

	start := time.Now()
	_, err := DoCall(context.Background(), c, "C", []byte(`{}`))
	require.ErrorIs(t, err, ocppj.ErrQueueFull)
	require.Less(t, time.Since(start), time.Second)

	f.Inject([]byte(`[3,"` + callID(t, first) + `",{}]`))
	second := <-f.Sent()
	f.Inject([]byte(`[3,"` + callID(t, second) + `",{}]`))
}

// A queued async call whose ctx is cancelled is removed, never sent, and its
// callback gets the ctx error; the next call is unaffected.
func TestSlot_AsyncCancelledWhileQueued(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	c, f := newTestConn(t, nil)

	go func() { _, _ = DoCall(context.Background(), c, "A", []byte(`{}`)) }()
	first := <-f.Sent()

	ctx, cancel := context.WithCancel(context.Background())
	cbErr := make(chan error, 1)
	require.NoError(t, DoCallAsync(ctx, c, "B", []byte(`{}`), func(_ []byte, err error) { cbErr <- err }))
	waitQueued(t, c, 1)
	cancel()
	require.ErrorIs(t, <-cbErr, context.Canceled)
	waitQueued(t, c, 0)

	done := make(chan error, 1)
	go func() { _, err := DoCall(context.Background(), c, "C", []byte(`{}`)); done <- err }()
	waitQueued(t, c, 1)
	f.Inject([]byte(`[3,"` + callID(t, first) + `",{}]`))
	next := <-f.Sent()
	require.Equal(t, "C", callAction(t, next), "cancelled call must not be sent")
	f.Inject([]byte(`[3,"` + callID(t, next) + `",{}]`))
	require.NoError(t, <-done)
}

// A peer that never answers frees the slot after CallTimeout, so the queue
// advances; a late CALLRESULT for the timed-out id is ignored.
func TestSlot_UnansweredCallFreesSlotAfterTimeout(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	c, f := newTestConn(t, func(cfg *Config) { cfg.CallTimeout = 100 * time.Millisecond })

	errA := make(chan error, 1)
	go func() { _, err := DoCall(context.Background(), c, "A", []byte(`{}`)); errA <- err }()
	first := <-f.Sent()
	errB := make(chan error, 1)
	waitStart := time.Now()
	go func() { _, err := DoCall(context.Background(), c, "B", []byte(`{}`)); errB <- err }()
	waitQueued(t, c, 1)

	require.ErrorIs(t, <-errA, ocppj.ErrCallTimeout)
	second := <-f.Sent()
	require.Equal(t, "B", callAction(t, second))
	// Late result for A: ignored. B's own timeout started at its write, not at
	// enqueue, so it is still pending.
	f.Inject([]byte(`[3,"` + callID(t, first) + `",{}]`))
	f.Inject([]byte(`[3,"` + callID(t, second) + `",{}]`))
	require.NoError(t, <-errB)
	require.Greater(t, time.Since(waitStart), 90*time.Millisecond)
}

// Replies to inbound requests and SEND bypass the call slot.
func TestSlot_RepliesBypassOutstandingCall(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Reset", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) { return []byte(`{}`), nil })
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() { _, _ = DoCall(context.Background(), c, "A", []byte(`{}`)) }()
	first := <-f.Sent() // A is outstanding and unanswered
	f.Inject([]byte(`[2,"in1","Reset",{}]`))
	require.JSONEq(t, `[3,"in1",{}]`, string(<-f.Sent()))
	f.Inject([]byte(`[2,"in2","Nope",{}]`))
	require.Contains(t, string(<-f.Sent()), "NotImplemented")
	f.Inject([]byte(`[3,"` + callID(t, first) + `",{}]`))
}

// One charger that never answers must not delay calls to other chargers.
func TestSlot_UnresponsiveChargerDoesNotDelayOthers(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	slow, slowWS := newTestConn(t, nil)
	go func() { _, _ = DoCall(context.Background(), slow, "Stuck", []byte(`{}`)) }()
	<-slowWS.Sent()
	for i := 0; i < 3; i++ {
		go func() { _, _ = DoCall(context.Background(), slow, "Behind", []byte(`{}`)) }()
	}
	waitQueued(t, slow, 3)

	fast, fastWS := newTestConn(t, nil)
	start := time.Now()
	for i := 0; i < 20; i++ {
		done := make(chan error, 1)
		go func() { _, err := DoCall(context.Background(), fast, "Heartbeat", []byte(`{}`)); done <- err }()
		raw := <-fastWS.Sent()
		fastWS.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
		require.NoError(t, <-done)
	}
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestSlot_ConnCloseFailsQueuedAsync(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	go func() { _, _ = DoCall(context.Background(), c, "A", []byte(`{}`)) }()
	<-f.Sent()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		require.NoError(t, DoCallAsync(context.Background(), c, "Q", []byte(`{}`), func(_ []byte, err error) { errs <- err }))
	}
	_ = c.Close(nil)
	require.Error(t, <-errs)
	require.Error(t, <-errs)
}

func TestStats(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	c, f := newTestConn(t, nil)
	st := c.Stats()
	require.False(t, st.ConnectedAt.IsZero())
	require.True(t, st.LastSentAt.IsZero())
	require.Zero(t, st.PendingCalls)

	done := make(chan struct{})
	go func() { _, _ = DoCall(context.Background(), c, "A", []byte(`{}`)); close(done) }()
	raw := <-f.Sent()
	require.Eventually(t, func() bool { return c.Stats().PendingCalls == 1 }, time.Second, time.Millisecond)
	st = c.Stats()
	require.False(t, st.LastSentAt.IsZero())

	before := st.LastReceivedAt
	time.Sleep(5 * time.Millisecond)
	f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
	<-done
	require.Eventually(t, func() bool { return c.Stats().LastReceivedAt.After(before) }, time.Second, time.Millisecond)
	// Ping/pong style activity counts too.
	before = c.Stats().LastReceivedAt
	time.Sleep(5 * time.Millisecond)
	c.NoteActivity()
	require.True(t, c.Stats().LastReceivedAt.After(before))
}
