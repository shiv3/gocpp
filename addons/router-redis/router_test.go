package routerredis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shiv3/gocpp/core/storage"
)

func TestCallLocalReturnsErrNotLocal(t *testing.T) {
	r := newRouter(newFakeBroker(), "A", newFakeRegistry(map[string]string{}))

	_, err := r.CallLocal(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, storage.ErrNotLocal) {
		t.Fatalf("CallLocal error = %v, want ErrNotLocal", err)
	}
}

func TestCallRemoteForwardsThroughBroker(t *testing.T) {
	br := newFakeBroker()
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	instA := newRouter(br, "A", reg, WithRequestTimeout(time.Second))
	instB := newRouter(br, "B", reg, WithRequestTimeout(time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- instB.ServeRemote(ctx, func(_ context.Context, cpID, action string, req []byte) ([]byte, error) {
			if cpID != "CP_1" {
				t.Errorf("cpID = %q, want CP_1", cpID)
			}
			if action != "Reset" {
				t.Errorf("action = %q, want Reset", action)
			}
			if string(req) != `{"type":"Hard"}` {
				t.Errorf("request = %s, want hard reset payload", req)
			}
			return []byte(`{"status":"Accepted"}`), nil
		})
	}()
	br.waitForSubscribers(t, instB.requestChannel("B"), 1)

	resp, err := instA.CallRemote(ctx, "CP_1", "Reset", []byte(`{"type":"Hard"}`))
	if err != nil {
		t.Fatalf("CallRemote returned error: %v", err)
	}
	if string(resp) != `{"status":"Accepted"}` {
		t.Fatalf("response = %s, want accepted", resp)
	}

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("ServeRemote error = %v, want context.Canceled", err)
	}
}

func TestCallRemoteReturnsErrNotLocalWhenGlobalBindingMissing(t *testing.T) {
	br := newFakeBroker()
	r := newRouter(br, "A", newFakeRegistry(map[string]string{}))

	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, storage.ErrNotLocal) {
		t.Fatalf("CallRemote error = %v, want ErrNotLocal", err)
	}
	if got := br.publishCount(); got != 0 {
		t.Fatalf("publish count = %d, want 0", got)
	}
}

func TestCallRemoteTimesOutWaitingForReply(t *testing.T) {
	br := newFakeBroker()
	r := newRouter(br, "A", newFakeRegistry(map[string]string{"CP_1": "B"}), WithRequestTimeout(10*time.Millisecond))

	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallRemote error = %v, want context deadline exceeded", err)
	}
}

func TestCallRemoteIgnoresMalformedReplyThenAcceptsValid(t *testing.T) {
	br := newFakeBroker()
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	r := newRouter(br, "A", reg, WithRequestTimeout(time.Second), withIDGenerator(func() string { return "req-1" }))

	br.onPublish = func(ctx context.Context, channel, payload string) {
		if channel != r.requestChannel("B") {
			return
		}
		var req requestEnvelope
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			t.Errorf("request envelope did not decode: %v", err)
			return
		}
		_ = br.Publish(ctx, req.ReplyChannel, `{not-json`)
		_ = br.Publish(ctx, req.ReplyChannel, `{"version":1,"id":"other","payload":"e30="}`)
		_ = br.Publish(ctx, req.ReplyChannel, `{"version":1,"id":"req-1","payload":"eyJvayI6dHJ1ZX0="}`)
	}

	resp, err := r.CallRemote(context.Background(), "CP_1", "StatusNotification", []byte(`{}`))
	if err != nil {
		t.Fatalf("CallRemote returned error: %v", err)
	}
	if string(resp) != `{"ok":true}` {
		t.Fatalf("response = %s, want valid reply payload", resp)
	}
}

