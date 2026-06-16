package dispatcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_WebSocketPingErrorClosesConnection(t *testing.T) {
	defer goleak.VerifyNone(t)
	want := errors.New("no pong")
	f := transport.NewFakeWS("ocpp1.6")
	f.SetPingFunc(func(context.Context) error {
		return want
	})
	cfg := DefaultConfig()
	cfg.PingInterval = time.Millisecond
	cfg.PongWait = time.Second
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	require.Eventually(t, func() bool {
		return c.Context().Err() != nil
	}, time.Second, time.Millisecond)
	require.ErrorIs(t, context.Cause(c.Context()), want)
	require.Contains(t, context.Cause(c.Context()).Error(), "ping")
}
