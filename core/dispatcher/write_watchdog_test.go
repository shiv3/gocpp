//go:build go1.25

package dispatcher

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func wdConfig(wt time.Duration) Config {
	cfg := DefaultConfig()
	cfg.WriteTimeout = wt
	cfg.ReadTimeout = 0
	cfg.PingInterval = 0
	return cfg
}

// A write that starts late in an armed period is still given the full
// WriteTimeout: the timer armed by the first write fires at 10s, finds a write
// that began at 9s and re-arms for the remaining 9s.
func TestWriteWatchdog_ExactTimeoutForLateWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGateWS()
		c := NewConn("CP_1", g, wdConfig(10*time.Second), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		require.NoError(t, c.send(context.Background(), []byte(`[3,"a",{}]`))) // arms the timer at t=0
		<-g.Sent()
		time.Sleep(9 * time.Second)
		g.hold(true)
		errc := make(chan error, 1)
		go func() { errc <- c.send(context.Background(), []byte(`[3,"b",{}]`)) }()

		time.Sleep(9*time.Second + 900*time.Millisecond) // t = 18.9s: 9.9s into the write
		synctest.Wait()
		require.NoError(t, c.Context().Err(), "timed out before WriteTimeout elapsed")
		time.Sleep(200 * time.Millisecond) // t = 19.1s
		synctest.Wait()
		require.ErrorContains(t, context.Cause(c.Context()), "write:")
		require.Error(t, <-errc)
	})
}

// Back-to-back fast writes never trip the watchdog, however long the series.
func TestWriteWatchdog_FastWritesNeverFire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGateWS()
		c := NewConn("CP_1", g, wdConfig(time.Second), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()
		for i := 0; i < 40; i++ {
			require.NoError(t, c.send(context.Background(), []byte(`[3,"a",{}]`)))
			<-g.Sent()
			time.Sleep(600 * time.Millisecond)
		}
		require.NoError(t, c.Context().Err())
	})
}

// Stress the arm/idle race: many quick writes, with the timer firing between
// them, must neither cancel the connection nor leave a stuck write unwatched.
func TestWriteWatchdog_ArmIdleRaceStress(t *testing.T) {
	g := newGateWS()
	c := NewConn("CP_1", g, wdConfig(3*time.Millisecond), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	for i := 0; i < 3000; i++ {
		require.NoError(t, c.send(context.Background(), []byte(`[3,"a",{}]`)), "write %d", i)
		<-g.Sent()
		if i%50 == 0 {
			time.Sleep(4 * time.Millisecond) // let the timer fire while idle
		}
	}
	require.NoError(t, c.Context().Err())
	// The watchdog must still be live after all that idling: a stuck write times out.
	g.hold(true)
	errc := make(chan error, 1)
	go func() { errc <- c.send(context.Background(), []byte(`[3,"z",{}]`)) }()
	select {
	case err := <-errc:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("stuck write not timed out after the timer had idled")
	}
}

// noAbortWS hides Abort so the dispatcher falls back to a context per write.
type noAbortWS struct{ transport.WS }

func TestWriteTimeout_FallbackWithoutAborter(t *testing.T) {
	g := newGateWS()
	g.hold(true)
	cfg := wdConfig(60 * time.Millisecond)
	c := NewConn("CP_1", noAbortWS{g}, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	require.Nil(t, c.aborter)
	err := c.send(context.Background(), []byte(`[3,"a",{}]`))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, context.Cause(c.Context()), "write:")
}
