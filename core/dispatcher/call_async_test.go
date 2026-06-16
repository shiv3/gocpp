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

func callAction(t *testing.T, raw []byte) string {
	t.Helper()
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &arr))
	var action string
	require.NoError(t, json.Unmarshal(arr[2], &action))
	return action
}

func TestDoCallAsync_NilCallback(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	require.Error(t, DoCallAsync(context.Background(), c, "Heartbeat", []byte(`{}`), nil))
}

func TestDoCallAsync_Concurrent(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	const n = 5
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		err := DoCallAsync(context.Background(), c, "Heartbeat", []byte(`{}`), func(resp []byte, err error) {
			if err == nil {
				require.JSONEq(t, `{}`, string(resp))
			}
			results <- err
		})
		require.NoError(t, err)
	}
	for i := 0; i < n; i++ {
		raw := <-f.Sent()
		f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
	}
	for i := 0; i < n; i++ {
		require.NoError(t, <-results)
	}
}

func TestDoCallAsync_SerializedFIFO(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	order := make(chan string, 3)
	for _, name := range []string{"A", "B", "C"} {
		require.NoError(t, DoCallAsync(context.Background(), c, name, []byte(`{}`), func(resp []byte, err error) {
			require.NoError(t, err)
			order <- name
		}))
	}

	for _, want := range []string{"A", "B", "C"} {
		raw := <-f.Sent()
		require.Equal(t, want, callAction(t, raw), "calls must be sent FIFO, one outstanding")
		// No further frame is sent until the in-flight call is answered.
		require.Never(t, func() bool {
			select {
			case <-f.Sent():
				return true
			default:
				return false
			}
		}, 30*time.Millisecond, time.Millisecond)
		f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
		require.Equal(t, want, <-order)
	}
}

func TestDoCallAsync_QueueFull(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	cfg.AsyncQueueSize = 1
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	sink := func(resp []byte, err error) {}
	require.NoError(t, DoCallAsync(context.Background(), c, "A", []byte(`{}`), sink))
	first := <-f.Sent()                                                               // worker took A and is now blocked awaiting its response
	require.NoError(t, DoCallAsync(context.Background(), c, "B", []byte(`{}`), sink)) // fills the size-1 queue
	require.ErrorIs(t, DoCallAsync(context.Background(), c, "C", []byte(`{}`), sink), ocppj.ErrQueueFull)

	f.Inject([]byte(`[3,"` + callID(t, first) + `",{}]`)) // let A complete so the worker drains cleanly
}

func TestDoCallAsync_ConnCloseFailsCallback(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())

	done := make(chan error, 1)
	require.NoError(t, DoCallAsync(context.Background(), c, "Heartbeat", []byte(`{}`), func(resp []byte, err error) {
		done <- err
	}))
	<-f.Sent() // sent, awaiting response

	_ = c.Close(nil)
	require.Error(t, <-done)
}
