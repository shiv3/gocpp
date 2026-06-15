//go:build go1.25

package dispatcher

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func TestDoCall_Timeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		cfg := DefaultConfig()
		cfg.CallTimeout = time.Second
		c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		// drain the outbound frame so the writer doesn't block
		go func() { <-f.Sent() }()

		type result struct {
			err error
		}
		done := make(chan result, 1)
		go func() {
			_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
			done <- result{err: err}
		}()

		synctest.Wait()
		time.Sleep(2 * time.Second) // advance past CallTimeout

		r := <-done
		require.ErrorIs(t, r.err, ocppj.ErrCallTimeout)
		require.Equal(t, 0, c.pending.len())
	})
}
