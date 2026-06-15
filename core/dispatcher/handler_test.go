package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_HandlerRespondsCallResult(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer c.Close(nil)

	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"h1",{"currentTime":"2026-06-15T00:00:00Z"}]`, string(sent))
}

func TestConn_UnknownActionReturnsNotImplemented(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer c.Close(nil)

	f.Inject([]byte(`[2,"x1","Nonexistent",{}]`))

	sent := <-f.Sent()
	require.Contains(t, string(sent), "NotImplemented")
	require.Contains(t, string(sent), `"x1"`)
}

func TestConn_HandlerPanicReturnsInternalErrorAndSurvives(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Boom", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		panic("handler blew up")
	})
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer c.Close(nil)

	// A panicking handler must not crash the process; it replies InternalError.
	f.Inject([]byte(`[2,"b1","Boom",{}]`))
	sent := <-f.Sent()
	require.Contains(t, string(sent), "InternalError")
	require.Contains(t, string(sent), `"b1"`)

	// The connection must still serve subsequent requests.
	f.Inject([]byte(`[2,"h2","Heartbeat",{}]`))
	sent = <-f.Sent()
	require.JSONEq(t, `[3,"h2",{"currentTime":"2026-06-15T00:00:00Z"}]`, string(sent))
}

func TestConn_HandlerErrorReturnsCallError(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Authorize", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return nil, ocppj.NewCallError(ocppj.ErrorCodePropertyConstraintViolation, "bad idTag", nil)
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer c.Close(nil)

	f.Inject([]byte(`[2,"a1","Authorize",{}]`))
	sent := <-f.Sent()
	require.Contains(t, string(sent), "PropertyConstraintViolation")
}
