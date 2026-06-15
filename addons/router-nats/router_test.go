package routernats

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/shiv3/gocpp/core/storage"
)

func TestNewReturnsMessageRouter(t *testing.T) {
	var _ storage.MessageRouter = New(nil, "A", nil)
}

func TestCallLocalReturnsErrNotLocal(t *testing.T) {
	r := newTestRouter(fakeTransport{}, "A", newFakeRegistry(map[string]string{}))

	_, err := r.CallLocal(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, storage.ErrNotLocal) {
		t.Fatalf("CallLocal error = %v, want %v", err, storage.ErrNotLocal)
	}
}

func TestCallRemoteForwardsRequest(t *testing.T) {
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	tr := fakeTransport{
		requestFunc: func(_ context.Context, subject string, data []byte) ([]byte, error) {
			if subject != "gocpp.route.B" {
				t.Fatalf("subject = %q, want gocpp.route.B", subject)
			}

			var req requestEnvelope
			if err := json.Unmarshal(data, &req); err != nil {
				t.Fatalf("decode request envelope: %v", err)
			}
			if req.CPID != "CP_1" || req.Action != "Reset" {
				t.Fatalf("request envelope = %#v", req)
			}
			if string(req.Payload) != `{"type":"Immediate"}` {
				t.Fatalf("payload = %s", req.Payload)
			}

			return mustMarshal(responseEnvelope{Payload: json.RawMessage(`{"status":"Accepted"}`)}), nil
		},
	}
	r := newTestRouter(tr, "A", reg)

	resp, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{"type":"Immediate"}`))
	if err != nil {
		t.Fatalf("CallRemote error = %v", err)
	}
	if string(resp) != `{"status":"Accepted"}` {
		t.Fatalf("response = %s", resp)
	}
}

func TestCallRemoteUsesCustomSubjectPrefix(t *testing.T) {
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	tr := fakeTransport{
		requestFunc: func(_ context.Context, subject string, _ []byte) ([]byte, error) {
			if subject != "custom.routes.B" {
				t.Fatalf("subject = %q, want custom.routes.B", subject)
			}
			return mustMarshal(responseEnvelope{Payload: json.RawMessage(`{}`)}), nil
		},
	}
	cfg := defaultConfig()
	WithSubjectPrefix("custom.routes.")(&cfg)
	r := newRouter(tr, "A", reg, cfg)

	if _, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`)); err != nil {
		t.Fatalf("CallRemote error = %v", err)
	}
}

func TestCallRemoteReturnsErrNotLocalWhenRegistryMisses(t *testing.T) {
	r := newTestRouter(fakeTransport{}, "A", newFakeRegistry(map[string]string{}))

	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, storage.ErrNotLocal) {
		t.Fatalf("CallRemote error = %v, want %v", err, storage.ErrNotLocal)
	}
}

func TestCallRemoteMapsNoRespondersToErrNotLocal(t *testing.T) {
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	tr := fakeTransport{
		requestFunc: func(context.Context, string, []byte) ([]byte, error) {
			return nil, nats.ErrNoResponders
		},
	}
	r := newTestRouter(tr, "A", reg)

	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, storage.ErrNotLocal) {
		t.Fatalf("CallRemote error = %v, want %v", err, storage.ErrNotLocal)
	}
}

func TestCallRemotePreservesTimeoutError(t *testing.T) {
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	tr := fakeTransport{
		requestFunc: func(context.Context, string, []byte) ([]byte, error) {
			return nil, nats.ErrTimeout
		},
	}
	r := newTestRouter(tr, "A", reg)

	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, nats.ErrTimeout) {
		t.Fatalf("CallRemote error = %v, want %v", err, nats.ErrTimeout)
	}
}

func TestCallRemotePreservesRemoteErrNotLocal(t *testing.T) {
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	tr := fakeTransport{
		requestFunc: func(context.Context, string, []byte) ([]byte, error) {
			return mustMarshal(responseEnvelope{Error: storage.ErrNotLocal.Error()}), nil
		},
	}
	r := newTestRouter(tr, "A", reg)

	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	if !errors.Is(err, storage.ErrNotLocal) {
		t.Fatalf("CallRemote error = %v, want %v", err, storage.ErrNotLocal)
	}
}

