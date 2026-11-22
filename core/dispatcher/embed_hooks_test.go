package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestDoCall_OutboundMessageIDOverride(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() {
		raw := <-f.Sent()
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[3,"` + id + `",{}]`))
	}()
	var seen string
	ctx := ocppj.WithMessageIDCallback(ocppj.ContextWithOutboundMessageID(context.Background(), "my-id-1"),
		func(id string) { seen = id })
	_, err := DoCall(ctx, c, "Heartbeat", []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, "my-id-1", seen)
}

func TestDoCall_RawCallErrorDetailsRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() {
		raw := <-f.Sent()
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[4,"` + id + `","GenericError","boom",["a",1,{"b":null}]]`))
	}()
	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	var ce *ocppj.CallError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, `["a",1,{"b":null}]`, string(ce.RawDetails))
}

func TestInbound_RawCallErrorDetailsOnWire(t *testing.T) {
	defer goleak.VerifyNone(t)
	reg := NewHandlerRegistry()
	reg.Register("Reset", func(ctx context.Context, c *Conn, p []byte) ([]byte, error) {
		return nil, &RawCallError{
			Err:  &ocppj.CallError{Code: ocppj.ErrorCodeInternalError, Description: "x", RawDetails: json.RawMessage(`["d",2]`)},
			Wire: "InternalError",
		}
	})
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"m1","Reset",{}]`))
	got := string(<-f.Sent())
	require.Equal(t, `[4,"m1","InternalError","x",["d",2]]`, got)
}

func TestConn_AbortWithStatusSendsChosenCode(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	c.AbortWithStatus(transport.StatusGoingAway, "bye")
	require.NoError(t, c.Close(nil))

	code, reason, ok := f.CloseCode()
	require.True(t, ok)
	require.Equal(t, transport.StatusGoingAway, code)
	require.Equal(t, "bye", reason)
	require.ErrorIs(t, context.Cause(c.Context()), ocppj.ErrConnClosedByServer)
}

func TestConn_AbortWithStatusTruncatesLongReason(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	c.AbortWithStatus(transport.StatusPolicyViolation, strings.Repeat("é", 100))
	require.NoError(t, c.Close(nil))

	_, reason, ok := f.CloseCode()
	require.True(t, ok)
	require.LessOrEqual(t, len(reason), 123)
	require.NotEmpty(t, reason)
}
