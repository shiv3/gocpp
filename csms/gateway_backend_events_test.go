package csms_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func sessEnv(k gateway.Kind, gw string, epoch uint64) gateway.Envelope {
	e := gateway.Envelope{
		Proto: gateway.ProtoVersion, Version: ocppj.V16, Kind: k, Attempt: 1,
		Session: gateway.SessionRef{CPID: "CP_1", GatewayID: gw, Boot: "b", Epoch: epoch},
	}
	switch k {
	case gateway.KindSessionOpened:
		e.Open = &gateway.SessionInfo{}
	case gateway.KindSessionClosed:
		e.Close = &gateway.CloseInfo{Cause: gateway.SentinelName(ocppj.ErrConnClosed)}
	}
	return e
}

// With several backends a session's SessionClosed may reach one that never saw its
// SessionOpened. That backend must still report the disconnect (review B2: it
// dropped it as stale, 21 of 40 OnDisconnect missing).
func TestGateway_SessionClosedOnAnotherBackendStillFiresDisconnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	var connects, disconnects atomic.Int32
	newBackend := func() *csms.Server {
		return csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"),
			csms.WithOnConnect(func(*csms.Conn) { connects.Add(1) }),
			csms.WithOnDisconnect(func(*csms.Conn, error) { disconnects.Add(1) }))
	}
	ctx := context.Background()
	for round := 0; round < 40; round++ {
		a, b := newBackend(), newBackend()
		gw := fmt.Sprintf("gw-%d", round%3)
		require.NoError(t, a.Upstream().Notify(ctx, sessEnv(gateway.KindSessionOpened, gw, 1)))
		require.NoError(t, b.Upstream().Notify(ctx, sessEnv(gateway.KindSessionClosed, gw, 1)))
		a.Close()
		b.Close()
	}
	require.EqualValues(t, 40, connects.Load())
	require.EqualValues(t, 40, disconnects.Load(), "OnDisconnect lost when the close reached another backend")
}

// A close of an already replaced session is still reported once only.
func TestGateway_LateClosedOfReplacedSessionSuppressed(t *testing.T) {
	defer goleak.VerifyNone(t)
	var disconnects atomic.Int32
	be := csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnDisconnect(func(*csms.Conn, error) { disconnects.Add(1) }))
	defer be.Close()
	ctx := context.Background()
	up := be.Upstream()
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionOpened, "gw-1", 1)))
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionOpened, "gw-1", 2)))
	require.EqualValues(t, 1, disconnects.Load())
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionClosed, "gw-1", 1)))
	require.EqualValues(t, 1, disconnects.Load(), "late close of the replaced session must not fire again")
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionClosed, "gw-1", 2)))
	require.EqualValues(t, 2, disconnects.Load())
}

// A CSMS call whose caller gives up while the frame is being written must not be
// reported NotSent when the charger got it: the caller would retry a command the
// charger already holds (review S1).
func TestGateway_CallCancelRaceNeverReportsNotSentForDeliveredFrame(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRig(t, nil, nil)
	defer r.close()
	r.serve()
	client := r.client()
	var received atomic.Int32
	require.NoError(t, cp.On(client, gwResetMsg, func(ctx context.Context, _ gwReset) (gwResetResp, error) {
		received.Add(1)
		return gwResetResp{Status: "Accepted"}, nil
	}))
	var h *csms.Conn
	require.Eventually(t, func() bool {
		var err error
		h, err = r.be.Lookup(context.Background(), "CP_1")
		return err == nil
	}, 2*time.Second, 5*time.Millisecond)
	ref, _ := h.GatewaySession()
	dn := r.hub.Backend()
	for i := 0; i < 400; i++ {
		before := received.Load()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(time.Duration(rand.IntN(300)) * time.Microsecond)
			cancel()
		}()
		rep, err := dn.Call(ctx, gateway.CallRequest{Target: ref, CallID: fmt.Sprintf("c%d", i),
			Action: "Reset", Payload: []byte(`{"type":"Soft"}`)})
		cancel()
		require.NoError(t, err)
		if rep.Sent == gateway.NotSent {
			time.Sleep(15 * time.Millisecond) // a late frame would show up by now
			require.Equal(t, before, received.Load(), "iteration %d: reported NotSent but the charger received the CALL", i)
		} else {
			require.Eventually(t, func() bool { return received.Load() > before }, time.Second, time.Millisecond)
		}
	}
}

// A blocked OnConnect delays only its own charger's next session event; it must
// not hold up other chargers (review S4: a striped lock stalled 1/64 of them).
func TestGateway_BlockedHookDoesNotStallOtherChargers(t *testing.T) {
	defer goleak.VerifyNone(t)
	release := make(chan struct{})
	be := csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnConnect(func(c *csms.Conn) {
			if c.ID() == "CP_BLOCK" {
				<-release
			}
		}))
	defer be.Close()
	up := be.Upstream()
	ctx := context.Background()
	open := func(id string) error {
		e := sessEnv(gateway.KindSessionOpened, "gw-1", 1)
		e.Session.CPID = id
		return up.Notify(ctx, e)
	}
	blocked := make(chan struct{})
	go func() { _ = open("CP_BLOCK"); close(blocked) }()
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			_ = open(fmt.Sprintf("CP_%d", i))
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("another charger's session event was stalled behind a blocked hook")
	}
	close(release)
	<-blocked
}

