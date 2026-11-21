package csms_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// drainCP is a simulated charger: it answers every CALL (after delay) and
// records how its socket ended.
type drainCP struct {
	id       string
	c        *websocket.Conn
	delay    atomic.Int64 // ns before a CALL is answered
	received atomic.Int32
	closedAt atomic.Int64 // unix nano
	code     atomic.Int32
	reason   atomic.Value
}

func dialDrainCP(ctx context.Context, t testing.TB, base, id string) *drainCP {
	t.Helper()
	c, _, err := websocket.Dial(ctx, "ws"+base[len("http"):]+"/ocpp/"+id, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	cp := &drainCP{id: id, c: c}
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				var ce websocket.CloseError
				if errors.As(err, &ce) {
					cp.code.Store(int32(ce.Code))
					cp.reason.Store(ce.Reason)
				}
				cp.closedAt.Store(time.Now().UnixNano())
				return
			}
			var frame []json.RawMessage
			if json.Unmarshal(data, &frame) != nil || len(frame) < 3 {
				continue
			}
			cp.received.Add(1)
			var msgID string
			_ = json.Unmarshal(frame[1], &msgID)
			d := time.Duration(cp.delay.Load())
			go func() {
				time.Sleep(d)
				out, _ := json.Marshal([]any{3, msgID, map[string]any{"status": "Accepted"}})
				_ = c.Write(ctx, websocket.MessageText, out)
			}()
		}
	}()
	t.Cleanup(func() { _ = c.CloseNow() })
	return cp
}

func newDrainServer(t testing.TB, opts ...csms.Option) (*csms.Server, *httptest.Server) {
	t.Helper()
	srv := csms.NewServer(append([]csms.Option{csms.WithSubProtocols("ocpp1.6")}, opts...)...)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return srv, ts
}

func connectAll(ctx context.Context, t testing.TB, srv *csms.Server, base string, n int) []*drainCP {
	t.Helper()
	cps := make([]*drainCP, n)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 50)
	for i := range cps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cps[i] = dialDrainCP(ctx, t, base, fmt.Sprintf("CP_%04d", i))
		}()
	}
	wg.Wait()
	require.Eventually(t, func() bool { return len(srv.Connections()) == n }, 20*time.Second, 20*time.Millisecond)
	return cps
}

