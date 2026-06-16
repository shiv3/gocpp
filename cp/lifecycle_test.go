package cp

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestClientLifecycleCallbacks(t *testing.T) {
	defer goleak.VerifyNone(t)
	drop := errors.New("dropped")
	var events []string
	var disconnectErr error
	client := NewClient(
		"CP_1",
		"ws://example.invalid",
		WithOnConnect(func() { events = append(events, "connect") }),
		WithOnDisconnect(func(err error) {
			events = append(events, "disconnect")
			disconnectErr = err
		}),
		WithOnReconnect(func() { events = append(events, "reconnect") }),
	)

	first := newClientLifecycleConn(t, client)
	client.handleConnected(first, false)
	client.handleDisconnected(first, drop)
	require.NoError(t, first.Close(nil))
	second := newClientLifecycleConn(t, client)
	client.handleConnected(second, true)
	require.NoError(t, second.Close(nil))

	require.Equal(t, []string{"connect", "disconnect", "connect", "reconnect"}, events)
	require.ErrorIs(t, disconnectErr, drop)
}

func TestClientLifecycleNilCallbacksSafe(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := NewClient("CP_1", "ws://example.invalid")
	conn := newClientLifecycleConn(t, client)

	client.handleConnected(conn, true)
	client.handleDisconnected(conn, errors.New("dropped"))
	require.NoError(t, conn.Close(nil))
}

func newClientLifecycleConn(t *testing.T, client *Client) *dispatcher.Conn {
	t.Helper()
	dconn := dispatcher.NewConn(client.id, transport.NewFakeWS("ocpp1.6"), client.cfg.dispatcher, client.reg)
	dconn.Start(context.Background())
	t.Cleanup(func() {
		_ = dconn.Close(nil)
	})
	return dconn
}
