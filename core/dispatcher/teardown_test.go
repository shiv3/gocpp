package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// Teardown is registered with context.AfterFunc, which fires immediately for a
// parent that is already cancelled; done must still close and the socket must be
// closed exactly once.
func TestConn_StartWithCancelledParentTearsDown(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	parent, cancel := context.WithCancelCause(context.Background())
	cancel(ocppj.ErrConnClosedByServer)
	c.Start(parent)

	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("done not closed for a connection started with a cancelled parent")
	}
	code, reason, ok := f.CloseCode()
	require.True(t, ok)
	require.Equal(t, transport.StatusNormalClosure, code)
	require.Equal(t, closedByServerReason, reason)
	require.NoError(t, c.Close(nil))
}

// A handler may close its own connection: Close waits for done, which waits for
// the reader and dispatcher, not for the handler goroutine.
func TestConn_CloseFromHandlerDoesNotDeadlock(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	closed := make(chan error, 1)
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		closed <- c.Close(nil)
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())

	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close from inside a handler deadlocked")
	}
}

// Each close cause maps to one code, and only the first close frame is sent.
func TestConn_CloseCodeByCause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		code   transport.StatusCode
		reason string
	}{
		{"replaced", ocppj.ErrConnReplaced, transport.StatusPolicyViolation, replacedCloseReason},
		{"server", ocppj.ErrConnClosedByServer, transport.StatusNormalClosure, closedByServerReason},
		{"closed", nil, transport.StatusNormalClosure, "closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			f := transport.NewFakeWS("ocpp1.6")
			c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
			c.Start(context.Background())
			require.NoError(t, c.Close(tc.cause))
			code, reason, ok := f.CloseCode()
			require.True(t, ok)
			require.Equal(t, tc.code, code)
			require.Equal(t, tc.reason, reason)
		})
	}
}

// goBG refuses new work once Close is draining, and Close waits for tracked work.
func TestConn_GoBGGatedByClose(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	release := make(chan struct{})
	finished := make(chan struct{})
	require.True(t, c.goBG(func() { <-release; close(finished) }))
	closeDone := make(chan struct{})
	go func() { _ = c.Close(nil); close(closeDone) }()
	select {
	case <-closeDone:
		t.Fatal("Close returned before tracked background work finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-closeDone
	<-finished
	require.False(t, c.goBG(func() {}))
}
