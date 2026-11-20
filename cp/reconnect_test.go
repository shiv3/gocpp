package cp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestNewBackOffNeverGivesUpByDefault(t *testing.T) {
	c := NewClient("CP_1", "ws://example.invalid")
	bo := c.newBackOff().(*backoff.ExponentialBackOff)
	clk := &fakeClock{now: time.Unix(1000, 0)}
	bo.Clock = clk
	bo.Reset()
	require.NotEqual(t, backoff.Stop, bo.NextBackOff())
	clk.now = clk.now.Add(2 * time.Hour) // far beyond the library's 15 min default
	require.NotEqual(t, backoff.Stop, bo.NextBackOff())
}

func TestNewBackOffHonoursConfiguredLimit(t *testing.T) {
	c := NewClient("CP_1", "ws://example.invalid", WithReconnectBackoff(time.Second, 5*time.Second, time.Minute))
	bo := c.newBackOff().(*backoff.ExponentialBackOff)
	require.Equal(t, time.Second, bo.InitialInterval)
	require.Equal(t, 5*time.Second, bo.MaxInterval)
	clk := &fakeClock{now: time.Unix(1000, 0)}
	bo.Clock = clk
	bo.Reset()
	clk.now = clk.now.Add(2 * time.Minute)
	require.Equal(t, backoff.Stop, bo.NextBackOff())
}

type countingBackOff struct {
	resets atomic.Int32
	nexts  atomic.Int32
}

func (b *countingBackOff) NextBackOff() time.Duration { b.nexts.Add(1); return time.Millisecond }
func (b *countingBackOff) Reset()                     { b.resets.Add(1) }

func TestRunResetsBackOffAfterSuccessfulConnect(t *testing.T) {
	var accepted atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ocpp1.6"}})
		if err != nil {
			return
		}
		accepted.Add(1)
		_ = c.Close(websocket.StatusNormalClosure, "drop")
	}))
	defer ts.Close()

	var mu sync.Mutex
	reconnects := 0
	client := NewClient("CP_1", "ws"+ts.URL[len("http"):],
		WithOnReconnect(func() { mu.Lock(); reconnects++; mu.Unlock() }))
	bo := &countingBackOff{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.run(ctx, bo) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reconnects >= 3
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	// Every connection that was established restarted the schedule.
	require.GreaterOrEqual(t, bo.resets.Load(), int32(3))
}

func TestRunStopsAfterConfiguredMaxElapsed(t *testing.T) {
	client := NewClient("CP_1", "ws://127.0.0.1:1",
		WithReconnectBackoff(time.Millisecond, 5*time.Millisecond, 50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.Error(t, client.Run(ctx))
}
