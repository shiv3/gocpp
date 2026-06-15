package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_ReaderResolvesPending(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	ch := make(chan rawResult, 1)
	c.pending.add("m1", &pendingCall{msgID: "m1", respCh: ch})

	// Inject a CallResult for m1.
	f.Inject([]byte(`[3,"m1",{"status":"Accepted"}]`))

	res := <-ch
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"Accepted"}`, string(res.payload))
}
