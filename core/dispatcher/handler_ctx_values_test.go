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

type ctxKey struct{}

// The slimmed handler context still shows handlers what it always did: the
// connection, its lifecycle context, the message id, the deadline and the
// connection context's values, and it is not cancelled by the connection.
func TestHandlerCtx_ValuesUnchanged(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.HandlerTimeout = time.Minute
	type seen struct {
		conn     *Conn
		connCtx  context.Context
		id       string
		deadline time.Time
		val      any
		err      error
	}
	got := make(chan seen, 1)
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		cc, _ := ConnContext(ctx)
		fc, _ := ConnFromContext(ctx)
		id, _ := ocppj.MessageIDFromContext(ctx)
		dl, _ := ctx.Deadline()
		got <- seen{fc, cc, id, dl, ctx.Value(ctxKey{}), ctx.Err()}
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.WithValue(context.Background(), ctxKey{}, "v"))
	defer func() { _ = c.Close(nil) }()

	before := time.Now()
	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))
	s := <-got
	require.Same(t, c, s.conn)
	require.Equal(t, c.Context(), s.connCtx)
	require.Equal(t, "h1", s.id)
	require.Equal(t, "v", s.val)
	require.NoError(t, s.err)
	require.WithinDuration(t, before.Add(time.Minute), s.deadline, 5*time.Second)
	_, ok := ConnContext(context.Background())
	require.False(t, ok)
}
