package tap_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/observability/tap"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type memSink struct {
	mu     sync.Mutex
	events []tap.Event
	closed bool
	gate   chan struct{} // when set, WriteBatch waits for it
	err    error
}

func (s *memSink) WriteBatch(_ context.Context, ev []tap.Event) error {
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, ev...)
	return nil
}
func (s *memSink) Close() error { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }
func (s *memSink) all() []tap.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tap.Event(nil), s.events...)
}

func frame(action, raw string) observability.FrameEvent {
	return observability.FrameEvent{CPID: "CP1", Version: ocppj.V16, Direction: observability.FrameIn, Raw: []byte(raw),
		Type: ocppj.Call, MsgID: "m", Action: action, Time: time.Now()}
}

func TestExporter_ExportsFramesAndSessions(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{}
	e := tap.New(sink, tap.WithSource("gw-1"), tap.WithBatch(2, time.Hour))
	raw := []byte(`[2,"m","Heartbeat",{}]`)
	e.FrameObserver()(context.Background(), frame("Heartbeat", string(raw)))
	// The frame is copied: reusing the connection's buffer must not change the event.
	raw[2] = 'X'
	e.SessionObserver()(context.Background(), observability.SessionEvent{
		Kind: observability.SessionClosed, CPID: "CP1", Version: ocppj.V16, Epoch: 7, Cause: "boom",
		Duration: 1500 * time.Millisecond, Attrs: map[string]string{"tenant": "a"}, Time: time.Now(),
	})
	require.NoError(t, e.Close(context.Background()))

	ev := sink.all()
	require.Len(t, ev, 2)
	require.Equal(t, tap.KindFrame, ev[0].Kind)
	require.Equal(t, `[2,"m","Heartbeat",{}]`, ev[0].Frame)
	require.Equal(t, "gw-1", ev[0].Source)
	require.Equal(t, "Heartbeat", ev[0].Action)
	require.EqualValues(t, 1, ev[0].Seq)
	require.Equal(t, tap.KindSession, ev[1].Kind)
	require.EqualValues(t, 2, ev[1].Seq)
	require.Equal(t, "closed", ev[1].Session)
	require.EqualValues(t, 7, ev[1].Epoch)
	require.EqualValues(t, 1500, ev[1].DurationMs)
	require.Equal(t, "a", ev[1].Attrs["tenant"])
	require.True(t, sink.closed)
	require.Equal(t, tap.Stats{Exported: 2}, e.Stats())
}

func TestExporter_TruncatesAndCanOmitFrames(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{}
	e := tap.New(sink, tap.WithMaxFrame(10))
	e.FrameObserver()(context.Background(), frame("X", strings.Repeat("a", 100)))
	meta := &memSink{}
	e2 := tap.New(meta, tap.WithMaxFrame(-1))
	e2.FrameObserver()(context.Background(), frame("X", "secret"))
	require.NoError(t, e.Close(context.Background()))
	require.NoError(t, e2.Close(context.Background()))
	require.Equal(t, strings.Repeat("a", 10), sink.all()[0].Frame)
	require.Equal(t, 100, sink.all()[0].FrameLen)
	require.Empty(t, meta.all()[0].Frame)
	require.Equal(t, "X", meta.all()[0].Action)
}

// A sink that never returns must not slow the observer: the buffer fills, the
// excess is dropped and counted, and every call returns immediately.
func TestExporter_NeverBlocksTheHotPath(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{gate: make(chan struct{})}
	e := tap.New(sink, tap.WithBuffer(8), tap.WithBatch(1, time.Hour))
	obs := e.FrameObserver()
	start := time.Now()
	for i := 0; i < 10_000; i++ {
		obs(context.Background(), frame("Heartbeat", "[]"))
	}
	require.Less(t, time.Since(start), 2*time.Second, "10k observations did not wait for the stuck sink")
	st := e.Stats()
	require.Greater(t, st.Dropped, uint64(9_000))
	// One batch is stuck in the sink and the buffer holds the rest.
	require.LessOrEqual(t, st.Queued, 8)

	close(sink.gate)
	require.NoError(t, e.Close(context.Background()))
	st = e.Stats()
	require.EqualValues(t, 10_000, st.Exported+st.Dropped, "every event is exported or counted as dropped")
	// Seq counts dropped events too, so the consumer sees the gaps.
	ev := sink.all()
	require.Greater(t, ev[len(ev)-1].Seq, uint64(len(ev)))
}

