package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type badFrameRecorder struct {
	MetricsHook
	mu      sync.Mutex
	reasons []string
}

func (r *badFrameRecorder) BadFrame(_, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasons = append(r.reasons, reason)
}

func (r *badFrameRecorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reasons...)
}

func newBadFrameConn(t *testing.T, cfg Config) (*Conn, *transport.FakeWS) {
	t.Helper()
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	return c, f
}

func TestMaxBadFrames_ClosesWithPolicyViolation(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &badFrameRecorder{MetricsHook: NoopMetrics}
	cfg := DefaultConfig()
	cfg.MaxBadFrames = 3
	cfg.Metrics = rec
	c, f := newBadFrameConn(t, cfg)
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`not json`))
	f.Inject([]byte(`[9,"x","Foo",{}]`))
	select {
	case <-c.Context().Done():
		t.Fatal("closed before the limit")
	case <-time.After(100 * time.Millisecond):
	}
	f.Inject([]byte(`{"a":1}`))

	select {
	case <-c.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("connection not closed")
	}
	var cs *CloseStatus
	require.True(t, errors.As(context.Cause(c.Context()), &cs))
	require.Equal(t, transport.StatusPolicyViolation, cs.Code)
	require.Eventually(t, func() bool {
		code, reason, ok := f.CloseCode()
		return ok && code == transport.StatusPolicyViolation && reason == badFrameCloseReason
	}, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"parse", "unknown_type", "parse"}, rec.got())
}

func TestMaxBadFrames_OffByDefaultStillCounts(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &badFrameRecorder{MetricsHook: NoopMetrics}
	cfg := DefaultConfig()
	cfg.Metrics = rec
	c, f := newBadFrameConn(t, cfg)
	defer func() { _ = c.Close(nil) }()

	for i := 0; i < 20; i++ {
		f.Inject([]byte(`garbage`))
	}
	require.Eventually(t, func() bool { return len(rec.got()) == 20 }, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, context.Cause(c.Context()))
}

func TestMaxBadFrames_WindowRestartsCount(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := DefaultConfig()
	cfg.MaxBadFrames = 2
	cfg.BadFrameWindow = 50 * time.Millisecond
	c, _ := newBadFrameConn(t, cfg)
	defer func() { _ = c.Close(nil) }()

	for i := 0; i < 4; i++ {
		c.noteBadFrame("parse")
		time.Sleep(80 * time.Millisecond)
	}
	require.NoError(t, context.Cause(c.Context()), "frames spread over several windows must not trip the limit")

	c.noteBadFrame("parse")
	c.noteBadFrame("parse")
	require.Error(t, context.Cause(c.Context()))
}

func TestMaxBadFrames_CountsSchemaRejectionNotUnknownAction(t *testing.T) {
	defer goleak.VerifyNone(t)
	rec := &badFrameRecorder{MetricsHook: NoopMetrics}
	cfg := DefaultConfig()
	cfg.Metrics = rec
	cfg.MaxBadFrames = 2
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(_ ocppj.Version, action, _ string, _ []byte) error {
		return errors.New("bad payload")
	}
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) { return []byte(`{}`), nil })
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	// Unknown actions are not bad frames, however many arrive.
	for _, id := range []string{"u1", "u2", "u3"} {
		f.Inject([]byte(`[2,"` + id + `","NoSuchAction",{}]`))
		<-f.Sent()
	}
	require.NoError(t, context.Cause(c.Context()))

	f.Inject([]byte(`[2,"s1","Heartbeat",{}]`))
	<-f.Sent()
	f.Inject([]byte(`[2,"s2","Heartbeat",{}]`))
	select {
	case <-c.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("connection not closed after two schema rejections")
	}
	require.Equal(t, []string{"schema", "schema"}, rec.got())
}
