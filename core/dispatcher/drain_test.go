package dispatcher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_TryDrainIdleClosesWith1012(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	require.True(t, c.TryDrain())
	require.True(t, c.Draining())
	require.NoError(t, c.Close(nil))
	code, reason, ok := f.CloseCode()
	require.True(t, ok)
	require.Equal(t, transport.StatusServiceRestart, code)
	require.Equal(t, drainingCloseReason, reason)
	require.ErrorIs(t, context.Cause(c.Context()), ocppj.ErrGatewayDraining)
}

func TestConn_TryDrainSkipsBusyAndRefusesNewCallsAfter(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	// A call in flight: the connection is busy.
	type res struct {
		out []byte
		err error
	}
	done := make(chan res, 1)
	go func() {
		out, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		done <- res{out, err}
	}()
	raw := <-f.Sent()
	require.False(t, c.TryDrain(), "a call awaits its reply")
	require.False(t, c.Draining())

	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &arr))
	var id string
	require.NoError(t, json.Unmarshal(arr[1], &id))
	f.Inject([]byte(`[3,"` + id + `",{"currentTime":"x"}]`))
	r := <-done
	require.NoError(t, r.err)

	// Idle again: it drains, and calls started afterwards fail unsent.
	require.Eventually(t, c.TryDrain, time.Second, time.Millisecond)
	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.ErrorIs(t, err, ocppj.ErrGatewayDraining)
	select {
	case m := <-f.Sent():
		t.Fatalf("a call written after the drain began: %s", m)
	default:
	}
}

func TestConn_ForceDrainFailsInFlightCallWithDrainTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	errc := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		errc <- err
	}()
	<-f.Sent()
	c.ForceDrain()
	err := <-errc
	require.ErrorIs(t, err, ocppj.ErrDrainTimeout)
	require.NotErrorIs(t, err, ocppj.ErrGatewayDraining, "an in-flight call is not provably unexecuted")
	require.NoError(t, c.Close(nil))
	code, _, _ := f.CloseCode()
	require.Equal(t, transport.StatusServiceRestart, code)
}

func TestConn_TryDrainSkipsConnectionWithInboundHandler(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	started, release := make(chan struct{}), make(chan struct{})
	reg.Register("Heartbeat", func(_ context.Context, _ *Conn, _ []byte) ([]byte, error) {
		close(started)
		<-release
		return []byte(`{}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"1","Heartbeat",{}]`))
	<-started
	require.False(t, c.TryDrain(), "an inbound handler is running")
	close(release)
	<-f.Sent()
	require.Eventually(t, c.TryDrain, time.Second, time.Millisecond)
}

func TestConn_QuiesceLetsInFlightCallFinish(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	errc := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		errc <- err
	}()
	raw := <-f.Sent()
	c.Quiesce()
	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.ErrorIs(t, err, ocppj.ErrGatewayDraining, "new calls are refused")
	require.False(t, c.TryDrain(), "the first call is still in flight")

	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &arr))
	var id string
	require.NoError(t, json.Unmarshal(arr[1], &id))
	f.Inject([]byte(`[3,"` + id + `",{}]`))
	require.NoError(t, <-errc, "the in-flight call completes")
	require.Eventually(t, c.TryDrain, time.Second, time.Millisecond)
}
