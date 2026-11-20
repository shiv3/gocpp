package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// With every handler slot busy and the (small) inbound queue partly used, a
// CALLRESULT must still reach its pending call: the reader only stops once the
// queue is completely full.
func TestInboundQueue_ResultsResolveWhileHandlersSaturated(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.MaxConcurrentHandlers = 2
	cfg.InboundQueueSize = 2
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	reg := NewHandlerRegistry()
	reg.Register("Slow", func(context.Context, *Conn, []byte) ([]byte, error) {
		started <- struct{}{}
		<-release
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"s1","Slow",{}]`))
	f.Inject([]byte(`[2,"s2","Slow",{}]`))
	<-started
	<-started // both slots busy
	// One frame waits in the dispatcher and one in the queue: not yet full.
	f.Inject([]byte(`[2,"s3","Slow",{}]`))
	f.Inject([]byte(`[2,"s4","Slow",{}]`))

	ch := make(chan rawResult, 1)
	c.pending.add("m1", &pendingCall{msgID: "m1", respCh: ch})
	f.Inject([]byte(`[3,"m1",{"status":"Accepted"}]`))
	select {
	case res := <-ch:
		require.NoError(t, res.err)
	case <-time.After(2 * time.Second):
		t.Fatal("CALLRESULT not resolved while handlers are saturated")
	}

	// Queued CALLs are not dropped: they run once slots free up.
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("queued CALL never ran")
		}
	}
}

// A zero or negative queue size must not panic (unbuffered channel).
func TestInboundQueue_NonPositiveSizeIsUnbuffered(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InboundQueueSize, cfg.OutboundQueueSize = -1, 0
	c := NewConn("CP_1", transport.NewFakeWS("ocpp1.6"), cfg, NewHandlerRegistry())
	require.Equal(t, 0, cap(c.in))
	require.Equal(t, 0, cap(c.out))
}
