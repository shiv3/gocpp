// Package tap exports frame and session events to an external sink for audit,
// analytics and replay, without ever slowing the connections down.
//
// An Exporter plugs into csms.WithFrameObserver and csms.WithSessionObserver.
// The hot path (a FrameObserver call, which runs while the connection's write
// lock or reader goroutine is held) only samples, copies a bounded prefix of the
// frame and does a non-blocking send into a bounded buffer; when the buffer is
// full the event is dropped and counted, never waited for. A background
// goroutine batches events and hands them to a Sink, which may block, retry or
// fail without affecting traffic.
//
// Delivery is best effort and at most once per exporter; every event carries
// (Source, Seq), where Seq counts every event the exporter produced, dropped
// ones included, so a consumer sees losses as gaps and can deduplicate when the
// transport redelivers (JetStream, Kafka). Session events also carry the
// registry epoch, which orders the connections of one charge point.
package tap

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/core/observability"
)

// Kind is the type of an Event.
type Kind string

const (
	KindFrame   Kind = "frame"
	KindSession Kind = "session"
)

// Event is one exported record. Fields not relevant to the kind are omitted from
// the JSON form.
type Event struct {
	Kind   Kind      `json:"kind"`
	Source string    `json:"source,omitempty"`
	Seq    uint64    `json:"seq"`
	Time   time.Time `json:"time"`
	CPID   string    `json:"cp_id"`
	// Version is the negotiated OCPP version ("1.6", "2.0.1", "2.1").
	Version string `json:"version,omitempty"`

	// Frame events.
	Direction  string `json:"direction,omitempty"` // "in" | "out"
	MsgType    int    `json:"msg_type,omitempty"`
	MsgID      string `json:"msg_id,omitempty"`
	Action     string `json:"action,omitempty"`
	Frame      string `json:"frame,omitempty"`     // raw text, cut at MaxFrame bytes
	FrameLen   int    `json:"frame_len,omitempty"` // full length when cut
	WriteError string `json:"write_error,omitempty"`

	// Session events.
	Session    string            `json:"session,omitempty"` // "opened" | "closed"
	RemoteAddr string            `json:"remote_addr,omitempty"`
	Epoch      uint64            `json:"epoch,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
	Cause      string            `json:"cause,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
}

// Sink receives batches of events on the exporter's background goroutine. A
// returned error counts the batch as failed (events are not retried by the
// exporter; a sink that wants retries does them itself, bounded). The slice is
// reused after the call returns.
type Sink interface {
	WriteBatch(ctx context.Context, events []Event) error
	// Close flushes and releases the sink. It is called once, after the last batch.
	Close() error
}

// Stats are the exporter's counters since creation.
type Stats struct {
	// Exported events were handed to the sink successfully.
	Exported uint64
	// Dropped events did not fit the buffer (or arrived after Close).
	Dropped uint64
	// Sampled counts frames skipped by the filter or the sampling rate.
	Sampled uint64
	// Failed events were in a batch the sink rejected.
	Failed uint64
	// Queued is the current buffer depth.
	Queued int
}

// Option configures an Exporter.
type Option func(*Exporter)

// WithSource names the producer (a gateway id, a pod name); it is copied into
// every event so a consumer of several gateways can tell them apart.
func WithSource(s string) Option { return func(e *Exporter) { e.source = s } }

// WithBuffer sets how many frame events may wait for the sink (default 4096). Frames
// beyond it are dropped and counted. Session events have extra room of their own
// (16384), so a frame flood cannot push them out.
func WithBuffer(n int) Option { return func(e *Exporter) { e.bufSize = n } }

// WithBatch sets the largest batch handed to the sink (default 256) and the
// longest an event waits for the batch to fill (default 1 s).
func WithBatch(size int, flushEvery time.Duration) Option {
	return func(e *Exporter) { e.batch, e.flushEvery = size, flushEvery }
}

// WithMaxFrame bounds how much of a frame is copied into an event (default
// 2048 bytes; the event records the full length when it cut). 0 keeps the default;
// a negative value exports no frame body, only the metadata.
func WithMaxFrame(n int) Option { return func(e *Exporter) { e.maxFrame = n } }

// WithFrameFilter keeps only the frames for which keep returns true (for
// example, drop Heartbeat or only record one direction). It runs on the hot path
// before anything is copied, so it must be cheap.
func WithFrameFilter(keep func(observability.FrameEvent) bool) Option {
	return func(e *Exporter) { e.filter = keep }
}

// WithFrameSampling exports one frame out of every n that pass the filter (n <=
// 1 exports all). The choice is a counter, not random, so the rate is exact and
// cheap; use the filter to sample by charger instead.
func WithFrameSampling(n int) Option { return func(e *Exporter) { e.every = uint64(max(n, 1)) } }

// WithSessionFilter keeps only the session events for which keep returns true.
func WithSessionFilter(keep func(observability.SessionEvent) bool) Option {
	return func(e *Exporter) { e.sessFilter = keep }
}