func TestRemoteHandlerErrorIsReturnedToCaller(t *testing.T) {
	br := newFakeBroker()
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	instA := newRouter(br, "A", reg, WithRequestTimeout(time.Second))
	instB := newRouter(br, "B", reg, WithRequestTimeout(time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = instB.ServeRemote(ctx, func(context.Context, string, string, []byte) ([]byte, error) {
			return nil, errors.New("boom")
		})
	}()
	br.waitForSubscribers(t, instB.requestChannel("B"), 1)

	_, err := instA.CallRemote(ctx, "CP_1", "Reset", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("CallRemote error = %v, want remote handler error containing boom", err)
	}
}

func TestServeRemoteIgnoresMalformedRequests(t *testing.T) {
	br := newFakeBroker()
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	instA := newRouter(br, "A", reg, WithRequestTimeout(time.Second))
	instB := newRouter(br, "B", reg, WithRequestTimeout(time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handled := make(chan struct{}, 1)
	go func() {
		_ = instB.ServeRemote(ctx, func(context.Context, string, string, []byte) ([]byte, error) {
			handled <- struct{}{}
			return []byte(`{"ok":true}`), nil
		})
	}()
	br.waitForSubscribers(t, instB.requestChannel("B"), 1)

	if err := br.Publish(ctx, instB.requestChannel("B"), `{not-json`); err != nil {
		t.Fatalf("publish malformed request: %v", err)
	}

	resp, err := instA.CallRemote(ctx, "CP_1", "Reset", []byte(`{}`))
	if err != nil {
		t.Fatalf("CallRemote returned error after malformed message: %v", err)
	}
	if string(resp) != `{"ok":true}` {
		t.Fatalf("response = %s, want ok", resp)
	}
	select {
	case <-handled:
	default:
		t.Fatal("handler was not called for valid request")
	}
}

type fakeRegistry struct {
	mu        sync.Mutex
	locations map[string]string
	err       error
}

func newFakeRegistry(locations map[string]string) *fakeRegistry {
	return &fakeRegistry{locations: locations}
}

func (r *fakeRegistry) PutLocal(context.Context, string, storage.LiveConn) error {
	return nil
}

func (r *fakeRegistry) GetLocal(string) (storage.LiveConn, bool) {
	return nil, false
}

func (r *fakeRegistry) DeleteLocal(context.Context, string) error {
	return nil
}

func (r *fakeRegistry) RangeLocal(func(string, storage.LiveConn) bool) {}

func (r *fakeRegistry) PutGlobal(_ context.Context, cpID, instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.locations[cpID] = instanceID
	return nil
}

func (r *fakeRegistry) LookupGlobal(_ context.Context, cpID string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return "", false, r.err
	}
	instanceID, ok := r.locations[cpID]
	return instanceID, ok, nil
}

func (r *fakeRegistry) DeleteGlobal(_ context.Context, cpID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.locations, cpID)
	return nil
}

type publishedMessage struct {
	channel string
	payload string
}

type fakeBroker struct {
	mu        sync.Mutex
	subs      map[string][]*fakeSubscription
	published []publishedMessage
	onPublish func(ctx context.Context, channel, payload string)
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{subs: make(map[string][]*fakeSubscription)}
}

func (b *fakeBroker) Publish(ctx context.Context, channel, payload string) error {
	b.mu.Lock()
	b.published = append(b.published, publishedMessage{channel: channel, payload: payload})
	subs := append([]*fakeSubscription(nil), b.subs[channel]...)
	hook := b.onPublish
	b.mu.Unlock()

	for _, sub := range subs {
		if err := sub.send(ctx, &redis.Message{Channel: channel, Payload: payload}); err != nil {
			return err
		}
	}
	if hook != nil {
		hook(ctx, channel, payload)
	}
	return nil
}

func (b *fakeBroker) Subscribe(_ context.Context, channel string) (subscription, error) {
	sub := &fakeSubscription{
		broker:  b,
		channel: channel,
		ch:      make(chan *redis.Message, 16),
	}
	b.mu.Lock()
	b.subs[channel] = append(b.subs[channel], sub)
	b.mu.Unlock()
	return sub, nil
}

func (b *fakeBroker) publishCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.published)
}

func (b *fakeBroker) waitForSubscribers(t *testing.T, channel string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		got := len(b.subs[channel])
		b.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscriber count for %q did not reach %d", channel, want)
}

func (b *fakeBroker) remove(sub *fakeSubscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[sub.channel]
	for i, candidate := range subs {
		if candidate == sub {
			b.subs[sub.channel] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

type fakeSubscription struct {
	broker  *fakeBroker
	channel string
	ch      chan *redis.Message
	mu      sync.Mutex
	closed  bool
}

func (s *fakeSubscription) Channel() <-chan *redis.Message {
	return s.ch
}

func (s *fakeSubscription) Close() error {
	s.broker.remove(s)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeSubscription) send(ctx context.Context, msg *redis.Message) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	ch := s.ch
	s.mu.Unlock()

	select {
	case ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
