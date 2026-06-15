package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_OpenAndClose_NoLeak(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	require.NoError(t, c.Close(nil))
	// idempotent
	require.NoError(t, c.Close(nil))
}
