//go:build go1.25

package transport_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func TestFakeWS_UnderSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		done := make(chan []byte, 1)
		go func() {
			msg, err := f.Read(ctx)
			require.NoError(t, err)
			done <- msg
		}()

		synctest.Wait()
		f.Inject([]byte("hi"))
		require.Equal(t, "hi", string(<-done))
	})
}