// Exporter buffers events and ships them to a Sink. Create it with New; Close it
// to flush.
type Exporter struct {
	sink       Sink
	source     string
	bufSize    int
	batch      int
	flushEvery time.Duration
	maxFrame   int
	every      uint64
	filter     func(observability.FrameEvent) bool
	sessFilter func(observability.SessionEvent) bool

	ch     chan Event
	seq    atomic.Uint64
	ticks  atomic.Uint64
	closed atomic.Bool
	// mu orders Close against senders: a send holds it shared, Close takes it
	// exclusively to close the channel.
	mu sync.RWMutex
	wg sync.WaitGroup

	exported, dropped, sampled, failed atomic.Uint64
}

// sessionReserve is the room in the queue that only session events may use. They
// are rare next to frames (a few per connection), so it absorbs a reconnect storm
// of thousands of chargers while the sink is slow and a frame flood has filled
// the rest.
const sessionReserve = 16384

// New starts an exporter that writes to sink.
func New(sink Sink, opts ...Option) *Exporter {
	e := &Exporter{sink: sink, bufSize: 4096, batch: 256, flushEvery: time.Second, maxFrame: 2048, every: 1}
	for _, o := range opts {
		o(e)
	}
	e.bufSize = max(e.bufSize, 1)
	e.batch = max(e.batch, 1)
	if e.flushEvery <= 0 {
		e.flushEvery = time.Second
	}
	if e.maxFrame == 0 {
		e.maxFrame = 2048
	}
	// One channel keeps the events in order; its last sessionReserve slots are
	// only for session events (see enqueueFrame).
	e.ch = make(chan Event, e.bufSize+sessionReserve)
	e.wg.Add(1)
	go e.run()
	return e
}

// FrameObserver is the observer to pass to csms.WithFrameObserver.
func (e *Exporter) FrameObserver() observability.FrameObserver {
	return func(_ context.Context, ev observability.FrameEvent) {
		if e.filter != nil && !e.filter(ev) {
			e.sampled.Add(1)
			return
		}
		if e.every > 1 && e.ticks.Add(1)%e.every != 1 {
			e.sampled.Add(1)
			return
		}
		out := Event{
			Kind: KindFrame, Source: e.source, Time: ev.Time, CPID: ev.CPID, Version: string(ev.Version),
			Direction: string(ev.Direction), MsgType: int(ev.Type), MsgID: ev.MsgID, Action: ev.Action,
		}
		if e.maxFrame > 0 {
			raw := ev.Raw
			if len(raw) > e.maxFrame {
				raw = raw[:e.maxFrame]
				out.FrameLen = len(ev.Raw)
			}
			out.Frame = string(raw) // a copy: Raw is shared with the connection
		}
		if ev.Err != nil {
			out.WriteError = ev.Err.Error()
		}
		e.enqueueFrame(out)
	}
}

// SessionObserver is the observer to pass to csms.WithSessionObserver.
func (e *Exporter) SessionObserver() observability.SessionObserver {
	return func(_ context.Context, ev observability.SessionEvent) {
		if e.sessFilter != nil && !e.sessFilter(ev) {
			return
		}
		e.enqueue(Event{
			Kind: KindSession, Source: e.source, Time: ev.Time, CPID: ev.CPID, Version: string(ev.Version),
			Session: string(ev.Kind), RemoteAddr: ev.RemoteAddr, Epoch: ev.Epoch, Attrs: ev.Attrs,
			Cause: ev.Cause, DurationMs: ev.Duration.Milliseconds(),
		})
	}
}

// enqueueFrame drops the event when the frame share of the queue is full, so a
// frame flood leaves the reserved room to session events.
func (e *Exporter) enqueueFrame(ev Event) {
	if len(e.ch) >= e.bufSize {
		e.seq.Add(1) // keep the gap visible to the consumer, as for any dropped event
		e.dropped.Add(1)
		return
	}
	e.enqueue(ev)
}

func (e *Exporter) enqueue(ev Event) {
	ev.Seq = e.seq.Add(1)
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed.Load() {
		e.dropped.Add(1)
		return
	}
	select {
	case e.ch <- ev:
	default:
		e.dropped.Add(1)
	}
}

// Stats returns the counters.
func (e *Exporter) Stats() Stats {
	return Stats{
		Exported: e.exported.Load(), Dropped: e.dropped.Load(), Sampled: e.sampled.Load(),
		Failed: e.failed.Load(), Queued: len(e.ch),
	}
}

func (e *Exporter) run() {
	defer e.wg.Done()
	buf := make([]Event, 0, e.batch)
	t := time.NewTicker(e.flushEvery)
	defer t.Stop()
	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := e.sink.WriteBatch(context.Background(), buf); err != nil {
			e.failed.Add(uint64(len(buf)))
		} else {
			e.exported.Add(uint64(len(buf)))
		}
		clear(buf)
		buf = buf[:0]
	}
	for {
		select {
		case ev, ok := <-e.ch:
			if !ok {
				flush()
				return
			}
			buf = append(buf, ev)
			if len(buf) >= e.batch {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

// Close stops accepting events, flushes what is buffered to the sink (waiting
// until ctx ends) and closes the sink. Events offered afterwards are dropped.
func (e *Exporter) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed.Swap(true) {
		e.mu.Unlock()
		return nil
	}
	close(e.ch)
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return e.sink.Close()
}
