package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestRateBucket(t *testing.T) {
	var b rateBucket
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		require.True(t, b.allow(10, 3, now), "burst token %d", i)
	}
	require.False(t, b.allow(10, 3, now))
	require.True(t, b.allow(10, 3, now.Add(100*time.Millisecond)), "10/s refills one token in 100ms")
	require.False(t, b.allow(10, 3, now.Add(100*time.Millisecond)))
	// Long idle never banks more than the burst.
	later := now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		require.True(t, b.allow(10, 3, later))
	}
	require.False(t, b.allow(10, 3, later))

	var d rateBucket // burst < 1 -> ceil(rate), at least 1
	require.True(t, d.allow(0.5, 0, now))
	require.False(t, d.allow(0.5, 0, now))
}

type rateRecorder struct {
	MetricsHook
	mu  sync.Mutex
	out []string
}

func (r *rateRecorder) InboundRateLimited(_, outcome string) {
	r.mu.Lock()
	r.out = append(r.out, outcome)
	r.mu.Unlock()
}

func newRateConn(cfg Config, calls *atomic.Int32) (*Conn, *transport.FakeWS) {
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
		calls.Add(1)
		return []byte(`{"currentTime":"2026-01-01T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	return c, f
}

func TestInboundRate_RejectAnswersGenericError(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &rateRecorder{MetricsHook: NoopMetrics}
	cfg := DefaultConfig()
	cfg.Metrics = rec
	cfg.InboundRate = 0.001 // effectively no refill during the test
	cfg.InboundBurst = 2
	var calls atomic.Int32
	c, f := newRateConn(cfg, &calls)
	defer func() { _ = c.Close(nil) }()

	var codes []string
	for _, id := range []string{"a", "b", "c", "d"} {
		f.Inject([]byte(`[2,"` + id + `","Heartbeat",{}]`))
		var arr []any
		require.NoError(t, json.Unmarshal(<-f.Sent(), &arr))
		if arr[0].(float64) == 4 {
			codes = append(codes, arr[2].(string))
		} else {
			codes = append(codes, "ok")
		}
	}
	require.Equal(t, []string{"ok", "ok", "GenericError", "GenericError"}, codes)
	require.EqualValues(t, 2, calls.Load(), "refused calls must not reach the handler")
	require.NoError(t, context.Cause(c.Context()), "reject keeps the connection")
	rec.mu.Lock()
	require.Equal(t, []string{"rejected", "rejected"}, rec.out)
	rec.mu.Unlock()
}

func TestInboundRate_ErrorCodeMapperApplies(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := DefaultConfig()
	cfg.InboundRate, cfg.InboundBurst = 0.001, 1
	cfg.ErrorCodeMapper = func(ctx ErrorContext) (string, bool) {
		if ctx.Source == ErrorSourceRateLimit {
			require.ErrorIs(t, ctx.Err, ErrInboundRateLimited)
			return "SecurityError", true
		}
		return ctx.WireCode, ctx.Reply
	}
	var calls atomic.Int32
	c, f := newRateConn(cfg, &calls)
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"a","Heartbeat",{}]`))
	<-f.Sent()
	f.Inject([]byte(`[2,"b","Heartbeat",{}]`))
	var arr []any
	require.NoError(t, json.Unmarshal(<-f.Sent(), &arr))
	require.Equal(t, "SecurityError", arr[2])
}

func TestInboundRate_CloseUses1008(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &rateRecorder{MetricsHook: NoopMetrics}
	cfg := DefaultConfig()
	cfg.Metrics = rec
	cfg.InboundRate, cfg.InboundBurst = 0.001, 1
	cfg.InboundRateAction = RateLimitClose
	var calls atomic.Int32
	c, f := newRateConn(cfg, &calls)
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"a","Heartbeat",{}]`))
	<-f.Sent()
	f.Inject([]byte(`[2,"b","Heartbeat",{}]`))
	select {
	case <-c.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("not closed")
	}
	var cs *CloseStatus
	require.True(t, errors.As(context.Cause(c.Context()), &cs))
	require.Equal(t, transport.StatusPolicyViolation, cs.Code)
	require.Eventually(t, func() bool {
		code, reason, ok := f.CloseCode()
		return ok && code == transport.StatusPolicyViolation && reason == rateLimitCloseReason
	}, 2*time.Second, 5*time.Millisecond)
}

func TestInboundRate_ResponsesAndDefaultUnlimited(t *testing.T) {
	defer goleak.VerifyNone(t)
	var calls atomic.Int32
	c, f := newRateConn(DefaultConfig(), &calls)
	defer func() { _ = c.Close(nil) }()
	for i := 0; i < 50; i++ {
		f.Inject([]byte(`[2,"m` + string(rune('A'+i%26)) + string(rune('a'+i/26)) + `","Heartbeat",{}]`))
		<-f.Sent()
	}
	require.EqualValues(t, 50, calls.Load())

	// A CALLRESULT is never limited even with an exhausted bucket.
	cfg := DefaultConfig()
	cfg.InboundRate, cfg.InboundBurst = 0.001, 1
	var n atomic.Int32
	c2, f2 := newRateConn(cfg, &n)
	defer func() { _ = c2.Close(nil) }()
	f2.Inject([]byte(`[2,"a","Heartbeat",{}]`))
	<-f2.Sent()
	done := make(chan error, 1)
	go func() { _, err := DoCall(context.Background(), c2, "Heartbeat", []byte(`{}`)); done <- err }()
	raw := <-f2.Sent()
	f2.Inject([]byte(`[3,"` + callID(t, raw) + `",{"currentTime":"2026-01-01T00:00:00Z"}]`))
	select {
	case err := <-done:
		var ce *ocppj.CallError
		require.False(t, errors.As(err, &ce), "got %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("call not resolved")
	}
}
