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

// watchdogConfig returns a config with the read idle watchdog enabled and the
// active ping loop disabled, so tests exercise the watchdog in isolation.
func watchdogConfig(readTimeout time.Duration) Config {
	cfg := DefaultConfig()
	cfg.PingInterval = 0
	cfg.PongWait = 0
	cfg.ReadTimeout = readTimeout
	return cfg
}

func TestReadWatchdog_CancelsOnIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		c := NewConn("CP_1", f, watchdogConfig(time.Minute), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		synctest.Wait()
		time.Sleep(time.Minute + time.Second)

		require.Error(t, c.Context().Err())
		require.ErrorContains(t, context.Cause(c.Context()), "read idle timeout")
	})
}

func TestReadWatchdog_ResetsOnActivity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		c := NewConn("CP_1", f, watchdogConfig(time.Minute), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		// Activity every 30s (< 60s timeout) for 3 minutes keeps the conn alive.
		for range 6 {
			time.Sleep(30 * time.Second)
			c.NoteActivity()
		}
		synctest.Wait()
		require.NoError(t, c.Context().Err())
	})
}

func TestReadWatchdog_ResetsOnInboundData(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		c := NewConn("CP_1", f, watchdogConfig(time.Minute), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		// An inbound frame every 30s resets the watchdog via reader().
		// A CallResult with an unknown id is a no-op beyond noting activity.
		for range 6 {
			time.Sleep(30 * time.Second)
			f.Inject([]byte(`[3,"x",{}]`))
		}
		synctest.Wait()
		require.NoError(t, c.Context().Err())
	})
}

func TestReadWatchdog_DisabledWhenZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		c := NewConn("CP_1", f, watchdogConfig(0), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		synctest.Wait()
		time.Sleep(5 * time.Minute)
		require.NoError(t, c.Context().Err())
	})
}