// With a session registry epochs order sessions across gateways: a late
// SessionOpened{A,5} after {B,6} must neither fire a disconnect for B's newer
// session nor replace it in the cache (review S3).
func TestGateway_LateOpenedFromOlderEpochOfOtherGatewayIgnored(t *testing.T) {
	defer goleak.VerifyNone(t)
	var connects, disconnects atomic.Int32
	var lastDisc atomic.Value
	be := csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"),
		csms.WithSessionRegistry(memory.NewSessionRegistry()),
		csms.WithOnConnect(func(*csms.Conn) { connects.Add(1) }),
		csms.WithOnDisconnect(func(_ *csms.Conn, err error) { disconnects.Add(1); lastDisc.Store(err) }))
	defer be.Close()
	ctx := context.Background()
	up := be.Upstream()
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionOpened, "gw-B", 6)))
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionOpened, "gw-A", 5)))
	require.EqualValues(t, 1, connects.Load(), "a late open of an older session must not be announced")
	require.EqualValues(t, 0, disconnects.Load(), "no false ErrConnReplaced for the newer session")

	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionClosed, "gw-A", 5)))
	require.EqualValues(t, 0, disconnects.Load(), "close of the older session is suppressed")
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionClosed, "gw-B", 6)))
	require.EqualValues(t, 1, disconnects.Load())
}

