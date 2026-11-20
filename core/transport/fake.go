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
	closeMu   sync.Mutex
	closed    bool
	closeCode StatusCode
	closeText string
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

// Close records the code and reason of the first call and unblocks Read/Write.
func (f *FakeWS) Close(code StatusCode, reason string) error {
	f.closeOnce.Do(func() {
		f.closeMu.Lock()
		f.closed, f.closeCode, f.closeText = true, code, reason
		f.closeMu.Unlock()
		close(f.done)
	})
	return nil
}

// Abort closes the fake at once, recording an abnormal closure unless Close
// already ran. It makes FakeWS a transport.Aborter.
func (f *FakeWS) Abort() error { return f.Close(StatusAbnormalClosure, "aborted") }

// CloseCode returns the code and reason of the first Close call; ok is false if
// Close has not been called yet.
func (f *FakeWS) CloseCode() (code StatusCode, reason string, ok bool) {
	f.closeMu.Lock()
	defer f.closeMu.Unlock()
	return f.closeCode, f.closeText, f.closed
}

func (f *FakeWS) Subprotocol() string { return f.sub }
