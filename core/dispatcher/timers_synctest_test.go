//go:build go1.25

package dispatcher

import (
	"context"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

// Activity at t=59s must push the deadline to t=119s: the timer fires at 60s,
// sees only 1s of idleness, and re-arms for the remaining 59s.
func TestReadWatchdog_RearmsForRemainingTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewConn("CP_1", transport.NewFakeWS("ocpp1.6"), watchdogConfig(time.Minute), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		time.Sleep(59 * time.Second)
		c.NoteActivity()
		time.Sleep(time.Second + time.Millisecond) // past the original deadline
		synctest.Wait()
		require.NoError(t, c.Context().Err(), "watchdog fired early after activity")

		time.Sleep(58*time.Second + 998*time.Millisecond) // t = 119s - 1ms
		synctest.Wait()
		require.NoError(t, c.Context().Err(), "watchdog fired before the re-armed deadline")

		time.Sleep(2 * time.Millisecond) // t = 119s + 1ms
		synctest.Wait()
		require.ErrorContains(t, context.Cause(c.Context()), "read idle timeout")
	})
}

// Close stops both timers: nothing fires afterwards and no goroutine is left,
// even when a ping is blocked waiting for a pong at that moment.
func TestTimers_StoppedOnTeardown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		pinging := make(chan struct{})
		f.SetPingFunc(func(ctx context.Context) error {
			close(pinging)
			<-ctx.Done() // never answered
			return ctx.Err()
		})
		cfg := DefaultConfig()
		cfg.PingInterval = 10 * time.Second
		cfg.PongWait = time.Hour
		cfg.ReadTimeout = time.Hour
		c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
		c.Start(context.Background())

		<-pinging // a ping callback is now parked on its pong
		require.NoError(t, c.Close(nil))
		require.Error(t, c.Context().Err())

		c.tmu.Lock()
		require.True(t, c.timersStopped)
		require.False(t, c.wdTimer.Stop(), "watchdog timer still armed after teardown")
		require.False(t, c.pingTimer.Stop(), "ping timer still armed after teardown")
		c.tmu.Unlock()
		// synctest.Test fails if any goroutine of the bubble outlives it.
	})
}

// A ping that fails cancels the connection, and a successful one is repeated.
func TestPing_FailureCancelsAndSuccessRearms(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		n := 0
		f.SetPingFunc(func(context.Context) error {
			n++
			if n == 3 {
				return context.DeadlineExceeded
			}
			return nil
		})
		cfg := DefaultConfig()
		cfg.PingInterval = time.Minute
		cfg.PongWait = time.Second
		cfg.ReadTimeout = 0
		c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		time.Sleep(3*time.Minute + time.Second)
		synctest.Wait()
		require.Equal(t, 3, n)
		require.ErrorContains(t, context.Cause(c.Context()), "ping:")
	})
}

// Keepalive and teardown add no resident goroutines: only reader, writer and
// dispatch exist per connection, with ping and watchdog on.
func TestTimers_NoResidentGoroutines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		synctest.Wait()
		base := runtime.NumGoroutine()
		cfg := DefaultConfig() // ping and read watchdog enabled
		c := NewConn("CP_1", transport.NewFakeWS("ocpp1.6"), cfg, NewHandlerRegistry())
		c.Start(context.Background())
		synctest.Wait()
		require.Equal(t, 3, runtime.NumGoroutine()-base)
		_ = c.Close(nil)
	})
}
