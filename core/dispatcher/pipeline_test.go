package dispatcher

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func newTestPipeline(mut func(*Config)) Pipeline {
	cfg := DefaultConfig()
	if mut != nil {
		mut(&cfg)
	}
	return NewPipeline(&cfg, ocppj.V16, "CP_1")
}

func callFrame(action, payload string) *ocppj.Frame {
	return &ocppj.Frame{Type: ocppj.Call, MsgID: "m1", Action: action, Payload: []byte(payload)}
}

func TestPipeline_RunCall_Result(t *testing.T) {
	p := newTestPipeline(nil)
	out := p.RunCall(context.Background(), callFrame("Heartbeat", `{}`), func(context.Context, []byte) ([]byte, error) {
		return []byte(`{"a":1}`), nil
	})
	require.Equal(t, "ok", out.Status)
	require.Nil(t, out.Err)
	require.JSONEq(t, `{"a":1}`, string(out.Payload))
}

func TestPipeline_RunCall_NotImplemented(t *testing.T) {
	p := newTestPipeline(nil)
	out := p.RunCall(context.Background(), callFrame("Nope", `{}`), nil)
	require.Equal(t, "not_implemented", out.Status)
	require.Equal(t, ocppj.ErrorCodeNotImplemented, out.Err.Code)
	require.Equal(t, "NotImplemented", out.Wire)
	require.False(t, out.NoReply)
}

func TestPipeline_RunCall_ErrorCodeMapperCanSuppressReply(t *testing.T) {
	p := newTestPipeline(func(c *Config) {
		c.ErrorCodeMapper = func(ErrorContext) (string, bool) { return "", false }
	})
	out := p.RunCall(context.Background(), callFrame("Nope", `{}`), nil)
	require.True(t, out.NoReply)
}

func TestPipeline_RunCall_PanicAndHandlerError(t *testing.T) {
	p := newTestPipeline(nil)
	out := p.RunCall(context.Background(), callFrame("X", `{}`), func(context.Context, []byte) ([]byte, error) {
		panic("boom")
	})
	require.Equal(t, "panic", out.Status)
	require.Equal(t, ocppj.ErrorCodeInternalError, out.Err.Code)

	out = p.RunCall(context.Background(), callFrame("X", `{}`), func(context.Context, []byte) ([]byte, error) {
		return nil, errors.New("db down")
	})
	require.Equal(t, "error", out.Status)
	require.Equal(t, "InternalError", out.Wire)

	out = p.RunCall(context.Background(), callFrame("X", `{}`), func(context.Context, []byte) ([]byte, error) {
		return nil, NewDecodeError(errors.New("bad"))
	})
	require.Equal(t, ocppj.ErrorCodeFormationViolation, out.Err.Code)
}

func TestPipeline_RunCall_SchemaStrictRejectsBeforeHandler(t *testing.T) {
	p := newTestPipeline(func(c *Config) {
		c.SchemaMode = SchemaModeStrict
		c.SchemaValidate = func(ocppj.Version, string, string, []byte) error { return errors.New("nope") }
	})
	called := false
	out := p.RunCall(context.Background(), callFrame("X", `{}`), func(context.Context, []byte) ([]byte, error) {
		called = true
		return nil, nil
	})
	require.False(t, called)
	require.Equal(t, "schema_invalid", out.Status)
	require.Equal(t, ocppj.ErrorCodeFormationViolation, out.Err.Code)
}

func TestPipeline_ValidateInbound_LenientNormalises(t *testing.T) {
	p := newTestPipeline(func(c *Config) {
		c.SchemaMode = SchemaModeLenient
		c.SchemaValidateLenient = func(_ ocppj.Version, _, _ string, pl []byte) ([]byte, []string, error) {
			return []byte(`{"fixed":true}`), []string{"enum"}, nil
		}
	})
	out, err := p.ValidateInbound(context.Background(), "X", "request", []byte(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"fixed":true}`, string(out))
}

func TestHandlerRegistry_Fallback(t *testing.T) {
	r := NewHandlerRegistry()
	_, ok := r.Resolve("A")
	require.False(t, ok)
	r.SetFallback(func(context.Context, *Conn, []byte) ([]byte, error) { return []byte(`{"fb":1}`), nil })
	r.Register("A", func(context.Context, *Conn, []byte) ([]byte, error) { return []byte(`{"a":1}`), nil })

	h, ok := r.Resolve("A")
	require.True(t, ok)
	b, _ := h(context.Background(), nil, nil)
	require.JSONEq(t, `{"a":1}`, string(b))
	h, ok = r.Resolve("B")
	require.True(t, ok)
	b, _ = h(context.Background(), nil, nil)
	require.JSONEq(t, `{"fb":1}`, string(b))
	_, ok = r.Lookup("B")
	require.False(t, ok, "Lookup must not see the fallback")
	r.SetFallback(nil)
	_, ok = r.Resolve("B")
	require.False(t, ok)
}

func TestConn_FallbackHandlesUnregisteredAction(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.SetFallback(func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) { return []byte(`{"fb":true}`), nil })
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"x1","Anything",{}]`))
	require.JSONEq(t, `[3,"x1",{"fb":true}]`, string(<-f.Sent()))
}