// A charger that reconnects through another gateway has its old socket closed at
// once, by the backend that sees the newer session, not when the old gateway's
// reconcile pass runs (review S5).
func TestGateway_ReconnectViaOtherGatewayClosesOldSocketAtOnce(t *testing.T) {
	defer goleak.VerifyNone(t)
	reg := memory.NewSessionRegistry()
	hub := inproc.New()
	slow := csms.SessionConfig{ReconcileInterval: time.Hour, HeartbeatInterval: time.Hour}
	mkGW := func(id string) (*csms.Server, *httptest.Server) {
		cfg := slow
		cfg.GatewayID = id
		gw := csms.NewGatewayServer(hub.Gateway(), csms.WithGatewayID(id), csms.WithSessionRegistry(reg, cfg))
		ts := httptest.NewServer(gw.Handler())
		return gw, ts
	}
	gwA, tsA := mkGW("gw-A")
	gwB, tsB := mkGW("gw-B")
	be := csms.NewServer(csms.WithGatewayLink(hub.Backend()), csms.WithSessionRegistry(reg, csms.SessionConfig{GatewayID: "be"}))
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { defer close(served); _ = be.ServeGateway(ctx) }()
	defer func() {
		tsA.Close()
		tsB.Close()
		gwA.Close()
		gwB.Close()
		cancel()
		<-served
		be.Close()
	}()
	require.Eventually(t, func() bool { return hub.Backends() == 1 }, 2*time.Second, 5*time.Millisecond)

	dial := func(ts *httptest.Server) *websocket.Conn {
		c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		require.NoError(t, err)
		return c
	}
	old := dial(tsA)
	defer func() { _ = old.CloseNow() }()
	require.Eventually(t, func() bool { _, ok := gwA.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)
	// The backend must have seen A's session before B's arrives.
	require.Eventually(t, func() bool { _, err := be.Lookup(ctx, "CP_1"); return err == nil }, 2*time.Second, 5*time.Millisecond)
	nw := dial(tsB)
	defer func() { _ = nw.CloseNow() }()

	rctx, rcancel := context.WithTimeout(ctx, 3*time.Second)
	defer rcancel()
	_, _, err := old.Read(rctx)
	var ce websocket.CloseError
	require.ErrorAs(t, err, &ce, "old socket was not closed (reconcile interval is an hour)")
	require.Equal(t, websocket.StatusPolicyViolation, ce.Code)
	_, ok := gwB.Get("CP_1")
	require.True(t, ok)
}

// Deadlines are absolute times on the gateway's clock; a backend whose clock is
// ahead must not cancel the handler at once (review S9). The envelope's budget
// is Deadline-ReceivedAt, both from the gateway's clock.
func TestGateway_HandlerBudgetIgnoresClockSkew(t *testing.T) {
	defer goleak.VerifyNone(t)
	be := csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"))
	defer be.Close()
	var ctxErr atomic.Value
	var hasDeadline atomic.Bool
	require.NoError(t, csms.On(be, gwBootMsg, func(ctx context.Context, _ *csms.Conn, _ gwBoot) (gwBootResp, error) {
		ctxErr.Store(fmt.Sprint(ctx.Err()))
		_, ok := ctx.Deadline()
		hasDeadline.Store(ok)
		return gwBootResp{Status: "Accepted", Interval: 60}, nil
	}))
	// The gateway's clock is one hour behind this process's.
	gwNow := time.Now().Add(-time.Hour)
	env := gateway.Envelope{
		Proto: gateway.ProtoVersion, Version: ocppj.V16, Kind: gateway.KindCall, Attempt: 1,
		Session: gateway.SessionRef{CPID: "CP_1", GatewayID: "gw-1", Boot: "b", Epoch: 1},
		MsgID:   "m1", Action: "BootNotification", Payload: []byte(`{"chargePointVendor":"x"}`),
		ReceivedAt: gwNow, Deadline: gwNow.Add(30 * time.Second),
	}
	rep, err := be.Upstream().Handle(context.Background(), env)
	require.NoError(t, err)
	require.Nil(t, rep.Error, "handler failed: %+v", rep.Error)
	require.Equal(t, "<nil>", ctxErr.Load(), "handler context was already cancelled by the skewed deadline")
	require.True(t, hasDeadline.Load(), "the handler keeps a deadline")
}

// The same for a CSMS call: the request carries the time left, and the gateway
// applies it from its own clock.
func TestGateway_CallBudgetIgnoresClockSkew(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRig(t, nil, nil)
	defer r.close()
	r.serve()
	client := r.client()
	require.NoError(t, cp.On(client, gwResetMsg, func(context.Context, gwReset) (gwResetResp, error) {
		return gwResetResp{Status: "Accepted"}, nil
	}))
	var h *csms.Conn
	require.Eventually(t, func() bool {
		var err error
		h, err = r.be.Lookup(context.Background(), "CP_1")
		return err == nil
	}, 2*time.Second, 5*time.Millisecond)
	ref, _ := h.GatewaySession()
	rep, err := r.hub.Backend().Call(context.Background(), gateway.CallRequest{
		Target: ref, CallID: "skew", Action: "Reset", Payload: []byte(`{"type":"Soft"}`),
		Deadline: time.Now().Add(-time.Hour), // the caller's clock is ahead of the gateway's
		Budget:   5 * time.Second,
	})
	require.NoError(t, err)
	require.Nil(t, rep.Error, "call failed: %v", rep.Error)
	require.Equal(t, gateway.Answered, rep.Sent)
}

// The processing tier can invalidate the gateways' auth caches after a
// credential change; without it a revoked credential keeps being admitted from the
// cache for its TTL (review S10).
func TestGateway_InvalidateGatewayAuthCache(t *testing.T) {
	defer goleak.VerifyNone(t)
	var decisions atomic.Int64
	var revoked atomic.Bool
	r := newRig(t, []csms.Option{csms.WithGatewayAuthCache(csms.AuthCacheConfig{TTL: time.Hour, StaleIfError: time.Hour})},
		[]csms.Option{csms.WithAuthenticator(auth.BasicAuth(func(cpID, _ string) (auth.Identity, error) {
			decisions.Add(1)
			if revoked.Load() {
				return auth.Identity{}, auth.ErrUnauthorized
			}
			return auth.Identity{CPID: cpID}, nil
		}))})
	defer r.close()
	r.serve()
	dial := func() int {
		h := http.Header{}
		h.Set("Authorization", "Basic "+basic("CP_1", "pw"))
		c, resp, err := websocket.Dial(context.Background(), r.url+"CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, HTTPHeader: h})
		if err != nil {
			return resp.StatusCode
		}
		_ = c.CloseNow()
		return http.StatusSwitchingProtocols
	}
	require.Equal(t, 101, dial())
	revoked.Store(true)
	require.Equal(t, 101, dial(), "still admitted from the gateway's cache")
	require.EqualValues(t, 1, decisions.Load())
	require.NoError(t, r.be.InvalidateGatewayAuthCache(context.Background(), "CP_1"))
	require.Equal(t, 401, dial(), "invalidated: the backend decides again")
}

// A full gateway refuses an upgrade before asking the backend to authenticate it
// (review NIT: the backend's credential store was hit even when no slot was free).
func TestGateway_FullGatewayDoesNotAskBackendToAuthenticate(t *testing.T) {
	defer goleak.VerifyNone(t)
	var decisions atomic.Int64
	r := newRig(t, []csms.Option{csms.WithMaxConnections(1)},
		[]csms.Option{csms.WithAuthenticator(auth.BasicAuth(func(cpID, _ string) (auth.Identity, error) {
			decisions.Add(1)
			return auth.Identity{CPID: cpID}, nil
		}))})
	defer r.close()
	r.serve()
	dial := func(id string) (*websocket.Conn, int) {
		h := http.Header{}
		h.Set("Authorization", "Basic "+basic(id, "pw"))
		c, resp, err := websocket.Dial(context.Background(), r.url+id, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, HTTPHeader: h})
		if err != nil {
			return nil, resp.StatusCode
		}
		return c, http.StatusSwitchingProtocols
	}
	c1, st := dial("CP_1")
	require.Equal(t, 101, st)
	defer func() { _ = c1.CloseNow() }()
	require.Eventually(t, func() bool { _, ok := r.gw.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)
	require.EqualValues(t, 1, decisions.Load())
	_, st = dial("CP_2")
	require.Equal(t, 503, st)
	require.EqualValues(t, 1, decisions.Load(), "the backend authenticated a charger the gateway had no slot for")
}
