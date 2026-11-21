package csms_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// A service written against gateway.Downstream reaches a charger held by a
// clustered instance through an existing router.
func TestRouterDownstream_CallsThroughTheRouter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	a := newNode(t, hub, "A")
	a.connect(ctx, t, "CP_1", func(action string) *ocppj.CallError {
		if action == "Reset" {
			return nil
		}
		return &ocppj.CallError{Code: ocppj.ErrorCodeNotSupported, Description: "no"}
	})
	d := csms.RouterDownstream(hub.Router("caller"))

	reply, err := d.Call(ctx, gateway.CallRequest{Target: gateway.SessionRef{CPID: "CP_1"}, Action: "Reset",
		Payload: []byte(`{"type":"Soft"}`), Deadline: time.Now().Add(5 * time.Second)})
	require.NoError(t, err)
	require.Nil(t, reply.Error)
	require.JSONEq(t, `{"status":"Accepted"}`, string(reply.Payload))
	require.Equal(t, gateway.Answered, reply.Sent)

	// The charger's CALLERROR keeps its code.
	reply, err = d.Call(ctx, gateway.CallRequest{Target: gateway.SessionRef{CPID: "CP_1"}, Action: "GetConfiguration", Payload: []byte(`{}`)})
	require.NoError(t, err)
	require.NotNil(t, reply.Error)
	require.Equal(t, gateway.ErrKindCallError, reply.Error.Kind)
	require.Equal(t, string(ocppj.ErrorCodeNotSupported), reply.Error.Code)

	// A charger nobody holds: not_connected, nothing was sent.
	reply, err = d.Call(ctx, gateway.CallRequest{Target: gateway.SessionRef{CPID: "NOPE"}, Action: "Reset", Payload: []byte(`{}`)})
	require.NoError(t, err)
	require.ErrorIs(t, reply.Error.Err(), ocppj.ErrNotConnected)
	require.Equal(t, gateway.NotSent, reply.Sent)

	// Routers carry calls only.
	require.ErrorIs(t, d.Send(ctx, gateway.CallRequest{}), gateway.ErrUnsupported)
	require.ErrorIs(t, d.Control(ctx, gateway.ControlRequest{}), gateway.ErrUnsupported)
}

type fakeDownstream struct {
	mu    sync.Mutex
	calls []gateway.CallRequest
	reply gateway.CallReply
	err   error
}

func (f *fakeDownstream) Call(_ context.Context, req gateway.CallRequest) (gateway.CallReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return f.reply, f.err
}
func (f *fakeDownstream) Send(context.Context, gateway.CallRequest) error       { return nil }
func (f *fakeDownstream) Control(context.Context, gateway.ControlRequest) error { return nil }

// A clustered server whose router is a Downstream forwards calls for chargers
// held elsewhere as Downstream.Call requests, with errors rebuilt.
func TestDownstreamRouter_ClusteredServerForwardsThroughDownstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	hub.Bind("CP_9", "elsewhere") // the global route says another instance holds it
	fd := &fakeDownstream{reply: gateway.CallReply{Payload: []byte(`{"status":"Accepted"}`), Sent: gateway.Answered}}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInstanceID("B"),
		csms.WithConnectionRegistry(hub.Registry()), csms.WithMessageRouter(csms.DownstreamRouter(fd))) //nolint:staticcheck // legacy option, under test
	t.Cleanup(srv.Close)

	conn, err := srv.Lookup(ctx, "CP_9")
	require.NoError(t, err)
	require.True(t, conn.Remote())
	resp, err := csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.NoError(t, err)
	require.Equal(t, messages.ResetResponseStatusAccepted, resp.Status)
	require.Len(t, fd.calls, 1)
	require.Equal(t, "CP_9", fd.calls[0].Target.CPID)
	require.Zero(t, fd.calls[0].Target.Epoch, "the current session, whatever its epoch")
	require.Equal(t, "Reset", fd.calls[0].Action)
	require.JSONEq(t, `{"type":"Soft"}`, string(fd.calls[0].Payload))
	require.False(t, fd.calls[0].Deadline.IsZero(), "the caller's deadline travels")

	// A charger-level error keeps its code ...
	fd.reply = gateway.CallReply{Error: gateway.EncodeError(&ocppj.CallError{Code: ocppj.ErrorCodeNotImplemented, Description: "nope"})}
	_, err = csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	var ce *ocppj.CallError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeNotImplemented, ce.Code)

	// ... a sentinel the link reports keeps its identity ...
	fd.reply = gateway.CallReply{Error: gateway.EncodeError(ocppj.ErrConnDropped)}
	_, err = csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.ErrorIs(t, err, ocppj.ErrConnDropped)

	// ... and so does "not connected", from the link's error or from its reply.
	fd.reply = gateway.CallReply{}
	fd.err = ocppj.ErrNotConnected
	_, err = csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.ErrorIs(t, err, ocppj.ErrNotConnected)

	// Other transport errors are reported as they are.
	fd.err = errors.New("link down")
	_, err = csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.ErrorContains(t, err, "link down")
}
