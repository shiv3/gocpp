package csms

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestServerLifecycleCallbacks(t *testing.T) {
	defer goleak.VerifyNone(t)
	drop := errors.New("dropped")
	conn := newStartedTestConn(t)
	var connected *Conn
	var disconnected *Conn
	var disconnectErr error
	srv := NewServer(
		WithOnConnect(func(c *Conn) { connected = c }),
		WithOnDisconnect(func(c *Conn, err error) {
			disconnected = c
			disconnectErr = err
		}),
	)

	srv.handleConnected(conn)
	srv.handleDisconnected(conn, drop)
	require.NoError(t, conn.inner.Close(nil))

	require.Same(t, conn, connected)
	require.Same(t, conn, disconnected)
	require.ErrorIs(t, disconnectErr, drop)
}

func TestServerLifecycleNilCallbacksSafe(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := NewServer()
	conn := newStartedTestConn(t)

	srv.handleConnected(conn)
	srv.handleDisconnected(conn, errors.New("dropped"))
	require.NoError(t, conn.inner.Close(nil))
}
