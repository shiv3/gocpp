package dispatcher

import (
	"context"
	"sync"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_CloseDuringCall_NoLeakNoPanic(t *testing.T) {
	defer goleak.VerifyNone(t)
	for i := 0; i < 50; i++ {
		f := transport.NewFakeWS("ocpp1.6")
		c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
		c.Start(context.Background())

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		}()
		// Close concurrently with the in-flight call.
		require.NoError(t, c.Close(nil))
		wg.Wait()
		require.Equal(t, 0, c.pending.len())
	}
}
