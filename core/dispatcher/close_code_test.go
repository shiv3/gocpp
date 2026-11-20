package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_CloseSendsNormalClosure(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	require.NoError(t, c.Close(nil))

	code, _, ok := f.CloseCode()
	require.True(t, ok)
	require.Equal(t, transport.StatusNormalClosure, code)
}

func TestConn_AbortReplacedSendsPolicyViolation(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	c.Abort(ocppj.ErrConnReplaced)
	// A following Close must not override the code chosen by the first close.
	require.NoError(t, c.Close(nil))

	code, reason, ok := f.CloseCode()
	require.True(t, ok)
	require.Equal(t, transport.StatusPolicyViolation, code)
	require.Equal(t, replacedCloseReason, reason)
	require.ErrorIs(t, context.Cause(c.Context()), ocppj.ErrConnReplaced)
}
