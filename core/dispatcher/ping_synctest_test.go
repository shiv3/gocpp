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

func TestConn_WebSocketPingFires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		var pings atomic.Int32
		f.SetPingFunc(func(context.Context) error {
			pings.Add(1)
			return nil
		})
		cfg := DefaultConfig()
		cfg.PingInterval = time.Minute
		cfg.PongWait = time.Second
		c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		synctest.Wait()
		time.Sleep(3*time.Minute + time.Second)
		require.GreaterOrEqual(t, pings.Load(), int32(3))
	})
}
