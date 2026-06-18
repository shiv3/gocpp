package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestDoSendWritesFrameAndReturns(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	err := DoSend(context.Background(), c, "NotifyPeriodicEventStream", []byte(`{"id":1}`))
	require.NoError(t, err)
	require.Equal(t, 0, c.pending.len(), "SEND must not create a pending call")

	raw := <-f.Sent()
	frame, parseErr := ocppj.Parse(raw)
	require.NoError(t, parseErr)
	require.Equal(t, ocppj.Send, frame.Type)
	require.Equal(t, "NotifyPeriodicEventStream", frame.Action)
	require.JSONEq(t, `{"id":1}`, string(frame.Payload))
}

func TestDoSendRejectsNon21(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp2.0.1")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	err := DoSend(context.Background(), c, "NotifyPeriodicEventStream", []byte(`{}`))
	require.Error(t, err)
	require.ErrorIs(t, err, ocppj.ErrUnsupportedVersion)
}
