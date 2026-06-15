package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/semaphore"
)

// A shared GlobalHandlerLimiter must bound the total number of concurrently
// running handlers across all connections, not just per connection.
func TestConn_GlobalLimiterBoundsConcurrencyAcrossConns(t *testing.T) {
	defer goleak.VerifyNone(t)

	started := make(chan string, 2)
	release := make(chan struct{})
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		started <- c.ID()
		<-release
		return []byte(`{"ok":true}`), nil
	})

	cfg := DefaultConfig()
	cfg.GlobalHandlerLimiter = semaphore.NewWeighted(1)

	fa := transport.NewFakeWS("ocpp1.6")
	fb := transport.NewFakeWS("ocpp1.6")
	ca := NewConn("CP_A", fa, cfg, reg)
	cb := NewConn("CP_B", fb, cfg, reg)
	ca.Start(context.Background())
	cb.Start(context.Background())
	defer func() {
		close(release)
		_ = ca.Close(nil)
		_ = cb.Close(nil)
	}()

	// First call on CP_A acquires the single global slot.
	fa.Inject([]byte(`[2,"a1","Heartbeat",{}]`))
	select {
	case id := <-started:
		require.Equal(t, "CP_A", id)
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}

	// A call on a different connection must NOT start while the global limiter
	// is saturated, even though CP_B has its own per-connection budget free.
	fb.Inject([]byte(`[2,"b1","Heartbeat",{}]`))
	select {
	case id := <-started:
		t.Fatalf("second handler %q started despite global limit of 1", id)
	case <-time.After(100 * time.Millisecond):
	}

	// Releasing the first handler frees the global slot; CP_B now proceeds.
	release <- struct{}{}
	select {
	case id := <-started:
		require.Equal(t, "CP_B", id)
	case <-time.After(time.Second):
		t.Fatal("second handler did not start after global slot freed")
	}
}