func TestExporter_SamplingAndFilter(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{}
	e := tap.New(sink, tap.WithFrameSampling(10),
		tap.WithFrameFilter(func(ev observability.FrameEvent) bool { return ev.Action != "Heartbeat" }))
	obs := e.FrameObserver()
	for i := 0; i < 100; i++ {
		obs(context.Background(), frame("MeterValues", "[]"))
		obs(context.Background(), frame("Heartbeat", "[]"))
	}
	e.SessionObserver()(context.Background(), observability.SessionEvent{Kind: observability.SessionOpened, CPID: "CP1"})
	require.NoError(t, e.Close(context.Background()))
	var frames, sessions int
	for _, ev := range sink.all() {
		switch ev.Kind {
		case tap.KindFrame:
			frames++
			require.Equal(t, "MeterValues", ev.Action)
		case tap.KindSession:
			sessions++
		}
	}
	require.Equal(t, 10, frames, "1 in 10 of the 100 frames that passed the filter")
	require.Equal(t, 1, sessions, "sessions are not sampled")
	require.EqualValues(t, 190, e.Stats().Sampled)
}

func TestExporter_SinkErrorsAreCountedNotFatal(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{err: errors.New("down")}
	e := tap.New(sink, tap.WithBatch(1, time.Hour))
	for i := 0; i < 5; i++ {
		e.FrameObserver()(context.Background(), frame("X", "[]"))
	}
	require.NoError(t, e.Close(context.Background()))
	require.EqualValues(t, 5, e.Stats().Failed)
	require.Zero(t, e.Stats().Exported)
}

func TestExporter_FlushesOnInterval(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{}
	e := tap.New(sink, tap.WithBatch(1000, 20*time.Millisecond))
	e.FrameObserver()(context.Background(), frame("X", "[]"))
	require.Eventually(t, func() bool { return len(sink.all()) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, e.Close(context.Background()))
}

func TestExporter_AfterCloseDropsAndConcurrentUseIsSafe(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &memSink{}
	e := tap.New(sink, tap.WithBuffer(64))
	var wg sync.WaitGroup
	var calls atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				e.FrameObserver()(context.Background(), frame("X", "[]"))
				calls.Add(1)
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, e.Close(context.Background())) // races with the producers on purpose
	wg.Wait()
	require.NoError(t, e.Close(context.Background()), "Close is idempotent")
	st := e.Stats()
	require.EqualValues(t, calls.Load(), st.Exported+st.Dropped)
}

func TestJSONL_OneObjectPerLine(t *testing.T) {
	defer goleak.VerifyNone(t)
	var buf bytes.Buffer
	e := tap.New(tap.JSONL(&buf), tap.WithSource("s"))
	e.FrameObserver()(context.Background(), frame("Heartbeat", `[2,"m","Heartbeat",{}]`))
	e.SessionObserver()(context.Background(), observability.SessionEvent{Kind: observability.SessionOpened, CPID: "CP1", RemoteAddr: "1.2.3.4:5"})
	require.NoError(t, e.Close(context.Background()))
	sc := bufio.NewScanner(&buf)
	var got []tap.Event
	for sc.Scan() {
		var ev tap.Event
		require.NoError(t, json.Unmarshal(sc.Bytes(), &ev))
		got = append(got, ev)
	}
	require.Len(t, got, 2)
	require.Equal(t, `[2,"m","Heartbeat",{}]`, got[0].Frame)
	require.Equal(t, "opened", got[1].Session)
	require.Equal(t, "1.2.3.4:5", got[1].RemoteAddr)
}

// BenchmarkFrameObserver measures what a tapped frame costs the connection: a
// bounded copy and a non-blocking channel send, with a sink that keeps up.
func BenchmarkFrameObserver(b *testing.B) {
	e := tap.New(&memSink{err: errors.New("discard")}, tap.WithBuffer(1<<16), tap.WithBatch(1024, time.Hour))
	obs := e.FrameObserver()
	ev := frame("MeterValues", `[2,"m","MeterValues",{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":"1"}]}]}]`)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		obs(ctx, ev)
	}
	b.StopTimer()
	_ = e.Close(ctx)
}
