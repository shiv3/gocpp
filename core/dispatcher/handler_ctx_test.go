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

// A disconnect must not cancel a handler mid-flight (e.g. a DB write): the
// handler finishes, and the CALLRESULT it produces is dropped silently.
func TestHandlerCtx_NotCancelledByDisconnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	started, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	var seenID string
	reg := NewHandlerRegistry()
	reg.Register("Slow", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		seenID, _ = ocppj.MessageIDFromContext(ctx)
		close(started)
		<-release
		result <- ctx.Err()
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())

	f.Inject([]byte(`[2,"s1","Slow",{}]`))
	<-started
	connCtx, ok := ConnContext(func() context.Context {
		// Observe the connection context the way a handler would.
		return handlerCtxProbe(c)
	}())
	require.True(t, ok)

	_ = c.Close(nil) // disconnect while the handler runs
	select {
	case <-connCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("connection context should be done")
	}
	close(release)
	require.NoError(t, <-result, "handler ctx must survive the disconnect")
	require.Equal(t, "s1", seenID)
	require.Eventually(t, func() bool { return handlersIdle(c) }, time.Second, time.Millisecond)
	select {
	case m := <-f.Sent():
		t.Fatalf("result after disconnect must be dropped, got %s", m)
	default:
	}
}

func handlerCtxProbe(c *Conn) context.Context {
	return c.handlerContext("probe")
}

func TestHandlerCtx_TimeoutSendsInternalError(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Hang", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	cfg := DefaultConfig()
	cfg.HandlerTimeout = 50 * time.Millisecond
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"h1","Hang",{}]`))
	sent := <-f.Sent()
	require.Contains(t, string(sent), `[4,"h1","InternalError"`)
}

func TestHandlerCtx_TimeoutZeroMeansNoDeadline(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	got := make(chan bool, 1)
	reg := NewHandlerRegistry()
	reg.Register("X", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		_, has := ctx.Deadline()
		got <- has
		return []byte(`{}`), nil
	})
	cfg := DefaultConfig()
	cfg.HandlerTimeout = 0
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	f.Inject([]byte(`[2,"x","X",{}]`))
	<-f.Sent()
	require.False(t, <-got)
}

func TestHandlerCtx_ConnFromContextAndDefaultDeadline(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	type seen struct {
		conn *Conn
		ok   bool
		dl   bool
	}
	got := make(chan seen, 1)
	reg := NewHandlerRegistry()
	reg.Register("X", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		cc, ok := ConnFromContext(ctx)
		_, dl := ctx.Deadline()
		got <- seen{cc, ok, dl}
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	f.Inject([]byte(`[2,"x","X",{}]`))
	<-f.Sent()
	s := <-got
	require.True(t, s.ok)
	require.Same(t, c, s.conn)
	require.True(t, s.dl, "default handler timeout applies")
}

// handlersIdle reports that no handler goroutine of c is running and nothing is
// queued.
func handlersIdle(c *Conn) bool {
	c.inMu.Lock()
	defer c.inMu.Unlock()
	return c.running == 0 && len(c.inQ) == 0
}

// HandlerTimeout bounds the handler, not the checks before it: a slow signature
// or schema validation must not eat the handler's budget (review NIT; it started
// before validation on the gateway branch).
func TestHandlerCtx_TimeoutStartsAfterValidation(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	left := make(chan time.Duration, 1)
	reg := NewHandlerRegistry()
	reg.Register("X", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		dl, _ := ctx.Deadline()
		left <- time.Until(dl)
		return []byte(`{}`), nil
	})
	cfg := DefaultConfig()
	cfg.HandlerTimeout = 400 * time.Millisecond
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(ocppj.Version, string, string, []byte) error {
		time.Sleep(300 * time.Millisecond)
		return nil
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	f.Inject([]byte(`[2,"x","X",{}]`))
	<-f.Sent()
	require.Greater(t, <-left, 300*time.Millisecond, "the timeout started before validation")
}
