//go:build go1.25

package dispatcher

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func TestConn_KeepaliveFires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		cfg := DefaultConfig()
		cfg.ReadTimeout = 0 // isolate the OCPP-Heartbeat ticker from the read watchdog
		c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		var ticks atomic.Int32
		c.StartKeepalive(time.Minute, func(context.Context) { ticks.Add(1) })

		synctest.Wait()
		time.Sleep(3*time.Minute + time.Second)
		require.GreaterOrEqual(t, ticks.Load(), int32(3))
	})
}
