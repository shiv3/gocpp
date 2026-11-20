package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestDoCallAsync_PanickingCallbackIsRecovered(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	require.NoError(t, DoCallAsync(context.Background(), c, "Heartbeat", []byte(`{}`), func([]byte, error) {
		panic("boom")
	}))
	raw := <-f.Sent()
	f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))

	// The connection is still usable after the recovered panic.
	got := make(chan error, 1)
	require.NoError(t, DoCallAsync(context.Background(), c, "Heartbeat", []byte(`{}`), func(_ []byte, err error) { got <- err }))
	raw = <-f.Sent()
	f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
	require.NoError(t, <-got)
}

func TestDoCallAsync_SerializedWorkerSurvivesPanickingCallback(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	second := make(chan error, 1)
	require.NoError(t, DoCallAsync(context.Background(), c, "A", []byte(`{}`), func([]byte, error) { panic("boom") }))
	require.NoError(t, DoCallAsync(context.Background(), c, "B", []byte(`{}`), func(_ []byte, err error) { second <- err }))

	for _, want := range []string{"A", "B"} {
		raw := <-f.Sent()
		require.Equal(t, want, callAction(t, raw))
		f.Inject([]byte(`[3,"` + callID(t, raw) + `",{}]`))
	}
	require.NoError(t, <-second)
}

func TestDoCallAsync_PanickingCallbackDuringDrain(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())

	require.NoError(t, DoCallAsync(context.Background(), c, "A", []byte(`{}`), func([]byte, error) {}))
	<-f.Sent() // worker is now awaiting A's response
	require.NoError(t, DoCallAsync(context.Background(), c, "B", []byte(`{}`), func([]byte, error) { panic("boom") }))

	// Close fails A and drains B through the panicking callback.
	require.NoError(t, c.Close(nil))
}