func TestDrain_PacedIdleFirstClose1012(t *testing.T) {
	const n = 400
	window := 8 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var disc sync.Map
	srv, ts := newDrainServer(t, csms.WithOnDisconnect(func(c *csms.Conn, err error) { disc.Store(c.ID(), err) }))
	cps := connectAll(ctx, t, srv, ts.URL, n)

	start := time.Now()
	require.NoError(t, srv.Drain(ctx, csms.DrainOptions{Window: window}))
	elapsed := time.Since(start)
	require.True(t, srv.Draining())
	require.Empty(t, srv.Connections())

	// Every charger got 1012 with the drain reason, and the application saw the cause.
	require.Eventually(t, func() bool {
		for _, cp := range cps {
			if cp.closedAt.Load() == 0 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	var times []time.Time
	for _, cp := range cps {
		require.Equal(t, int32(websocket.StatusServiceRestart), cp.code.Load(), cp.id)
		require.Equal(t, "gateway draining", cp.reason.Load())
		times = append(times, time.Unix(0, cp.closedAt.Load()))
		v, ok := disc.Load(cp.id)
		require.True(t, ok)
		require.ErrorIs(t, v.(error), ocppj.ErrGatewayDraining)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	// Target rate: sessions / (0.9 x window). Measure over the whole run and over
	// the middle half, away from the ramp at both ends.
	target := float64(n) / (0.9 * window.Seconds())
	span := times[n-1].Sub(times[0]).Seconds()
	require.InEpsilon(t, target, float64(n-1)/span, 0.10, "overall close rate; elapsed %v", elapsed)
	mid := times[n/4 : 3*n/4]
	midRate := float64(len(mid)-1) / mid[len(mid)-1].Sub(mid[0]).Seconds()
	require.InEpsilon(t, target, midRate, 0.10, "steady close rate")
	require.LessOrEqual(t, elapsed, window+time.Second)
}

func TestDrain_RefusesNewUpgradesWithJitteredRetryAfter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, ts := newDrainServer(t)
	connectAll(ctx, t, srv, ts.URL, 5)

	go func() { _ = srv.Drain(ctx, csms.DrainOptions{Window: 4 * time.Second, RetryAfter: 20 * time.Second}) }()
	require.Eventually(t, srv.Draining, time.Second, time.Millisecond)

	ready := httptest.NewRecorder()
	srv.ReadyHandler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/ready", nil))
	require.Equal(t, http.StatusServiceUnavailable, ready.Code)

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		_, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/NEW", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		require.Error(t, err)
		require.NotNil(t, resp)
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		var secs int
		_, scanErr := fmt.Sscanf(resp.Header.Get("Retry-After"), "%d", &secs)
		require.NoError(t, scanErr)
		require.GreaterOrEqual(t, secs, 10)
		require.LessOrEqual(t, secs, 30)
		seen[resp.Header.Get("Retry-After")] = true
	}
	require.Greater(t, len(seen), 3, "Retry-After is jittered")
	require.Equal(t, uint64(20), srv.AdmissionStats().RejectedDraining)
	require.ErrorIs(t, srv.Drain(ctx, csms.DrainOptions{}), csms.ErrDrainInProgress)
}

func TestDrain_BusySessionIsWaitedFor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, ts := newDrainServer(t)
	cps := connectAll(ctx, t, srv, ts.URL, 3)
	busy := cps[0]
	busy.delay.Store(int64(1500 * time.Millisecond))

	conn, ok := srv.Get(busy.id)
	require.True(t, ok)
	callDone := make(chan error, 1)
	go func() {
		_, err := csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
		callDone <- err
	}()
	require.Eventually(t, func() bool { return busy.received.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	start := time.Now()
	require.NoError(t, srv.Drain(ctx, csms.DrainOptions{Window: 20 * time.Second, Rate: 50, IdleGrace: 10 * time.Second}))
	// The in-flight call finished normally; the busy session left after it, the idle ones first.
	require.NoError(t, <-callDone)
	require.GreaterOrEqual(t, time.Since(start), 1400*time.Millisecond)
	require.Eventually(t, func() bool { return busy.closedAt.Load() != 0 }, 3*time.Second, 10*time.Millisecond)
	for _, cp := range cps[1:] {
		require.Eventually(t, func() bool { return cp.closedAt.Load() != 0 }, 3*time.Second, 10*time.Millisecond)
		require.Less(t, cp.closedAt.Load(), busy.closedAt.Load(), "idle sessions close before the busy one")
	}
	require.Equal(t, int32(websocket.StatusServiceRestart), busy.code.Load())
}

func TestDrain_NoCallLostMidFlight(t *testing.T) {
	const n = 150
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv, ts := newDrainServer(t)
	cps := connectAll(ctx, t, srv, ts.URL, n)
	for _, cp := range cps {
		cp.delay.Store(int64(20 * time.Millisecond))
	}

	var ok, notSent, lost atomic.Int32
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 20; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := w; ; i += 7 {
				select {
				case <-stop:
					return
				default:
				}
				conn, ok2 := srv.Get(cps[i%n].id)
				if !ok2 {
					continue
				}
				cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := csms.Call(cctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
				ccancel()
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ocppj.ErrGatewayDraining):
					notSent.Add(1)
				default:
					lost.Add(1)
					t.Logf("call lost: %v", err)
				}
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, srv.Drain(ctx, csms.DrainOptions{Window: 4 * time.Second}))
	close(stop)
	wg.Wait()
	require.Positive(t, ok.Load())
	require.Zero(t, lost.Load(), "calls written to a charger must complete; %d ok, %d refused unsent", ok.Load(), notSent.Load())
}

func TestDrain_LeaseMarkedDraining(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reg := memory.NewSessionRegistry()
	srv, ts := newDrainServer(t, csms.WithSessionRegistry(reg, csms.SessionConfig{GatewayID: "A"}))
	connectAll(ctx, t, srv, ts.URL, 2)
	gws, err := reg.Gateways(ctx)
	require.NoError(t, err)
	require.Equal(t, storage.GatewayServing, gws[0].State)

	require.NoError(t, srv.Drain(ctx, csms.DrainOptions{Window: 2 * time.Second}))
	gws, _ = reg.Gateways(ctx)
	require.Len(t, gws, 1)
	require.Equal(t, storage.GatewayDraining, gws[0].State)
}

func TestAcceptRateLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, ts := newDrainServer(t, csms.WithAcceptRateLimit(5, 10))

	var okN, refused int
	for i := 0; i < 40; i++ {
		c, resp, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/"+fmt.Sprintf("CP_%d", i), &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		if err == nil {
			okN++
			t.Cleanup(func() { _ = c.CloseNow() })
			continue
		}
		require.NotNil(t, resp)
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		require.NotEmpty(t, resp.Header.Get("Retry-After"))
		refused++
	}
	// A burst of 10 plus what 5/s refills while the loop runs.
	require.GreaterOrEqual(t, okN, 10)
	require.LessOrEqual(t, okN, 15)
	require.Equal(t, uint64(refused), srv.AdmissionStats().RejectedRateLimit)

	// The bucket refills.
	time.Sleep(1100 * time.Millisecond)
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/AFTER", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	_ = c.CloseNow()
}

func TestRateLimitListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	limited := csms.RateLimitListener(ln, 2, 3)
	t.Cleanup(func() { _ = limited.Close() })
	accepted := make(chan net.Conn, 16)
	go func() {
		for {
			c, err := limited.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	var dropped int
	for i := 0; i < 8; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		require.NoError(t, err)
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
		if _, err := c.Read(make([]byte, 1)); err != nil && !errors.Is(err, net.ErrClosed) {
			var ne net.Error
			if !(errors.As(err, &ne) && ne.Timeout()) {
				dropped++ // closed by the limiter (EOF / reset)
			}
		}
		_ = c.Close()
	}
	require.GreaterOrEqual(t, dropped, 3, "connections over the burst are closed at accept")
	require.LessOrEqual(t, len(accepted), 5)
}
