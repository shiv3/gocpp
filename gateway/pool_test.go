package gateway_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type fakeUp struct {
	handle func(ctx context.Context, env gateway.Envelope) (gateway.Reply, error)
	calls  atomic.Int64
}

func (f *fakeUp) Connect(context.Context, gateway.ConnectRequest) (gateway.ConnectReply, error) {
	return gateway.ConnectReply{}, nil
}

func (f *fakeUp) Handle(ctx context.Context, env gateway.Envelope) (gateway.Reply, error) {
	f.calls.Add(1)
	return f.handle(ctx, env)
}
func (f *fakeUp) Notify(context.Context, gateway.Envelope) error { return nil }

func ok(context.Context, gateway.Envelope) (gateway.Reply, error) {
	return gateway.Reply{Payload: []byte(`{}`)}, nil
}

func TestPool_NackRedispatchesWithinDeadline(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(time.Second)
	var attempts []uint32
	var mu sync.Mutex
	nack := &fakeUp{handle: func(_ context.Context, env gateway.Envelope) (gateway.Reply, error) {
		mu.Lock()
		attempts = append(attempts, env.Attempt)
		mu.Unlock()
		return gateway.Reply{Nack: true}, nil
	}}
	good := &fakeUp{handle: func(_ context.Context, env gateway.Envelope) (gateway.Reply, error) {
		mu.Lock()
		attempts = append(attempts, env.Attempt)
		mu.Unlock()
		return gateway.Reply{Payload: []byte(`{"ok":true}`)}, nil
	}}
	p.Add(gateway.BackendInfo{ID: "a"}, nack)
	p.Add(gateway.BackendInfo{ID: "b"}, good)
	for i := 0; i < 20; i++ {
		rep, err := p.Handle(context.Background(), gateway.Envelope{Kind: gateway.KindCall, Deadline: time.Now().Add(time.Second)})
		require.NoError(t, err)
		require.JSONEq(t, `{"ok":true}`, string(rep.Payload))
	}
	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, attempts, uint32(2), "a redispatch after Nack is attempt 2")
}

func TestPool_NoRedispatchAfterTransportError(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(time.Second)
	boom := errors.New("stream broke")
	a := &fakeUp{handle: func(context.Context, gateway.Envelope) (gateway.Reply, error) { return gateway.Reply{}, boom }}
	b := &fakeUp{handle: ok}
	p.Add(gateway.BackendInfo{ID: "a"}, a)
	// Only the failing member exists: the error comes back, nothing is retried.
	_, err := p.Handle(context.Background(), gateway.Envelope{})
	require.ErrorIs(t, err, boom)
	require.EqualValues(t, 1, a.calls.Load())
	// With a healthy member too, a failure is still not retried on it: the
	// request may have been executed.
	p.Add(gateway.BackendInfo{ID: "b"}, b)
	var failures int
	for i := 0; i < 60; i++ {
		before := b.calls.Load()
		_, err := p.Handle(context.Background(), gateway.Envelope{})
		if err != nil {
			failures++
			require.ErrorIs(t, err, boom)
			require.Equal(t, before, b.calls.Load(), "no redispatch after a transport error")
		}
	}
	require.NotZero(t, failures)
}

func TestPool_NotDeliveredIsRedispatched(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(time.Second)
	a := &fakeUp{handle: func(context.Context, gateway.Envelope) (gateway.Reply, error) {
		return gateway.Reply{}, gateway.ErrNotDelivered
	}}
	b := &fakeUp{handle: ok}
	p.Add(gateway.BackendInfo{ID: "a"}, a)
	p.Add(gateway.BackendInfo{ID: "b"}, b)
	for i := 0; i < 30; i++ {
		_, err := p.Handle(context.Background(), gateway.Envelope{})
		require.NoError(t, err)
	}
	require.EqualValues(t, 30, b.calls.Load())
}

