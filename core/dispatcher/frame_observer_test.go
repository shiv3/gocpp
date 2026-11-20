package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type frameRec struct {
	mu  sync.Mutex
	evs []observability.FrameEvent
}

func (r *frameRec) observe(_ context.Context, ev observability.FrameEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *frameRec) snapshot() []observability.FrameEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]observability.FrameEvent(nil), r.evs...)
}

func (r *frameRec) waitN(t *testing.T, n int) []observability.FrameEvent {
	t.Helper()
	require.Eventually(t, func() bool { return len(r.snapshot()) >= n }, 2*time.Second, time.Millisecond)
	return r.snapshot()
}

func TestFrameObserver_InboundAndOutbound(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &frameRec{}
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
		return []byte(`{"currentTime":"x"}`), nil
	})
	cfg := DefaultConfig()
	cfg.FrameObserver = rec.observe
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	in := []byte(`[2,"m1","Heartbeat",{}]`)
	f.Inject(in)
	<-f.Sent()
	evs := rec.waitN(t, 2)

	require.Equal(t, observability.FrameIn, evs[0].Direction)
	require.Equal(t, "CP_1", evs[0].CPID)
	require.Equal(t, ocppj.V16, evs[0].Version)
	require.Equal(t, ocppj.Call, evs[0].Type)
	require.Equal(t, "m1", evs[0].MsgID)
	require.Equal(t, "Heartbeat", evs[0].Action)
	require.Equal(t, in, evs[0].Raw)
	require.False(t, evs[0].Time.IsZero())

	require.Equal(t, observability.FrameOut, evs[1].Direction)
	require.Equal(t, ocppj.CallResult, evs[1].Type)
	require.Equal(t, "m1", evs[1].MsgID)
	require.NoError(t, evs[1].Err)
}

func TestFrameObserver_UnparsableAndCallError(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &frameRec{}
	f := transport.NewFakeWS("ocpp2.0.1")
	cfg := DefaultConfig()
	cfg.FrameObserver = rec.observe
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`not json`))
	f.Inject([]byte(`[2,"bad","Heartbeat"]`)) // bad shape, readable id: answered
	<-f.Sent()
	evs := rec.waitN(t, 3)
	require.Equal(t, []byte(`not json`), evs[0].Raw)
	require.Equal(t, ocppj.MessageType(0), evs[0].Type)
	require.Equal(t, "bad", evs[1].MsgID)
	require.Equal(t, observability.FrameOut, evs[2].Direction)
	require.Equal(t, ocppj.MessageTypeCallError, evs[2].Type)
}

func TestFrameObserver_OutboundCallAndWriteError(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &frameRec{}
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.FrameObserver = rec.observe
	cfg.CallTimeout = 50 * time.Millisecond
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	var id string
	ctx := ocppj.WithMessageIDCallback(context.Background(), func(m string) { id = m })
	_, err := DoCall(ctx, c, "Heartbeat", []byte(`{}`))
	require.ErrorIs(t, err, ocppj.ErrCallTimeout)
	evs := rec.waitN(t, 1)
	require.Equal(t, ocppj.Call, evs[0].Type)
	require.Equal(t, "Heartbeat", evs[0].Action)
	require.NotEmpty(t, id)
	require.Equal(t, id, evs[0].MsgID)

}

type failWriteWS struct{ *transport.FakeWS }

func (failWriteWS) Write(context.Context, []byte) error { return errors.New("write failed") }

func TestFrameObserver_WriteErrorReported(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &frameRec{}
	cfg := DefaultConfig()
	cfg.FrameObserver = rec.observe
	c := NewConn("CP_1", failWriteWS{transport.NewFakeWS("ocpp1.6")}, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.Error(t, err)
	evs := rec.waitN(t, 1)
	require.Equal(t, observability.FrameOut, evs[0].Direction)
	require.EqualError(t, evs[0].Err, "write failed")
	require.Equal(t, "Heartbeat", evs[0].Action)
}

func TestFrameObserver_PanicRecovered(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.FrameObserver = func(context.Context, observability.FrameEvent) { panic("boom") }
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) { return []byte(`{}`), nil })
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	f.Inject([]byte(`[2,"m1","Heartbeat",{}]`))
	require.Contains(t, string(<-f.Sent()), `[3,"m1"`)
}

func TestFrameObserver_SendAndCallResultError(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &frameRec{}
	f := transport.NewFakeWS("ocpp2.1")
	cfg := DefaultConfig()
	cfg.FrameObserver = rec.observe
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	require.NoError(t, DoSend(context.Background(), c, "NotifyPeriodicEventStream", []byte(`{}`)))
	c.sendCallResultError("rid", ocppj.NewCallError(ocppj.ErrorCodeFormatViolation, "bad", nil))
	evs := rec.waitN(t, 2)
	require.Equal(t, ocppj.Send, evs[0].Type)
	require.Equal(t, "NotifyPeriodicEventStream", evs[0].Action)
	require.Equal(t, ocppj.MessageTypeCallResultError, evs[1].Type)
	require.Equal(t, "rid", evs[1].MsgID)
}

func TestMessageIDFromContext_InboundHandlerAndSend(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp2.1")
	got := make(chan string, 2)
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, _ *Conn, _ []byte) ([]byte, error) {
		id, ok := ocppj.MessageIDFromContext(ctx)
		require.True(t, ok)
		got <- id
		return []byte(`{}`), nil
	})
	reg.RegisterSend("Ev", func(ctx context.Context, _ *Conn, _ []byte) error {
		id, _ := ocppj.MessageIDFromContext(ctx)
		got <- id
		return nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	f.Inject([]byte(`[2,"call-1","Heartbeat",{}]`))
	require.Equal(t, "call-1", <-got)
	f.Inject([]byte(`[6,"send-1","Ev",{}]`))
	require.Equal(t, "send-1", <-got)

	_, ok := ocppj.MessageIDFromContext(context.Background())
	require.False(t, ok)
}

func TestMessageIDCallback_PanicRecovered(t *testing.T) {
	ctx := ocppj.WithMessageIDCallback(context.Background(), func(string) { panic("x") })
	require.NotPanics(t, func() { ocppj.NotifyMessageID(ctx, "id") })
}

type connMetricsRec struct {
	recordingMetrics
	mu     sync.Mutex
	depths []int
	pings  int
}

func (r *connMetricsRec) OutboundQueueDepth(_ string, d int) {
	r.mu.Lock()
	r.depths = append(r.depths, d)
	r.mu.Unlock()
}
func (r *connMetricsRec) PingLatency(string, time.Duration) {
	r.mu.Lock()
	r.pings++
	r.mu.Unlock()
}

func TestConnectionMetrics_QueueDepthAndPingLatency(t *testing.T) {
	defer goleak.VerifyNone(t)
	rm := &connMetricsRec{}
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.Metrics = MetricsHookFrom(rm, "ocpp1.6")
	cfg.PingInterval = time.Millisecond
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() { _, _ = DoCall(context.Background(), c, "Heartbeat", []byte(`{}`)) }()
	<-f.Sent()
	require.Eventually(t, func() bool {
		rm.mu.Lock()
		defer rm.mu.Unlock()
		return len(rm.depths) >= 1 && rm.pings >= 1
	}, 2*time.Second, time.Millisecond)
}

func TestMetricsBridge_ConnectionMetricsOptional(t *testing.T) {
	// A Metrics without the optional interface must not break.
	hook := MetricsHookFrom(&recordingMetrics{}, "1.6").(connectionMetricsHook)
	hook.OutboundQueueDepth("1.6", 3)
	hook.PingLatency("1.6", time.Millisecond)
}
