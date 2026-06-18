package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestReaderCallResultErrorResolvesPending(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	ch := make(chan rawResult, 1)
	c.pending.add("m1", &pendingCall{msgID: "m1", respCh: ch})

	// Inject a CALLRESULTERROR frame for m1.
	f.Inject([]byte(`[5,"m1","FormatViolation","bad",{}]`))

	res := <-ch
	var ce *ocppj.CallError
	require.ErrorAs(t, res.err, &ce)
	require.Equal(t, ocppj.ErrorCode("FormatViolation"), ce.Code)
	require.True(t, ce.IsResultError)
}