func TestPool_HoldsForABackendThenGivesUp(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(300 * time.Millisecond)
	start := time.Now()
	_, err := p.Handle(context.Background(), gateway.Envelope{})
	require.ErrorIs(t, err, gateway.ErrNoBackend)
	require.GreaterOrEqual(t, time.Since(start), 250*time.Millisecond)

	// A member that appears during the hold gets the request.
	go func() {
		time.Sleep(100 * time.Millisecond)
		p.Add(gateway.BackendInfo{ID: "late"}, &fakeUp{handle: ok})
	}()
	rep, err := p.Handle(context.Background(), gateway.Envelope{})
	require.NoError(t, err)
	require.NotNil(t, rep.Payload)

	// The hold ends with the envelope's deadline and with the context.
	q := gateway.NewPool(10 * time.Second)
	start = time.Now()
	_, err = q.Handle(context.Background(), gateway.Envelope{Deadline: time.Now().Add(100 * time.Millisecond)})
	require.ErrorIs(t, err, gateway.ErrNoBackend)
	require.Less(t, time.Since(start), time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = q.Handle(ctx, gateway.Envelope{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPool_DrainWaitsForInFlightAndStopsNewWork(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(200 * time.Millisecond)
	release := make(chan struct{})
	entered := make(chan struct{})
	slow := &fakeUp{handle: func(context.Context, gateway.Envelope) (gateway.Reply, error) {
		close(entered)
		<-release
		return gateway.Reply{Payload: []byte(`{}`)}, nil
	}}
	m := p.Add(gateway.BackendInfo{ID: "slow"}, slow)
	done := make(chan error, 1)
	go func() {
		_, err := p.Handle(context.Background(), gateway.Envelope{})
		done <- err
	}()
	<-entered

	drained := make(chan error, 1)
	go func() { drained <- p.Drain(context.Background(), m) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-drained:
		t.Fatalf("drain returned with work in flight: %v", err)
	default:
	}
	// New work does not go to the draining member.
	_, err := p.Handle(context.Background(), gateway.Envelope{})
	require.ErrorIs(t, err, gateway.ErrNoBackend)

	close(release)
	require.NoError(t, <-done, "the in-flight request finishes normally")
	require.NoError(t, <-drained)
	require.Equal(t, 0, p.Len())
}

func TestPool_PowerOfTwoChoicesSpreadsLoad(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(time.Second)
	const n = 4
	ups := make([]*fakeUp, n)
	gate := make(chan struct{})
	for i := range ups {
		ups[i] = &fakeUp{handle: func(context.Context, gateway.Envelope) (gateway.Reply, error) {
			<-gate
			return gateway.Reply{}, nil
		}}
		p.Add(gateway.BackendInfo{ID: string(rune('a' + i))}, ups[i])
	}
	var wg sync.WaitGroup
	const reqs = 400
	for i := 0; i < reqs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Handle(context.Background(), gateway.Envelope{})
		}()
	}
	require.Eventually(t, func() bool {
		var total int64
		for _, u := range ups {
			total += u.calls.Load()
		}
		return total == reqs
	}, 5*time.Second, 5*time.Millisecond)
	close(gate)
	wg.Wait()
	for _, u := range ups {
		// Least-loaded of two random choices keeps every member close to the mean.
		require.InDelta(t, reqs/n, u.calls.Load(), 40)
	}
}

func TestWireError_RoundTripKeepsIdentity(t *testing.T) {
	for _, err := range []error{
		ocppj.ErrNotConnected, ocppj.ErrCallTimeout, ocppj.ErrConnReplaced,
		gateway.ErrSessionStale, gateway.ErrGatewayDraining, gateway.ErrNoBackend,
	} {
		got := gateway.EncodeError(err).Err()
		require.ErrorIs(t, got, err)
	}
	ce := &ocppj.CallError{Code: "NotSupported", Description: "no", Details: map[string]any{"a": "b"}}
	got := gateway.EncodeError(ce).Err()
	var back *ocppj.CallError
	require.ErrorAs(t, got, &back)
	require.Equal(t, ce.Code, back.Code)
	require.Equal(t, "no", back.Description)

	other := gateway.EncodeError(errors.New("kaput")).Err()
	require.EqualError(t, other, "kaput")
	require.Nil(t, gateway.EncodeError(nil))
	require.NoError(t, (*gateway.WireError)(nil).Err())

	wrapped := gateway.WithSent(ocppj.ErrCallTimeout, gateway.SentNoResponse)
	require.ErrorIs(t, wrapped, ocppj.ErrCallTimeout)
	require.Equal(t, gateway.SentNoResponse, gateway.SentOf(wrapped))
	require.Equal(t, gateway.SentUnknown, gateway.SentOf(errors.New("x")))
}

func TestPoolSet_OnlyNamedPoolsGetWork(t *testing.T) {
	defer goleak.VerifyNone(t)
	ps := gateway.NewPoolSet(150 * time.Millisecond)
	stable := &fakeUp{handle: ok}
	canary := &fakeUp{handle: ok}
	ps.Add(gateway.BackendInfo{ID: "s", Version: "v1"}, stable)
	ps.Add(gateway.BackendInfo{ID: "c", Pool: "canary", Version: "v2"}, canary)
	ctx := context.Background()

	// No route (no policy): the default pool only; the canary receives nothing.
	for i := 0; i < 10; i++ {
		_, err := ps.Handle(ctx, gateway.Envelope{})
		require.NoError(t, err)
	}
	require.EqualValues(t, 10, stable.calls.Load())
	require.EqualValues(t, 0, canary.calls.Load())

	// A routed message goes to its pool.
	_, err := ps.Handle(ctx, gateway.Envelope{Route: &gateway.Route{Pool: "canary"}})
	require.NoError(t, err)
	require.EqualValues(t, 1, canary.calls.Load())

	// An unavailable pool falls back at once (no hold); without a fallback it holds.
	start := time.Now()
	_, err = ps.Handle(ctx, gateway.Envelope{Route: &gateway.Route{Pool: "ghost", Fallback: gateway.DefaultPool}})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 100*time.Millisecond)
	_, err = ps.Handle(ctx, gateway.Envelope{Route: &gateway.Route{Pool: "ghost"}})
	require.ErrorIs(t, err, gateway.ErrNoBackend)

	// Every member of the routed pool Nacks: redispatched on the fallback.
	nack := &fakeUp{handle: func(context.Context, gateway.Envelope) (gateway.Reply, error) { return gateway.Reply{Nack: true}, nil }}
	ps.Add(gateway.BackendInfo{ID: "n", Pool: "nacky"}, nack)
	before := stable.calls.Load()
	_, err = ps.Handle(ctx, gateway.Envelope{Route: &gateway.Route{Pool: "nacky", Fallback: gateway.DefaultPool}, Deadline: time.Now().Add(time.Second)})
	require.NoError(t, err)
	require.Equal(t, before+1, stable.calls.Load())

	// Callers take no inbound work.
	require.Nil(t, ps.Add(gateway.BackendInfo{ID: "x", Role: gateway.RoleCaller}, &fakeUp{handle: ok}))
	require.Len(t, ps.Status(), 4)
}

func TestPool_SendNeedsCapability(t *testing.T) {
	defer goleak.VerifyNone(t)
	p := gateway.NewPool(100 * time.Millisecond)
	old := &fakeUp{handle: ok}
	p.Add(gateway.BackendInfo{ID: "old"}, old)
	require.ErrorIs(t, p.Notify(context.Background(), gateway.Envelope{Kind: gateway.KindSend}), gateway.ErrNoBackend)
	p.Add(gateway.BackendInfo{ID: "new", Caps: []string{gateway.CapSend21}}, &fakeUp{handle: ok})
	require.NoError(t, p.Notify(context.Background(), gateway.Envelope{Kind: gateway.KindSend}))
}