func TestCallRemoteRejectsInvalidJSONPayload(t *testing.T) {
	called := false
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	tr := fakeTransport{
		requestFunc: func(context.Context, string, []byte) ([]byte, error) {
			called = true
			return nil, nil
		},
	}
	r := newTestRouter(tr, "A", reg)

	if _, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{`)); err == nil {
		t.Fatal("CallRemote error = nil, want invalid JSON error")
	}
	if called {
		t.Fatal("transport request was called for invalid JSON payload")
	}
}

func TestServeRemoteHandlesMessagesAndDrainsOnCancel(t *testing.T) {
	var gotSubject string
	var gotHandler func(inboundMessage)
	sub := &fakeSubscription{}
	subscribed := make(chan struct{})
	tr := fakeTransport{
		subscribeFunc: func(subject string, handler func(inboundMessage)) (subscription, error) {
			gotSubject = subject
			gotHandler = handler
			close(subscribed)
			return sub, nil
		},
		flushFunc: func(context.Context) error { return nil },
	}
	r := newTestRouter(tr, "B", newFakeRegistry(nil))

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.ServeRemote(ctx, func(_ context.Context, cpID, action string, req []byte) ([]byte, error) {
			if cpID != "CP_1" || action != "Reset" {
				t.Errorf("handler args = %q, %q", cpID, action)
			}
			if string(req) != `{"type":"Immediate"}` {
				t.Errorf("handler payload = %s", req)
			}
			return []byte(`{"status":"Accepted"}`), nil
		})
	}()
	<-subscribed

	if gotSubject != "gocpp.route.B" {
		t.Fatalf("subject = %q, want gocpp.route.B", gotSubject)
	}
	respCh := make(chan responseEnvelope, 1)
	gotHandler(inboundMessage{
		data: mustMarshal(requestEnvelope{
			CPID:    "CP_1",
			Action:  "Reset",
			Payload: json.RawMessage(`{"type":"Immediate"}`),
		}),
		respond: func(env responseEnvelope) { respCh <- env },
	})

	resp := <-respCh
	if resp.Error != "" {
		t.Fatalf("response error = %q", resp.Error)
	}
	if string(resp.Payload) != `{"status":"Accepted"}` {
		t.Fatalf("response payload = %s", resp.Payload)
	}

	cancel()
	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ServeRemote error = %v, want %v", err, context.Canceled)
	}
	if !sub.drained {
		t.Fatal("subscription was not drained")
	}
	if sub.unsubscribed {
		t.Fatal("subscription was unexpectedly unsubscribed after successful drain")
	}
}

func TestServeRemoteRespondsWithDecodeError(t *testing.T) {
	var gotHandler func(inboundMessage)
	subscribed := make(chan struct{})
	tr := fakeTransport{
		subscribeFunc: func(_ string, handler func(inboundMessage)) (subscription, error) {
			gotHandler = handler
			close(subscribed)
			return &fakeSubscription{}, nil
		},
		flushFunc: func(context.Context) error { return nil },
	}
	r := newTestRouter(tr, "B", newFakeRegistry(nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	called := make(chan struct{}, 1)
	go func() {
		errCh <- r.ServeRemote(ctx, func(context.Context, string, string, []byte) ([]byte, error) {
			called <- struct{}{}
			return nil, nil
		})
	}()
	<-subscribed

	respCh := make(chan responseEnvelope, 1)
	gotHandler(inboundMessage{
		data:    []byte(`{`),
		respond: func(env responseEnvelope) { respCh <- env },
	})
	resp := <-respCh
	if resp.Error == "" {
		t.Fatal("response error = empty, want decode error")
	}
	select {
	case <-called:
		t.Fatal("handler was called for an invalid request envelope")
	default:
	}

	cancel()
	<-errCh
}

func newTestRouter(tr fakeTransport, instanceID string, reg storage.ConnectionRegistry) storage.MessageRouter {
	return newRouter(tr, instanceID, reg, defaultConfig())
}

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

type fakeTransport struct {
	requestFunc   func(context.Context, string, []byte) ([]byte, error)
	subscribeFunc func(string, func(inboundMessage)) (subscription, error)
	flushFunc     func(context.Context) error
}

func (f fakeTransport) request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	if f.requestFunc == nil {
		return nil, errors.New("unexpected request")
	}
	return f.requestFunc(ctx, subject, data)
}

func (f fakeTransport) subscribe(subject string, handler func(inboundMessage)) (subscription, error) {
	if f.subscribeFunc == nil {
		return nil, errors.New("unexpected subscribe")
	}
	return f.subscribeFunc(subject, handler)
}

func (f fakeTransport) flush(ctx context.Context) error {
	if f.flushFunc == nil {
		return nil
	}
	return f.flushFunc(ctx)
}

type fakeSubscription struct {
	drained      bool
	unsubscribed bool
	drainErr     error
	unsubErr     error
}

func (f *fakeSubscription) drain() error {
	f.drained = true
	return f.drainErr
}

func (f *fakeSubscription) unsubscribe() error {
	f.unsubscribed = true
	return f.unsubErr
}

type fakeRegistry struct {
	bindings map[string]string
	err      error
}

func newFakeRegistry(bindings map[string]string) *fakeRegistry {
	if bindings == nil {
		bindings = map[string]string{}
	}
	return &fakeRegistry{bindings: bindings}
}

func (r *fakeRegistry) PutLocal(context.Context, string, storage.LiveConn) error { return nil }
func (r *fakeRegistry) GetLocal(string) (storage.LiveConn, bool)                 { return nil, false }
func (r *fakeRegistry) DeleteLocal(context.Context, string) error                { return nil }
func (r *fakeRegistry) RangeLocal(func(string, storage.LiveConn) bool)           {}
func (r *fakeRegistry) PutGlobal(_ context.Context, cpID, instanceID string) error {
	r.bindings[cpID] = instanceID
	return nil
}
func (r *fakeRegistry) LookupGlobal(_ context.Context, cpID string) (string, bool, error) {
	if r.err != nil {
		return "", false, r.err
	}
	instanceID, ok := r.bindings[cpID]
	return instanceID, ok, nil
}
func (r *fakeRegistry) DeleteGlobal(_ context.Context, cpID string) error {
	delete(r.bindings, cpID)
	return nil
}
