package transport

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrFakeClosed is returned by a closed FakeWS.
var ErrFakeClosed = errors.New("transport: fake ws closed")

// FakeWS is an in-memory WS for tests. Inbound messages are injected via Inject;
// outbound writes are observable via Sent.
type FakeWS struct {
	sub       string
	inbound   chan []byte
	sent      chan []byte
	closeOnce sync.Once
	done      chan struct{}
	pingMu    sync.Mutex
	pingFunc  func(context.Context) error
	pingCount atomic.Int64
}

// NewFakeWS creates a fake with the given negotiated subprotocol.
func NewFakeWS(subprotocol string) *FakeWS {
	return &FakeWS{
		sub:     subprotocol,
		inbound: make(chan []byte, 64),
		sent:    make(chan []byte, 64),
		done:    make(chan struct{}),
	}
}

// Inject queues an inbound message to be returned by Read.
func (f *FakeWS) Inject(msg []byte) { f.inbound <- msg }

// Sent exposes outbound messages written via Write.
func (f *FakeWS) Sent() <-chan []byte { return f.sent }

// SetPingFunc configures Ping behavior for tests. nil restores the default.
func (f *FakeWS) SetPingFunc(fn func(context.Context) error) {
	f.pingMu.Lock()
	f.pingFunc = fn
	f.pingMu.Unlock()
}

// PingCount returns how many times Ping has been called.
func (f *FakeWS) PingCount() int64 { return f.pingCount.Load() }

func (f *FakeWS) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		return nil, ErrFakeClosed
	case msg := <-f.inbound:
		return msg, nil
	}
}

func (f *FakeWS) Write(ctx context.Context, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.done:
		return ErrFakeClosed
	case f.sent <- data:
		return nil
	}
}

func (f *FakeWS) Ping(ctx context.Context) error {
	f.pingCount.Add(1)
	f.pingMu.Lock()
	fn := f.pingFunc
	f.pingMu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.done:
		return ErrFakeClosed
	default:
		return nil
	}
}

func (f *FakeWS) Close(StatusCode, string) error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}

func (f *FakeWS) Subprotocol() string { return f.sub }
